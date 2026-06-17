package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// capturingServer records every webhook payload it receives so tests can assert
// on what the bot actually posted.
type capturingServer struct {
	mu       sync.Mutex
	payloads []webhookPayload
	srv      *httptest.Server
}

func newCapturingServer() *capturingServer {
	cs := &capturingServer{}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p webhookPayload
		_ = json.Unmarshal(body, &p)
		cs.mu.Lock()
		cs.payloads = append(cs.payloads, p)
		cs.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	return cs
}

func (cs *capturingServer) url() string { return cs.srv.URL }
func (cs *capturingServer) close()      { cs.srv.Close() }
func (cs *capturingServer) count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.payloads)
}
func (cs *capturingServer) last() webhookPayload {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.payloads[len(cs.payloads)-1]
}

// writeEvents serialises events to a temp jsonl file and returns its path.
func writeEvents(t *testing.T, events []Event) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
	return path
}

// newTestTracker wires a Tracker with the given config + events file, no real
// Twitter clients (the summary/digest/alert paths don't need them).
func newTestTracker(cfg *Config, eventsPath, statePath string) *Tracker {
	return &Tracker{
		cfg:        cfg,
		state:      NewState(),
		webhook:    NewDiscordWebhook(),
		eventsPath: eventsPath,
		statePath:  statePath,
	}
}

func recentEvent(minsAgo int, watcher, target, category, bio string, followers int, createdAt string) Event {
	e := MakeEvent(watcher, target, bio, target, category, followers, "", createdAt)
	e.Ts = time.Now().Add(-time.Duration(minsAgo) * time.Minute).UTC().Format(time.RFC3339)
	return e
}

func TestSendDigestEndToEnd(t *testing.T) {
	cs := newCapturingServer()
	defer cs.close()

	created := time.Now().Add(-10 * 24 * time.Hour).Format(twitterTimeLayout)
	events := []Event{
		recentEvent(5, "alice", "hot", "AI", "ai bio", 200, created),
		recentEvent(4, "bob", "hot", "AI", "ai bio", 200, created),
		recentEvent(3, "carol", "hot", "AI", "ai bio", 200, created),
		recentEvent(2, "dave", "mid", "DeFi", "defi bio", 800, ""),
		recentEvent(1, "erin", "mid", "DeFi", "defi bio", 800, ""),
	}
	eventsPath := writeEvents(t, events)
	statePath := filepath.Join(filepath.Dir(eventsPath), "state.json")

	cfg := &Config{}
	cfg.Discord.DigestEnabled = boolPtr(true)
	cfg.Discord.DigestWebhook = cs.url()
	cfg.Discord.DigestTopN = 5

	tr := newTestTracker(cfg, eventsPath, statePath)
	tr.sendDigest(24 * time.Hour)

	if cs.count() != 1 {
		t.Fatalf("expected 1 digest post, got %d", cs.count())
	}
	desc := cs.last().Embeds[0].Description
	// "hot" had 3 watchers, "mid" had 2 → hot ranks first with the 🥇 medal.
	if !contains(desc, "🥇") || !contains(desc, "@hot") {
		t.Errorf("expected hot as rank 1 in digest, got:\n%s", desc)
	}
	if !contains(desc, "@mid") {
		t.Errorf("expected mid in digest, got:\n%s", desc)
	}
}

func TestThresholdAlertEndToEnd(t *testing.T) {
	cs := newCapturingServer()
	defer cs.close()

	// 3 distinct watchers on "surge" within 10 minutes → crosses threshold of 3.
	events := []Event{
		recentEvent(8, "alice", "surge", "AI", "0x1234567890abcdef1234567890abcdef12345678", 150, ""),
		recentEvent(5, "bob", "surge", "AI", "0x1234567890abcdef1234567890abcdef12345678", 150, ""),
		recentEvent(2, "carol", "surge", "AI", "0x1234567890abcdef1234567890abcdef12345678", 150, ""),
		// only 1 watcher on "quiet" → below threshold.
		recentEvent(3, "dave", "quiet", "DeFi", "nothing", 50, ""),
	}
	eventsPath := writeEvents(t, events)
	statePath := filepath.Join(filepath.Dir(eventsPath), "state.json")

	cfg := &Config{}
	cfg.Discord.ThresholdAlertEnabled = boolPtr(true)
	cfg.Discord.ThresholdAlertWebhook = cs.url()
	cfg.Discord.ThresholdAlertWatchers = 3
	cfg.Discord.ThresholdAlertWindow = "30m"
	cfg.Discord.ThresholdAlertMention = "<@123>"

	tr := newTestTracker(cfg, eventsPath, statePath)
	tr.checkThresholdAlerts()

	if cs.count() != 1 {
		t.Fatalf("expected exactly 1 instant alert (only 'surge' crosses), got %d", cs.count())
	}
	p := cs.last()
	if p.Content != "<@123>" {
		t.Errorf("expected mention content, got %q", p.Content)
	}
	desc := p.Embeds[0].Description
	if !contains(desc, "@surge") || !contains(desc, "3 watchers") {
		t.Errorf("expected surge alert with 3 watchers, got:\n%s", desc)
	}
	// Chart link should be present (contract in bio).
	if !contains(desc, "dexscreener.com") {
		t.Errorf("expected dexscreener chart link in alert, got:\n%s", desc)
	}

	// Second call must NOT re-alert (deduped via AlertedTargets).
	tr.checkThresholdAlerts()
	if cs.count() != 1 {
		t.Errorf("expected no re-alert on second call, got %d total", cs.count())
	}
}

func TestThresholdAlertRespectsMute(t *testing.T) {
	cs := newCapturingServer()
	defer cs.close()

	events := []Event{
		recentEvent(8, "alice", "memecoin", "Meme", "wow", 150, ""),
		recentEvent(5, "bob", "memecoin", "Meme", "wow", 150, ""),
		recentEvent(2, "carol", "memecoin", "Meme", "wow", 150, ""),
	}
	eventsPath := writeEvents(t, events)
	statePath := filepath.Join(filepath.Dir(eventsPath), "state.json")

	cfg := &Config{}
	cfg.Discord.ThresholdAlertEnabled = boolPtr(true)
	cfg.Discord.ThresholdAlertWebhook = cs.url()
	cfg.Discord.ThresholdAlertWatchers = 3
	cfg.Discord.ThresholdAlertWindow = "30m"
	cfg.Discord.SummaryMuteCategories = []string{"Meme"}

	tr := newTestTracker(cfg, eventsPath, statePath)
	tr.checkThresholdAlerts()

	if cs.count() != 0 {
		t.Errorf("expected muted category to suppress instant alert, got %d", cs.count())
	}
}

// contains is a tiny strings.Contains shim to keep imports tight in this file.
func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
