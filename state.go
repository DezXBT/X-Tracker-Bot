package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type State struct {
	ByWatcher  map[string][]string `json:"byWatcher"`
	SentPairs  map[string]int64    `json:"sentPairs"`
}

type Event struct {
	Ts              string  `json:"ts"`
	Watcher         string  `json:"watcher"`
	Target          string  `json:"target"`
	TargetLower     string  `json:"targetLower"`
	Name            string  `json:"name"`
	Bio             string  `json:"bio"`
	FollowersCount  *int    `json:"followersCount"`
	ProfileImageURL string  `json:"profileImageUrl"`
	TargetURL       string  `json:"targetUrl"`
}

func NewState() *State {
	return &State{
		ByWatcher: make(map[string][]string),
		SentPairs: make(map[string]int64),
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

func MakeEvent(watcher, targetScreen, bio, name string, followersCount int, profileImageURL string) Event {
	ev := Event{
		Ts:              time.Now().UTC().Format(time.RFC3339),
		Watcher:         watcher,
		Target:          targetScreen,
		TargetLower:     strings.ToLower(targetScreen),
		Name:            name,
		Bio:             bio,
		ProfileImageURL: profileImageURL,
		TargetURL:       fmt.Sprintf("https://x.com/%s", targetScreen),
	}
	if followersCount > 0 {
		ev.FollowersCount = &followersCount
	}
	return ev
}
