package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// categoryCacheVersion is bumped whenever the categorization taxonomy or prompt
// changes in a way that should invalidate previously cached categories. On load,
// a mismatch triggers a one-time cache reset (see MaybeResetCategoryCache).
const categoryCacheVersion = 4

type State struct {
	ByWatcher            map[string][]string      `json:"byWatcher"`
	SentPairs            map[string]int64         `json:"sentPairs"`
	CategoryCache        map[string]CategoryEntry `json:"categoryCache"`
	CategoryCacheVersion int                      `json:"categoryCacheVersion"`
	SummarizedTargets    map[string]int64         `json:"summarizedTargets"`
	FrontrunCache        map[string]FrontrunEntry `json:"frontrunCache"`
	// AlertedTargets records when a target last fired an instant threshold alert,
	// so the same surge isn't alerted repeatedly within the alert TTL.
	AlertedTargets map[string]int64 `json:"alertedTargets"`
}

// CategoryEntry is a cached categorization result for a single handle.
type CategoryEntry struct {
	Category string `json:"category"`
	Ts       int64  `json:"ts"` // unix millis when it was computed
}

// FrontrunEntry is a cached Frontrun enrichment result for a single handle.
type FrontrunEntry struct {
	UsernameChanged bool   `json:"usernameChanged"`
	OldUsername     string `json:"oldUsername"`
	SmartFollowers  int    `json:"smartFollowers"`
	Ts              int64  `json:"ts"` // unix millis when it was fetched
}

type Event struct {
	Ts              string `json:"ts"`
	Watcher         string `json:"watcher"`
	Target          string `json:"target"`
	TargetLower     string `json:"targetLower"`
	Name            string `json:"name"`
	Bio             string `json:"bio"`
	Category        string `json:"category"`
	FollowersCount  *int   `json:"followersCount"`
	ProfileImageURL string `json:"profileImageUrl"`
	TargetURL       string `json:"targetUrl"`
	// CreatedAt is the target's X account-creation timestamp (legacy format),
	// carried so the summary can compute account age without a re-fetch.
	CreatedAt string `json:"createdAt,omitempty"`
}

func NewState() *State {
	return &State{
		ByWatcher:         make(map[string][]string),
		SentPairs:         make(map[string]int64),
		CategoryCache:     make(map[string]CategoryEntry),
		SummarizedTargets: make(map[string]int64),
		FrontrunCache:     make(map[string]FrontrunEntry),
		AlertedTargets:    make(map[string]int64),
	}
}

func LoadState(statePath string) *State {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return NewState()
	}
	s := NewState()
	json.Unmarshal(data, s)
	if s.ByWatcher == nil {
		s.ByWatcher = make(map[string][]string)
	}
	if s.SentPairs == nil {
		s.SentPairs = make(map[string]int64)
	}
	if s.CategoryCache == nil {
		s.CategoryCache = make(map[string]CategoryEntry)
	}
	if s.SummarizedTargets == nil {
		s.SummarizedTargets = make(map[string]int64)
	}
	if s.FrontrunCache == nil {
		s.FrontrunCache = make(map[string]FrontrunEntry)
	}
	if s.AlertedTargets == nil {
		s.AlertedTargets = make(map[string]int64)
	}
	return s
}

func (s *State) Save(statePath string) error {
	dir := filepath.Dir(statePath)
	os.MkdirAll(dir, 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0644)
}

func (s *State) MakePairKey(watcher, target string) string {
	return strings.ToLower(watcher) + "=>" + strings.ToLower(target)
}

// PrunePairs drops SentPairs entries older than maxAge so the map (and the
// persisted state file) cannot grow without bound on long-running instances.
func (s *State) PrunePairs(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge).UnixMilli()
	for k, ts := range s.SentPairs {
		if ts < cutoff {
			delete(s.SentPairs, k)
		}
	}
}

// GetCachedCategory returns a still-valid cached category for handle, if any.
func (s *State) GetCachedCategory(handle string, ttl time.Duration) (string, bool) {
	e, ok := s.CategoryCache[strings.ToLower(handle)]
	if !ok {
		return "", false
	}
	if time.Now().UnixMilli()-e.Ts > ttl.Milliseconds() {
		return "", false
	}
	return e.Category, true
}

// SetCachedCategory stores a category for handle with the current timestamp.
func (s *State) SetCachedCategory(handle, category string) {
	s.CategoryCache[strings.ToLower(handle)] = CategoryEntry{
		Category: category,
		Ts:       time.Now().UnixMilli(),
	}
}

// MaybeResetCategoryCache clears the category cache once when the stored version
// differs from the given version (e.g. after a taxonomy/prompt change), so stale
// categories get re-evaluated. Returns the number of entries cleared.
func (s *State) MaybeResetCategoryCache(version int) int {
	if s.CategoryCacheVersion == version {
		return 0
	}
	n := len(s.CategoryCache)
	s.CategoryCache = make(map[string]CategoryEntry)
	s.CategoryCacheVersion = version
	return n
}

// PruneCategoryCache drops cache entries older than ttl.
func (s *State) PruneCategoryCache(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl).UnixMilli()
	for k, e := range s.CategoryCache {
		if e.Ts < cutoff {
			delete(s.CategoryCache, k)
		}
	}
}

// GetFrontrun returns a still-valid cached Frontrun enrichment for handle.
func (s *State) GetFrontrun(handle string, ttl time.Duration) (FrontrunEntry, bool) {
	e, ok := s.FrontrunCache[strings.ToLower(handle)]
	if !ok {
		return FrontrunEntry{}, false
	}
	if time.Now().UnixMilli()-e.Ts > ttl.Milliseconds() {
		return FrontrunEntry{}, false
	}
	return e, true
}

// SetFrontrun caches a Frontrun enrichment for handle with the current timestamp.
func (s *State) SetFrontrun(handle string, usernameChanged bool, oldUsername string, smartFollowers int) {
	s.FrontrunCache[strings.ToLower(handle)] = FrontrunEntry{
		UsernameChanged: usernameChanged,
		OldUsername:     oldUsername,
		SmartFollowers:  smartFollowers,
		Ts:              time.Now().UnixMilli(),
	}
}

// PruneFrontrun drops Frontrun cache entries older than ttl.
func (s *State) PruneFrontrun(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl).UnixMilli()
	for k, e := range s.FrontrunCache {
		if e.Ts < cutoff {
			delete(s.FrontrunCache, k)
		}
	}
}

// IsSummarized reports whether a target has already appeared in a summary
// within ttl, so it should be excluded from future summaries.
func (s *State) IsSummarized(target string, ttl time.Duration) bool {
	ts, ok := s.SummarizedTargets[strings.ToLower(target)]
	if !ok {
		return false
	}
	return time.Now().UnixMilli()-ts <= ttl.Milliseconds()
}

// MarkSummarized records that a target has been included in a summary.
func (s *State) MarkSummarized(target string) {
	s.SummarizedTargets[strings.ToLower(target)] = time.Now().UnixMilli()
}

// PruneSummarized drops summarized-target entries older than ttl.
func (s *State) PruneSummarized(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl).UnixMilli()
	for k, ts := range s.SummarizedTargets {
		if ts < cutoff {
			delete(s.SummarizedTargets, k)
		}
	}
}

// WasAlerted reports whether a target already fired an instant threshold alert
// within ttl (so the same surge isn't re-alerted every scan).
func (s *State) WasAlerted(target string, ttl time.Duration) bool {
	ts, ok := s.AlertedTargets[strings.ToLower(target)]
	if !ok {
		return false
	}
	return time.Now().UnixMilli()-ts <= ttl.Milliseconds()
}

// MarkAlerted records that a target fired an instant threshold alert.
func (s *State) MarkAlerted(target string) {
	s.AlertedTargets[strings.ToLower(target)] = time.Now().UnixMilli()
}

// PruneAlerted drops alerted-target entries older than ttl.
func (s *State) PruneAlerted(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl).UnixMilli()
	for k, ts := range s.AlertedTargets {
		if ts < cutoff {
			delete(s.AlertedTargets, k)
		}
	}
}

func AppendEvent(eventsPath string, event Event) error {
	dir := filepath.Dir(eventsPath)
	os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

func MakeEvent(watcher, targetScreen, bio, name, category string, followersCount int, profileImageURL, createdAt string) Event {
	ev := Event{
		Ts:              time.Now().UTC().Format(time.RFC3339),
		Watcher:         watcher,
		Target:          targetScreen,
		TargetLower:     strings.ToLower(targetScreen),
		Name:            name,
		Bio:             bio,
		Category:        category,
		ProfileImageURL: profileImageURL,
		TargetURL:       fmt.Sprintf("https://x.com/%s", targetScreen),
		CreatedAt:       createdAt,
	}
	if followersCount > 0 {
		ev.FollowersCount = &followersCount
	}
	return ev
}

// ReadRecentEvents reads events.jsonl and returns those whose timestamp falls
// within the given window (now-window .. now). Malformed lines are skipped.
// A missing file is not an error — it just yields no events.
func ReadRecentEvents(eventsPath string, window time.Duration) ([]Event, error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()

	cutoff := time.Now().Add(-window)
	var events []Event
	scanner := bufio.NewScanner(f)
	// Event lines (bio included) can be long; raise the buffer ceiling.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, ev.Ts)
		if err != nil {
			continue
		}
		if ts.Before(cutoff) {
			continue
		}
		events = append(events, ev)
	}
	return events, scanner.Err()
}
