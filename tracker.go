package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// sentPairTTL bounds how long a watcher=>target alert is remembered so the
// SentPairs map (and state.json) cannot grow without limit.
const sentPairTTL = 30 * 24 * time.Hour

// Tracker manages the follow tracking loop.
type Tracker struct {
	cfg        *Config
	state      *State
	clients    []*TwitterClient
	webhook    *DiscordWebhook
	accounts   []string
	eventsPath string
	statePath  string
	cookieIdx  int
	mu         sync.Mutex
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
		t.state.ByWatcher[strings.ToLower(watcher)] = mapKeys(current)
		time.Sleep(300 * time.Millisecond)
	}
	t.state.PrunePairs(sentPairTTL)
	t.state.Save(t.statePath)
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
