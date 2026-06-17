package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
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
	Content string         `json:"content,omitempty"`
	Embeds  []webhookEmbed `json:"embeds"`
}

// SendFollowAlert posts a raw follow alert to all configured webhooks.
func (dw *DiscordWebhook) SendFollowAlert(webhookURLs []string, watcher, targetScreen, bio, category string, followersCount int, profileImageURL string, loc *time.Location) error {
	watcherLink := fmt.Sprintf("https://x.com/%s", watcher)
	targetLink := fmt.Sprintf("https://x.com/%s", targetScreen)

	cleanBio := bio
	if cleanBio == "" {
		cleanBio = "(no bio)"
	}
	if utf8.RuneCountInString(cleanBio) > 900 {
		cleanBio = truncateRunes(cleanBio, 897) + "..."
	}

	followerText := "Unknown"
	if followersCount > 0 {
		followerText = formatNumber(followersCount)
	}

	if category == "" {
		category = UncategorizedLabel
	}

	embed := webhookEmbed{
		Color:       0xFFD700,
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

// SendSummary posts the categorized hourly summary to a single webhook. Each
// category becomes an embed field listing its targets and how many distinct
// watchlist accounts followed each one within the window. It returns the target
// handles actually included in the embed, so the caller marks only what was
// really shown (some categories may be dropped to respect Discord's limits).
func (dw *DiscordWebhook) SendSummary(webhookURL string, cats []SummaryCategory, window time.Duration, loc *time.Location) ([]string, error) {
	const (
		maxFieldValue = 1024 // Discord per-field value limit (characters)
		maxFields     = 25   // Discord per-embed field limit
		embedBudget   = 5500 // headroom under Discord's 6000-char total embed limit
	)

	var fields []webhookField
	var included []string
	used := 0
	for _, c := range cats {
		if len(fields) >= maxFields {
			break
		}
		var b strings.Builder
		for _, t := range c.Targets {
			renderTarget(&b, t)
			b.WriteByte('\n')
		}
		val := strings.TrimRight(b.String(), "\n")
		if val == "" {
			continue
		}
		if utf8.RuneCountInString(val) > maxFieldValue {
			val = truncateRunes(val, maxFieldValue-2) + "\n…"
		}
		name := fmt.Sprintf("%s  %s · %d", categoryEmoji(c.Name), c.Name, len(c.Targets))
		// Stop before exceeding the overall embed character budget.
		cost := utf8.RuneCountInString(name) + utf8.RuneCountInString(val)
		if used+cost > embedBudget {
			break
		}
		used += cost
		fields = append(fields, webhookField{Name: name, Value: val})
		for _, t := range c.Targets {
			included = append(included, t.Handle)
		}
	}
	if len(fields) == 0 {
		return nil, nil
	}

	embed := webhookEmbed{
		Title:  fmt.Sprintf("📊 Summary · Last %s", humanizeDuration(window)),
		Color:  0xFFD700,
		Fields: fields,
		Footer: &webhookFooter{
			Text: fmt.Sprintf("X-Tracker-Bot | %s WIB", time.Now().In(loc).Format("02/01/2006, 15:04:05")),
		},
	}
	if err := dw.postToAll([]string{webhookURL}, webhookPayload{Embeds: []webhookEmbed{embed}}); err != nil {
		return nil, err
	}
	return included, nil
}

// renderTarget writes one summary line (plus optional bio sub-line) for a
// target, including all the fast-scan signal markers. Order is tuned so the
// strongest urgency cues (heat, burst, fresh, mutual) sit closest to the handle.
func renderTarget(b *strings.Builder, t SummaryTarget) {
	// Heat marker first — it's the loudest "look here" cue.
	if t.HeatMarker != "" {
		fmt.Fprintf(b, "%s ", t.HeatMarker)
	}
	fmt.Fprintf(b, "`%d×` [@%s](https://x.com/%s)", t.Count, t.Handle, t.Handle)

	if t.IsBurst {
		fmt.Fprintf(b, " · ⚡%s", humanizeSpan(t.BurstSpan))
	}
	if t.IsFresh {
		if t.AgeLabel != "" {
			fmt.Fprintf(b, " · ✨%s", t.AgeLabel)
		} else {
			b.WriteString(" · ✨")
		}
	} else if t.AgeLabel != "" {
		fmt.Fprintf(b, " · 🕯️%s", t.AgeLabel)
	}
	if t.Mutual {
		b.WriteString(" · ⭐")
	}
	if t.Followers > 0 {
		fmt.Fprintf(b, " · 👥 %s", formatCompact(t.Followers))
	}
	if t.SmartFollowers > 0 {
		fmt.Fprintf(b, " · 🧠 %s", formatCompact(t.SmartFollowers))
	}
	if t.UsernameChanged {
		if t.OldUsername != "" {
			fmt.Fprintf(b, " · ✏️ ex @%s", t.OldUsername)
		} else {
			b.WriteString(" · ✏️")
		}
	}
	// Action links on their own line so the main line stays scannable.
	if len(t.ActionLinks) > 0 {
		fmt.Fprintf(b, "\n  %s", strings.Join(t.ActionLinks, " · "))
	}
	if bio := cleanBio(t.Bio); bio != "" {
		// Latest bio on its own indented, italic line under the account.
		fmt.Fprintf(b, "\n> _%s_", truncateRunes(bio, 140))
	}
}

// SendInstantAlert posts a high-urgency alert for a single target that crossed
// the watcher threshold inside the alert window. mention is prepended (outside
// the embed) so Discord actually pings.
func (dw *DiscordWebhook) SendInstantAlert(webhookURL, mention string, t SummaryTarget, category string, window time.Duration, loc *time.Location) error {
	if webhookURL == "" {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%d watchers** followed [@%s](https://x.com/%s)", t.Count, t.Handle, t.Handle)
	if t.IsBurst {
		fmt.Fprintf(&b, " in just **%s**", humanizeSpan(t.BurstSpan))
	} else {
		fmt.Fprintf(&b, " within %s", humanizeDuration(window))
	}
	if t.IsFresh && t.AgeLabel != "" {
		fmt.Fprintf(&b, "\n✨ Fresh account · %s old", t.AgeLabel)
	}
	if t.Followers > 0 {
		fmt.Fprintf(&b, "\n👥 %s followers", formatCompact(t.Followers))
	}
	if len(t.ActionLinks) > 0 {
		fmt.Fprintf(&b, "\n%s", strings.Join(t.ActionLinks, " · "))
	}
	if bio := cleanBio(t.Bio); bio != "" {
		fmt.Fprintf(&b, "\n> _%s_", truncateRunes(bio, 180))
	}

	embed := webhookEmbed{
		Title:       fmt.Sprintf("🚨 HOT: %s", category),
		Color:       0xFF3B30, // red for urgency
		Description: b.String(),
		Footer: &webhookFooter{
			Text: fmt.Sprintf("X-Tracker-Bot · instant alert | %s WIB", time.Now().In(loc).Format("02/01/2006, 15:04:05")),
		},
	}
	payload := webhookPayload{Embeds: []webhookEmbed{embed}}
	if mention != "" {
		payload.Content = mention
	}
	return dw.postOne(webhookURL, payload)
}

// SendDigest posts a daily top-N recap: a flat, ranked leaderboard of the most-
// followed targets across the digest window, regardless of category.
func (dw *DiscordWebhook) SendDigest(webhookURL string, targets []SummaryTarget, window time.Duration, loc *time.Location) error {
	if webhookURL == "" || len(targets) == 0 {
		return nil
	}
	var b strings.Builder
	for i, t := range targets {
		medal := digestRank(i)
		fmt.Fprintf(&b, "%s `%d×` [@%s](https://x.com/%s)", medal, t.Count, t.Handle, t.Handle)
		if t.IsFresh && t.AgeLabel != "" {
			fmt.Fprintf(&b, " · ✨%s", t.AgeLabel)
		}
		if t.Followers > 0 {
			fmt.Fprintf(&b, " · 👥 %s", formatCompact(t.Followers))
		}
		b.WriteByte('\n')
	}
	embed := webhookEmbed{
		Title:       fmt.Sprintf("🗓️ Daily Digest · Top %d · Last %s", len(targets), humanizeDuration(window)),
		Color:       0x34C759, // green
		Description: strings.TrimRight(b.String(), "\n"),
		Footer: &webhookFooter{
			Text: fmt.Sprintf("X-Tracker-Bot · digest | %s WIB", time.Now().In(loc).Format("02/01/2006, 15:04:05")),
		},
	}
	return dw.postOne(webhookURL, webhookPayload{Embeds: []webhookEmbed{embed}})
}

// digestRank returns a medal for the top 3 ranks, else a numbered bullet.
func digestRank(i int) string {
	switch i {
	case 0:
		return "🥇"
	case 1:
		return "🥈"
	case 2:
		return "🥉"
	default:
		return fmt.Sprintf("`%2d`", i+1)
	}
}

// humanizeSpan renders a short burst span compactly: "8m", "45s", "1h2m".
func humanizeSpan(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// categoryEmoji returns a small icon for a category so the summary scans faster.
func categoryEmoji(cat string) string {
	switch cat {
	case "AI":
		return "🤖"
	case "Layer 1":
		return "⛓️"
	case "Layer 2":
		return "🟪"
	case "DeFi":
		return "💧"
	case "NFT":
		return "🖼️"
	case "Gaming":
		return "🎮"
	case "Meme":
		return "🐸"
	case "DePIN":
		return "📡"
	case "RWA":
		return "🏦"
	case "Infra":
		return "🔧"
	case "Social":
		return "💬"
	case "KOL":
		return "🎤"
	case "Trading":
		return "📈"
	default:
		return "🔹"
	}
}

// truncateRunes limits s to at most maxRunes runes without splitting a
// multi-byte UTF-8 character (which would produce an invalid string Discord
// rejects).
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes])
}

// cleanBio collapses newlines and runs of whitespace in a bio into single
// spaces so it renders as one tidy line inside the summary's blockquote.
func cleanBio(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// humanizeDuration renders common durations in English (e.g. "1 Hour").
func humanizeDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		if h := int(d / time.Hour); h == 1 {
			return "1 Hour"
		} else {
			return fmt.Sprintf("%d Hours", h)
		}
	case d%time.Minute == 0:
		if m := int(d / time.Minute); m == 1 {
			return "1 Minute"
		} else {
			return fmt.Sprintf("%d Minutes", m)
		}
	}
	return d.String()
}

// postOne posts a payload to a single webhook URL, returning an error on a
// transport failure or a non-2xx/204 status.
func (dw *DiscordWebhook) postOne(url string, payload webhookPayload) error {
	return dw.postToAll([]string{url}, payload)
}

func (dw *DiscordWebhook) postToAll(urls []string, payload webhookPayload) error {
	// No webhooks configured is not a failure — there is simply nothing to send.
	// (Returning an error here would make a summary-only setup, with raw_webhooks
	// empty, drop the event and starve the hourly summary.)
	if len(urls) == 0 {
		return nil
	}

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

// formatCompact renders a follower count compactly: 1234 -> "1.2K",
// 1_200_000 -> "1.2M". Keeps the summary lines short on the right side.
func formatCompact(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	case n >= 1_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000), ".0") + "K"
	default:
		return fmt.Sprintf("%d", n)
	}
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
