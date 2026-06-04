package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type DiscordWebhook struct {
	client *http.Client
}

func NewDiscordWebhook() *DiscordWebhook {
	return &DiscordWebhook{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type webhookEmbed struct {
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color,omitempty"`
	Fields      []webhookField `json:"fields,omitempty"`
	Thumbnail   *webhookThumb  `json:"thumbnail,omitempty"`
	Footer      *webhookFooter `json:"footer,omitempty"`
}

type webhookField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type webhookThumb struct {
	URL string `json:"url"`
}

type webhookFooter struct {
	Text string `json:"text"`
}

type webhookPayload struct {
	Embeds []webhookEmbed `json:"embeds"`
}

// SendFollowAlert posts a raw follow alert to all configured webhooks.
func (dw *DiscordWebhook) SendFollowAlert(webhookURLs []string, watcher, targetScreen, bio, category string, followersCount int, profileImageURL string, loc *time.Location) error {
	watcherLink := fmt.Sprintf("https://x.com/%s", watcher)
	targetLink := fmt.Sprintf("https://x.com/%s", targetScreen)

	cleanBio := bio
	if cleanBio == "" {
		cleanBio = "(no bio)"
	}
	if len(cleanBio) > 900 {
		cleanBio = cleanBio[:897] + "..."
	}

	followerText := "Unknown"
	if followersCount > 0 {
		followerText = formatNumber(followersCount)
	}

	if category == "" {
		category = UncategorizedLabel
	}

	embed := webhookEmbed{
		Color: 0xFFD700,
		Description: fmt.Sprintf("[@%s](%s) just followed [%s](%s)", watcher, watcherLink, targetScreen, targetLink),
		Fields: []webhookField{
			{Name: "Category", Value: category, Inline: true},
			{Name: "Followers", Value: followerText, Inline: true},
			{Name: "Bio", Value: cleanBio},
		},
		Footer: &webhookFooter{
			Text: fmt.Sprintf("Detected by X-Tracker-Bot | %s WIB", time.Now().In(loc).Format("02/01/2006, 15:04:05")),
		},
	}

	if profileImageURL != "" {
		embed.Thumbnail = &webhookThumb{URL: profileImageURL}
	}

	payload := webhookPayload{Embeds: []webhookEmbed{embed}}
	return dw.postToAll(webhookURLs, payload)
}

// SendSummary posts the categorized hourly summary to a single webhook.
// Each category becomes an embed field listing its targets and how many
// distinct watchlist accounts followed each one within the window.
func (dw *DiscordWebhook) SendSummary(webhookURL string, cats []SummaryCategory, window time.Duration, loc *time.Location) error {
	var fields []webhookField
	for _, c := range cats {
		var b strings.Builder
		for _, t := range c.Targets {
			fmt.Fprintf(&b, "[@%s](https://x.com/%s) → **%d**\n", t.Handle, t.Handle, t.Count)
		}
		val := strings.TrimRight(b.String(), "\n")
		if val == "" {
			continue
		}
		// Discord caps a field value at 1024 chars.
		if len(val) > 1024 {
			val = val[:1000] + "\n…"
		}
		fields = append(fields, webhookField{Name: c.Name, Value: val})
	}
	if len(fields) == 0 {
		return nil
	}
	// Discord caps an embed at 25 fields.
	if len(fields) > 25 {
		fields = fields[:25]
	}

	embed := webhookEmbed{
		Title:       fmt.Sprintf("📊 Ringkasan %s Terakhir", humanizeDuration(window)),
		Color:       0x1DA1F2,
		Description: "Akun yang baru di-follow watchlist, dikelompokkan per kategori.\nAngka = jumlah akun tracker yang follow.",
		Fields:      fields,
		Footer: &webhookFooter{
			Text: fmt.Sprintf("X-Tracker-Bot | %s WIB", time.Now().In(loc).Format("02/01/2006, 15:04:05")),
		},
	}
	return dw.postToAll([]string{webhookURL}, webhookPayload{Embeds: []webhookEmbed{embed}})
}

// humanizeDuration renders common durations in Indonesian (e.g. "1 Jam").
func humanizeDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%d Jam", int(d/time.Hour))
	case d%time.Minute == 0:
		return fmt.Sprintf("%d Menit", int(d/time.Minute))
	default:
		return d.String()
	}
}

func (dw *DiscordWebhook) postToAll(urls []string, payload webhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	var lastErr error
	failCount := 0
	for _, u := range urls {
		resp, err := dw.client.Post(u, "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = err
			failCount++
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != 204 && resp.StatusCode != 200 {
			lastErr = fmt.Errorf("webhook HTTP %d", resp.StatusCode)
			failCount++
		}
	}

	if failCount == len(urls) {
		return fmt.Errorf("all %d webhook(s) failed: %v", failCount, lastErr)
	}
	if failCount > 0 {
		logWarn("partial webhook failure: %d/%d", failCount, len(urls))
	}
	return nil
}

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
