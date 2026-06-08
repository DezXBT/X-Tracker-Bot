package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// sentPairTTL bounds how long a watcher=>target alert is remembered so the
// SentPairs map (and state.json) cannot grow without limit.
const sentPairTTL = 30 * 24 * time.Hour

// Tracker manages the follow tracking loop.
type Tracker struct {
	cfg         *Config
	state       *State
	clients     []*TwitterClient
	webhook     *DiscordWebhook
	categorizer *Categorizer
	frontrun    *FrontrunClient // optional; nil when Frontrun enrichment is off
	accounts    []string
	eventsPath  string
	statePath   string
	cookieIdx   int
	mu          sync.Mutex
	// stateMu serializes state mutations + saves between the scan loop and the
	// summary loop, which run on separate goroutines.
	stateMu sync.Mutex
}

func NewTracker(cfg *Config, accounts []string, state *State, eventsPath, statePath string, categorizer *Categorizer) *Tracker {
	clients := make([]*TwitterClient, len(cfg.Twitter.Cookies))
	for i, c := range cfg.Twitter.Cookies {
		clients[i] = NewTwitterClient(c)
	}

	return &Tracker{
		cfg:         cfg,
		state:       state,
		clients:     clients,
		webhook:     NewDiscordWebhook(),
		categorizer: categorizer,
		frontrun:    newFrontrunFromConfig(cfg),
		accounts:    accounts,
		eventsPath:  eventsPath,
		statePath:   statePath,
	}
}

// newFrontrunFromConfig builds a Frontrun client from config, or returns nil when
// the feature is off or its credentials are incomplete (logging a warning in the
// latter case so a misconfiguration is visible).
func newFrontrunFromConfig(cfg *Config) *FrontrunClient {
	if !cfg.FrontrunEnabled() {
		return nil
	}
	tokens := cfg.FrontrunTokens()
	if cfg.Frontrun.BaseURL == "" || len(tokens) == 0 {
		logWarn("[frontrun] enabled but base_url or tokens missing; enrichment disabled")
		return nil
	}
	logInfo("[frontrun] enrichment enabled (%d token(s) in pool)", len(tokens))
	return NewFrontrunClient(cfg.Frontrun.BaseURL, tokens, cfg.Frontrun.ClientVersion, cfg.Frontrun.ClientLanguage)
}

// categorize resolves a target's project category, using the cache first and
// falling back to the categorizer (LLM + keywords) on a miss. On a miss it may
// also fetch the target's recent tweets as an extra signal.
func (t *Tracker) categorize(target User) string {
	ttl := t.cfg.CacheTTLDuration()
	if cat, ok := t.state.GetCachedCategory(target.ScreenName, ttl); ok {
		return cat
	}

	// Recent tweets are only worth an extra API call when the LLM can use them;
	// keyword matching works fine on name + bio alone.
	tweetText := ""
	if t.categorizer.HasLLM() && t.cfg.UseTweetsEnabled() && target.RestID != "" {
		if txt, err := t.getUserTweetsWithRetry(target.RestID, t.cfg.Categorization.TweetCount); err != nil {
			logWarn("[categorize] fetch tweets @%s failed: %v", target.ScreenName, err)
		} else {
			tweetText = txt
		}
	}

	cat := t.categorizer.Categorize(target.Name, target.ScreenName, target.Description, tweetText)
	// Don't cache a non-result: an Uncategorized outcome usually means the LLM
	// was unavailable, so caching it would suppress retries for the full TTL.
	if cat != UncategorizedLabel {
		t.state.SetCachedCategory(target.ScreenName, cat)
	}
	return cat
}

// getUserTweetsWithRetry fetches recent tweet text, rotating across clients on
// auth errors.
func (t *Tracker) getUserTweetsWithRetry(userID string, count int) (string, error) {
	var lastErr error
	for i := 0; i < len(t.clients); i++ {
		txt, err := t.nextClient().GetUserTweets(userID, count)
		if err == nil {
			return txt, nil
		}
		lastErr = err
		if !isAuthError(err) {
			return "", err
		}
		logWarn("[client] auth error on GetUserTweets, rotating: %v", err)
	}
	return "", fmt.Errorf("all clients failed for GetUserTweets: %w", lastErr)
}

// nextClient returns the next client in round-robin order.
func (t *Tracker) nextClient() *TwitterClient {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.clients[t.cookieIdx]
	t.cookieIdx = (t.cookieIdx + 1) % len(t.clients)
	return c
}

// getUserWithRetry fetches a user profile, rotating across all clients on auth
// errors so a single expired cookie does not abort the scan.
func (t *Tracker) getUserWithRetry(handle string) (*User, error) {
	var lastErr error
	for i := 0; i < len(t.clients); i++ {
		user, err := t.nextClient().GetUser(handle)
		if err == nil {
			return user, nil
		}
		lastErr = err
		if !isAuthError(err) {
			return nil, err
		}
		logWarn("[client] auth error on GetUser %s, rotating: %v", handle, err)
	}
	return nil, fmt.Errorf("all clients failed for GetUser %s: %w", handle, lastErr)
}

// getFollowingWithRetry fetches one following page, rotating across all clients
// on auth errors.
func (t *Tracker) getFollowingWithRetry(userID string, count int, cursor string) (*PaginatedResult, error) {
	var lastErr error
	for i := 0; i < len(t.clients); i++ {
		result, err := t.nextClient().GetFollowing(userID, count, cursor)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isAuthError(err) {
			return nil, err
		}
		logWarn("[client] auth error on GetFollowing, rotating: %v", err)
	}
	return nil, fmt.Errorf("all clients failed for GetFollowing: %w", lastErr)
}

// fetchFollowingMap fetches the following map for a watcher using all available pages.
func (t *Tracker) fetchFollowingMap(watcher string) (map[string]User, error) {
	user, err := t.getUserWithRetry(watcher)
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", watcher, err)
	}

	followingMap := make(map[string]User)
	var cursor string

	for page := 0; page < t.cfg.Tracking.MaxPages; page++ {
		result, err := t.getFollowingWithRetry(user.RestID, t.cfg.Tracking.PageSize, cursor)
		if err != nil {
			return nil, fmt.Errorf("get following page %d: %w", page+1, err)
		}

		for _, u := range result.Items {
			followingMap[strings.ToLower(u.ScreenName)] = u
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
		t.state.ByWatcher[strings.ToLower(watcher)] = keys
		logInfo("[warmup] @%s baseline size=%d", watcher, len(m))
		time.Sleep(300 * time.Millisecond)
	}
	t.state.Save(t.statePath)
}

// ScanOnce runs one scan cycle across all watchers.
func (t *Tracker) ScanOnce() {
	// Hold stateMu for the whole cycle so the summary loop can't read/write
	// state or save mid-scan. Scans are infrequent, so the contention is fine.
	t.stateMu.Lock()
	defer t.stateMu.Unlock()

	for _, watcher := range t.accounts {
		logInfo("[scan] watcher @%s", watcher)

		followingMap, err := t.fetchFollowingMap(watcher)
		if err != nil {
			logError("[scan] failed @%s: %v", watcher, err)
			continue
		}

		previous := toSet(t.state.ByWatcher[strings.ToLower(watcher)])
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
			targetLower := strings.ToLower(target)
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

			// Resolve the project category (cached → LLM + tweets → keyword fallback).
			category := t.categorize(targetData)

			// Send the raw alert. A failure here must NOT skip recording the
			// follow below — otherwise the hourly summary (which reads the event
			// log) would starve whenever raw delivery fails or raw_webhooks is
			// empty for a summary-only setup.
			if err := t.webhook.SendFollowAlert(
				t.cfg.Discord.RawWebhooks,
				watcher, targetScreen, bio, category, followersCount, profileImageURL,
				t.cfg.Timezone(),
			); err != nil {
				logError("[alert] failed %s: %v", pairKey, err)
			} else {
				logInfo("[alert] sent %s [%s]", pairKey, category)
			}

			// Record the follow once it's detected, independent of raw delivery,
			// so it appears in the summary and isn't reprocessed every scan.
			t.state.SentPairs[pairKey] = time.Now().UnixMilli()
			ev := MakeEvent(watcher, targetScreen, bio, name, category, followersCount, profileImageURL)
			if err := AppendEvent(t.eventsPath, ev); err != nil {
				logWarn("[event] failed to append: %v", err)
			}
		}

		// Update baseline
		t.state.ByWatcher[strings.ToLower(watcher)] = mapKeys(current)
		time.Sleep(300 * time.Millisecond)
	}
	t.state.PrunePairs(sentPairTTL)
	t.state.PruneCategoryCache(t.cfg.CacheTTLDuration())
	t.state.Save(t.statePath)
}

// ──────────────────────────────────────────────────────────────────────────────
// Hourly categorized summary
// ──────────────────────────────────────────────────────────────────────────────

// RunSummaryLoop posts a categorized summary every summary_interval until ctx
// is cancelled. It is a no-op when no summary_webhook is configured.
func (t *Tracker) RunSummaryLoop(ctx context.Context) {
	if t.cfg.Discord.SummaryWebhook == "" {
		logInfo("[summary] no summary_webhook configured; summary disabled")
		return
	}
	interval := t.cfg.SummaryIntervalDuration()
	if interval <= 0 {
		logWarn("[summary] non-positive interval %s; summary disabled", interval)
		return
	}
	logInfo("[summary] enabled; interval=%s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logError("[summary] recovered from panic: %v", r)
					}
				}()
				t.sendSummary(interval)
			}()
		}
	}
}

func (t *Tracker) sendSummary(window time.Duration) {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()

	events, err := ReadRecentEvents(t.eventsPath, window)
	if err != nil {
		logWarn("[summary] read events: %v", err)
		return
	}
	if len(events) == 0 {
		logInfo("[summary] nothing in last %s; skipping", window)
		return
	}

	grouped := aggregateByCategory(events)

	// Optionally drop targets whose follower count exceeds the configured max,
	// so the summary can focus on smaller/early accounts.
	if t.cfg.SummaryFollowerFilterEnabled() {
		max := t.cfg.SummaryMaxFollowersValue()
		grouped = filterByMaxFollowers(grouped, max)
		if len(grouped) == 0 {
			logInfo("[summary] all targets exceed max followers (%d); skipping", max)
			return
		}
	}

	// Exclude targets already shown in a previous summary, so each project is
	// reported at most once (per dedup TTL) — keeps the channel readable.
	ttl := t.cfg.SummaryDedupTTLDuration()
	grouped, newTargets := filterUnsummarized(grouped, t.state, ttl)
	if len(newTargets) == 0 {
		logInfo("[summary] no new projects since last summary; skipping")
		return
	}

	// Optionally enrich the (already filtered) targets with Frontrun signals —
	// username-change marker + smart-followers count. Only runs when enabled, and
	// only for targets that will actually be shown, to keep API calls minimal.
	if t.frontrun != nil {
		t.enrichSummaryFrontrun(grouped)
	}

	// Drop bios up front when the toggle is off so the renderer can stay simple
	// ("show it if present").
	if !t.cfg.SummaryShowBio() {
		for ci := range grouped {
			for ti := range grouped[ci].Targets {
				grouped[ci].Targets[ti].Bio = ""
			}
		}
	}

	// Mark only what was actually included in the embed (SendSummary may drop
	// categories to fit Discord's limits) so dropped projects aren't lost.
	included, err := t.webhook.SendSummary(t.cfg.Discord.SummaryWebhook, grouped, window, t.cfg.Timezone())
	if err != nil {
		logError("[summary] send failed: %v", err)
		return
	}
	if len(included) == 0 {
		return
	}
	for _, h := range included {
		t.state.MarkSummarized(h)
	}
	t.state.PruneSummarized(ttl)
	if t.frontrun != nil {
		t.state.PruneFrontrun(t.cfg.FrontrunCacheTTLDuration())
	}
	t.state.Save(t.statePath)
	logInfo("[summary] sent (%d new projects)", len(included))
}

// enrichSummaryFrontrun fills the Frontrun fields (username-change marker +
// smart-followers count) on each summary target, using the cache first and
// calling the Frontrun API on a miss. Per-target errors are non-fatal: the
// target simply shows no extra marker.
func (t *Tracker) enrichSummaryFrontrun(cats []SummaryCategory) {
	ttl := t.cfg.FrontrunCacheTTLDuration()
	showName := t.cfg.FrontrunShowUsernameChange()
	showSmart := t.cfg.FrontrunShowSmartFollowers()

	for ci := range cats {
		for ti := range cats[ci].Targets {
			handle := cats[ci].Targets[ti].Handle

			if e, ok := t.state.GetFrontrun(handle, ttl); ok {
				cats[ci].Targets[ti].UsernameChanged = e.UsernameChanged
				cats[ci].Targets[ti].OldUsername = e.OldUsername
				cats[ci].Targets[ti].SmartFollowers = e.SmartFollowers
				continue
			}

			var (
				changed bool
				oldName string
				smart   int
			)
			if showName {
				if hist, err := t.frontrun.GetUsernameHistory(handle); err != nil {
					logWarn("[frontrun] username-history @%s: %v", handle, err)
				} else if len(hist) > 0 {
					changed = true
					oldName = latestOldUsername(hist)
				}
			}
			if showSmart {
				if sf, err := t.frontrun.GetSmartFollowers(handle); err != nil {
					logWarn("[frontrun] smart-followers @%s: %v", handle, err)
				} else {
					smart = len(sf)
				}
			}

			cats[ci].Targets[ti].UsernameChanged = changed
			cats[ci].Targets[ti].OldUsername = oldName
			cats[ci].Targets[ti].SmartFollowers = smart
			t.state.SetFrontrun(handle, changed, oldName, smart)

			// Be gentle on the Frontrun API between targets.
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// latestOldUsername returns the most recently changed-from username in the
// history (the handle's immediately previous username).
func latestOldUsername(hist []UsernameHistoryEntry) string {
	best := ""
	var bestTs time.Time
	for _, h := range hist {
		if h.OldUsername == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, h.ChangedAt)
		if err != nil {
			// Unparseable timestamp: fall back to the last non-empty entry seen.
			best = h.OldUsername
			continue
		}
		if best == "" || ts.After(bestTs) {
			best = h.OldUsername
			bestTs = ts
		}
	}
	return best
}

// filterByMaxFollowers drops targets whose follower count exceeds max, and
// removes any category left empty. Targets with an unknown follower count
// (Followers == 0) are kept, so accounts whose data hasn't been resolved yet
// are not silently dropped.
func filterByMaxFollowers(cats []SummaryCategory, max int) []SummaryCategory {
	var out []SummaryCategory
	for _, c := range cats {
		var kept []SummaryTarget
		for _, tgt := range c.Targets {
			if tgt.Followers > max {
				continue
			}
			kept = append(kept, tgt)
		}
		if len(kept) > 0 {
			out = append(out, SummaryCategory{Name: c.Name, Targets: kept})
		}
	}
	return out
}

// filterUnsummarized removes targets already reported within ttl and drops any
// category left empty. It returns the filtered categories and the list of new
// target handles included.
func filterUnsummarized(cats []SummaryCategory, state *State, ttl time.Duration) ([]SummaryCategory, []string) {
	var out []SummaryCategory
	var newTargets []string
	for _, c := range cats {
		var kept []SummaryTarget
		for _, tgt := range c.Targets {
			if state.IsSummarized(tgt.Handle, ttl) {
				continue
			}
			kept = append(kept, tgt)
			newTargets = append(newTargets, tgt.Handle)
		}
		if len(kept) > 0 {
			out = append(out, SummaryCategory{Name: c.Name, Targets: kept})
		}
	}
	return out, newTargets
}

// SummaryTarget is one followed account and how many distinct watchers followed
// it within the summary window.
type SummaryTarget struct {
	Handle    string
	Count     int
	Followers int
	// Bio is the account's latest bio (from X GraphQL, carried via the event).
	Bio string
	// Optional Frontrun enrichment (zero values when disabled or unavailable).
	UsernameChanged bool
	OldUsername     string
	SmartFollowers  int
}

// SummaryCategory groups targets under a project category.
type SummaryCategory struct {
	Name    string
	Targets []SummaryTarget
}

// aggregateByCategory groups events by category, counting distinct watchers per
// target. Categories and targets are sorted by activity (descending).
func aggregateByCategory(events []Event) []SummaryCategory {
	// category -> targetLower -> set of watchers
	byCat := make(map[string]map[string]map[string]bool)
	display := make(map[string]string) // targetLower -> original screen name
	followers := make(map[string]int)  // targetLower -> highest followers seen
	bios := make(map[string]string)    // targetLower -> latest non-empty bio

	for _, e := range events {
		cat := e.Category
		if cat == "" {
			cat = UncategorizedLabel
		}
		tl := e.TargetLower
		if tl == "" {
			tl = strings.ToLower(e.Target)
		}
		if tl == "" {
			continue // skip events with no resolvable target handle
		}
		if byCat[cat] == nil {
			byCat[cat] = make(map[string]map[string]bool)
		}
		if byCat[cat][tl] == nil {
			byCat[cat][tl] = make(map[string]bool)
		}
		byCat[cat][tl][strings.ToLower(e.Watcher)] = true
		display[tl] = e.Target
		if e.FollowersCount != nil && *e.FollowersCount > followers[tl] {
			followers[tl] = *e.FollowersCount
		}
		// Events are appended chronologically, so the last non-empty bio wins.
		if e.Bio != "" {
			bios[tl] = e.Bio
		}
	}

	var cats []SummaryCategory
	for cat, targets := range byCat {
		var ts []SummaryTarget
		for tl, watchers := range targets {
			ts = append(ts, SummaryTarget{Handle: display[tl], Count: len(watchers), Followers: followers[tl], Bio: bios[tl]})
		}
		sort.Slice(ts, func(i, j int) bool {
			if ts[i].Count != ts[j].Count {
				return ts[i].Count > ts[j].Count
			}
			return strings.ToLower(ts[i].Handle) < strings.ToLower(ts[j].Handle)
		})
		cats = append(cats, SummaryCategory{Name: cat, Targets: ts})
	}

	sort.Slice(cats, func(i, j int) bool {
		ci, cj := categoryTotal(cats[i]), categoryTotal(cats[j])
		if ci != cj {
			return ci > cj
		}
		return cats[i].Name < cats[j].Name
	})
	return cats
}

func categoryTotal(c SummaryCategory) int {
	total := 0
	for _, t := range c.Targets {
		total += t.Count
	}
	return total
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, v := range items {
		s[strings.ToLower(v)] = true
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

func isAuthError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "could not authenticate") ||
		strings.Contains(msg, "401")
}
