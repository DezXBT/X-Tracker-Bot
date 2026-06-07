package main

import (
	"testing"
	"time"
)

func TestFilterByMaxFollowers(t *testing.T) {
	cats := []SummaryCategory{
		{
			Name: "AI",
			Targets: []SummaryTarget{
				{Handle: "small", Count: 2, Followers: 500},  // kept (<= max)
				{Handle: "exact", Count: 1, Followers: 1000}, // kept (== max)
				{Handle: "big", Count: 3, Followers: 50000},  // dropped (> max)
				{Handle: "unknown", Count: 1, Followers: 0},  // kept (unknown)
			},
		},
		{
			Name: "DeFi",
			Targets: []SummaryTarget{
				{Handle: "whale", Count: 5, Followers: 999999}, // dropped → category becomes empty
			},
		},
	}

	out := filterByMaxFollowers(cats, 1000)

	if len(out) != 1 {
		t.Fatalf("expected 1 category to remain (empty ones dropped), got %d", len(out))
	}
	if out[0].Name != "AI" {
		t.Fatalf("expected remaining category to be AI, got %q", out[0].Name)
	}

	kept := make(map[string]bool)
	for _, tgt := range out[0].Targets {
		kept[tgt.Handle] = true
	}
	wantKept := []string{"small", "exact", "unknown"}
	if len(out[0].Targets) != len(wantKept) {
		t.Fatalf("expected %d targets kept, got %d", len(wantKept), len(out[0].Targets))
	}
	for _, h := range wantKept {
		if !kept[h] {
			t.Errorf("expected target %q to be kept", h)
		}
	}
	if kept["big"] {
		t.Errorf("target 'big' (> max) should have been dropped")
	}
}

func TestLatestOldUsername(t *testing.T) {
	hist := []UsernameHistoryEntry{
		{OldUsername: "older", ChangedAt: "2024-01-01T00:00:00Z"},
		{OldUsername: "newest", ChangedAt: "2024-06-01T00:00:00Z"},
		{OldUsername: "middle", ChangedAt: "2024-03-01T00:00:00Z"},
	}
	if got := latestOldUsername(hist); got != "newest" {
		t.Errorf("expected most recent old username 'newest', got %q", got)
	}

	if got := latestOldUsername(nil); got != "" {
		t.Errorf("expected empty string for no history, got %q", got)
	}

	// Empty OldUsername entries are ignored.
	if got := latestOldUsername([]UsernameHistoryEntry{{OldUsername: "", ChangedAt: "2024-06-01T00:00:00Z"}}); got != "" {
		t.Errorf("expected empty string when all entries blank, got %q", got)
	}
}

func TestFrontrunCacheTTL(t *testing.T) {
	s := NewState()
	s.SetFrontrun("Foo", true, "oldfoo", 12)

	// Fresh entry is returned (case-insensitive handle).
	e, ok := s.GetFrontrun("foo", time.Hour)
	if !ok || !e.UsernameChanged || e.OldUsername != "oldfoo" || e.SmartFollowers != 12 {
		t.Fatalf("expected cached entry, got %+v ok=%v", e, ok)
	}

	// Expired entry is treated as a miss.
	if _, ok := s.GetFrontrun("foo", -time.Hour); ok {
		t.Errorf("expected expired entry to be a cache miss")
	}

	// Prune drops it.
	s.PruneFrontrun(-time.Hour)
	if _, ok := s.GetFrontrun("foo", time.Hour); ok {
		t.Errorf("expected entry to be pruned")
	}
}
