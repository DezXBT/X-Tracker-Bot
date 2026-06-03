package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
func (dw *DiscordWebhook) SendFollowAlert(webhookURLs []string, watcher, targetScreen, bio string, followersCount int, profileImageURL string, loc *time.Location) error {
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

	embed := webhookEmbed{
		Color: 0x561e08,
		Description: fmt.Sprintf("[@%s](%s) just followed [%s](%s)", watcher, watcherLink, targetScreen, targetLink),
		Fields: []webhookField{
			{Name: "Followers", Value: followerText, Inline: true},
			{Name: "Bio", Value: cleanBio},
		},
		Footer: &webhookFooter{
			Text: fmt.Sprintf("Detected by EARLY-TRACKING | %s", time.Now().In(loc).Format("1/2/2006, 3:04:05 PM") + " WIB"),
		},
	}

	if profileImageURL != "" {
		embed.Thumbnail = &webhookThumb{URL: profileImageURL}
	}

	payload := webhookPayload{Embeds: []webhookEmbed{embed}}
	return dw.postToAll(webhookURLs, payload)
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
