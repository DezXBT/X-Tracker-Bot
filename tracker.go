package main

import (
	"fmt"
	"sync"
	"time"
)

// Tracker manages the follow tracking loop.
type Tracker struct {
	cfg       *Config
	state     *State
	clients   []*TwitterClient
	webhook   *DiscordWebhook
	accounts  []string
	eventsPath string
	statePath  string
	cookieIdx  int
	mu        sync.Mutex
}

func NewTracker(cfg *Config, accounts []string, state *State, eventsPath, statePath string) *Tracker {
	clients := make([]*TwitterClient, len(cfg.Twitter.Cookies))
	for i, c := range cfg.Twitter.Cookies {
		clients[i] = NewTwitterClient(c)
	}

	return &Tracker{
		cfg:        cfg,
		state:      state,
		clients:    clients,
		webhook:    NewDiscordWebhook(),
		accounts:   accounts,
		eventsPath: eventsPath,
		statePath:  statePath,
	}
}

// nextClient returns the next client in round-robin order.
func (t *Tracker) nextClient() *TwitterClient {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.clients[t.cookieIdx]
	t.cookieIdx = (t.cookieIdx + 1) % len(t.clients)
	return c
}

// getClientWithRetry tries all clients, skipping auth failures.
func (t *Tracker) getClientForOp() (*TwitterClient, error) {
	n := len(t.clients)
	start := t.cookieIdx
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		return t.clients[idx], nil
	}
	return nil, fmt.Errorf("no clients available")
}

func (t *Tracker) rotateClient() {
	t.mu.Lock()
	t.cookieIdx = (t.cookieIdx + 1) % len(t.clients)
	t.mu.Unlock()
}

// fetchFollowingMap fetches the full following map for a watcher using all available pages.
func (t *Tracker) fetchFollowingMap(watcher string) (map[string]User, error) {
	// First get user ID
	client := t.nextClient()
	user, err := client.GetUser(watcher)
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", watcher, err)
	}

	followingMap := make(map[string]User)
	var cursor string

	for page := 0; page < t.cfg.Tracking.MaxPages; page++ {
		client = t.nextClient()
		result, err := client.GetFollowing(user.RestID, t.cfg.Tracking.PageSize, cursor)
		if err != nil {
			// Try next client on auth errors
			if isAuthError(err) {
				t.rotateClient()
				client = t.nextClient()
				result, err = client.GetFollowing(user.RestID, t.cfg.Tracking.PageSize, cursor)
				if err != nil {
					return nil, fmt.Errorf("get following page %d: %w", page+1, err)
				}
			} else {
				return nil, fmt.Errorf("get following page %d: %w", page+1, err)
			}
		}

		for _, u := range result.Items {
			followingMap[toLowerCase(u.ScreenName)] = u
		}

		if !result.HasMore || result.Cursor == "" {
			break
		}
		cursor = result.Cursor
		time.Sleep(t.cfg.PageDelayDuration())
	}

	return followingMap, nil
}

// Warmup fetches baseline following for all watchers without sending alerts.
func (t *Tracker) Warmup() {
	for _, watcher := range t.accounts {
		logInfo("[warmup] @%s fetching baseline...", watcher)
		m, err := t.fetchFollowingMap(watcher)
		if err != nil {
			logError("[warmup] failed @%s: %v", watcher, err)
			continue
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		t.state.ByWatcher[toLowerCase(watcher)] = keys
		logInfo("[warmup] @%s baseline size=%d", watcher, len(m))
		time.Sleep(300 * time.Millisecond)
	}
	t.state.Save(t.statePath)
}

// ScanOnce runs one scan cycle across all watchers.
func (t *Tracker) ScanOnce() {
	for _, watcher := range t.accounts {
		logInfo("[scan] watcher @%s", watcher)

		followingMap, err := t.fetchFollowingMap(watcher)
		if err != nil {
			logError("[scan] failed @%s: %v", watcher, err)
			continue
		}

		previous := toSet(t.state.ByWatcher[toLowerCase(watcher)])
		current := make(map[string]bool)
		for k := range followingMap {
			current[k] = true
		}

		// Determine which targets to check
		var targetsToCheck []string
		if t.cfg.Tracking.TrackAllFollows {
			for k := range current {
				targetsToCheck = append(targetsToCheck, k)
			}
		} else {
			targetsToCheck = t.accounts
		}

		for _, target := range targetsToCheck {
			targetLower := toLowerCase(target)
			if !current[targetLower] {
				continue
			}
			if previous[targetLower] {
				continue // not a new follow
			}

			pairKey := t.state.MakePairKey(watcher, targetLower)
			if _, sent := t.state.SentPairs[pairKey]; sent {
				continue
			}

			targetData := followingMap[targetLower]
			bio := targetData.Description
			targetScreen := targetData.ScreenName
			followersCount := targetData.FollowersCount
			profileImageURL := targetData.ProfileImageURL
			name := targetData.Name

			// Send webhook alert
			err := t.webhook.SendFollowAlert(
				t.cfg.Discord.RawWebhooks,
				watcher, targetScreen, bio, followersCount, profileImageURL,
				t.cfg.Timezone(),
			)
			if err != nil {
				logError("[alert] failed %s: %v", pairKey, err)
				continue
			}
			t.state.SentPairs[pairKey] = time.Now().UnixMilli()
			logInfo("[alert] sent %s", pairKey)

			// Append event
			ev := MakeEvent(watcher, targetScreen, bio, name, followersCount, profileImageURL)
			if err := AppendEvent(t.eventsPath, ev); err != nil {
				logWarn("[event] failed to append: %v", err)
			}
		}

		// Update baseline
		t.state.ByWatcher[toLowerCase(watcher)] = mapKeys(current)
		time.Sleep(300 * time.Millisecond)
	}
	t.state.Save(t.statePath)
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, v := range items {
		s[toLowerCase(v)] = true
	}
	return s
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func toLowerCase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

func isAuthError(err error) bool {
	msg := err.Error()
	return contains(msg, "unauthorized") || contains(msg, "could not authenticate") || contains(msg, "401")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
