package main

import (
	"testing"
	"time"
)

func newTestCategorizer() *Categorizer {
	return &Categorizer{
		enabled:    true,
		categories: defaultCategories,
	}
}

func TestClassifyKeyword(t *testing.T) {
	c := newTestCategorizer()
	cases := []struct {
		name, screen, bio, want string
	}{
		{"Some NFT", "nftguy", "we mint pfp collections", "NFT"},
		{"BaseChain", "baseku", "the best layer 2 rollup", "Layer 2"},
		{"AI Labs", "ailabs", "building autonomous ai agents", "AI"},
		{"DeFi Pro", "defipro", "perp dex with yield", "DeFi"},
		{"Random", "randomguy", "just a person", ""},
	}
	for _, tc := range cases {
		got := c.classifyKeyword(tc.name, tc.screen, tc.bio, "")
		if got != tc.want {
			t.Errorf("classifyKeyword(%q,%q,%q) = %q, want %q", tc.name, tc.screen, tc.bio, got, tc.want)
		}
	}
}

func TestClassifyKeywordUsesTweets(t *testing.T) {
	c := newTestCategorizer()
	// No signal in name/screen/bio, but tweets mention gaming.
	got := c.classifyKeyword("Foo", "foo", "", "just launched our gamefi season")
	if got != "Gaming" {
		t.Errorf("expected Gaming from tweets, got %q", got)
	}
}

func TestClassifyKeywordIgnoresUsername(t *testing.T) {
	c := newTestCategorizer()
	// A handle like "imho_nft" must NOT force NFT — the username is not matched.
	if got := c.classifyKeyword("imho", "imho_nft", "", ""); got == "NFT" {
		t.Errorf("username should not drive category, got %q", got)
	}
	// "jpeg" in a handle must not trigger anything either.
	if got := c.classifyKeyword("", "h1jpeg", "", ""); got != "" {
		t.Errorf("expected no keyword match from username, got %q", got)
	}
}

func TestClassifyKeywordTrading(t *testing.T) {
	c := newTestCategorizer()
	if got := c.classifyKeyword("", "", "daily trading signals", ""); got != "Trading" {
		t.Errorf("expected Trading, got %q", got)
	}
}

func TestMaybeResetCategoryCache(t *testing.T) {
	s := NewState()
	s.SetCachedCategory("alice", "DeFi")
	s.SetCachedCategory("bob", "NFT")

	// Different version → clears and reports count.
	if n := s.MaybeResetCategoryCache(1); n != 2 {
		t.Fatalf("first reset: cleared %d, want 2", n)
	}
	if len(s.CategoryCache) != 0 {
		t.Fatalf("cache not cleared: %d entries", len(s.CategoryCache))
	}
	// Same version → no-op.
	s.SetCachedCategory("carol", "Meme")
	if n := s.MaybeResetCategoryCache(1); n != 0 {
		t.Fatalf("second reset (same version): cleared %d, want 0", n)
	}
	if _, ok := s.GetCachedCategory("carol", time.Hour); !ok {
		t.Fatalf("entry wrongly cleared on same-version reset")
	}
}

func TestNormalize(t *testing.T) {
	c := newTestCategorizer()
	cases := []struct{ in, want string }{
		{"AI", "AI"},
		{"ai", "AI"}, // snaps to canonical casing
		{"\"Layer 2\"", "Layer 2"},
		{"Gaming.", "Gaming"},
		{"SocialFi", "SocialFi"}, // new category proposed by model
		{"I cannot determine the category for this account based on the information", ""}, // junk
		{"", ""},
	}
	for _, tc := range cases {
		if got := c.normalize(tc.in); got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFilterUnsummarized(t *testing.T) {
	cats := []SummaryCategory{
		{Name: "AI", Targets: []SummaryTarget{{Handle: "awp_project", Count: 3}, {Handle: "newai", Count: 1}}},
		{Name: "Layer 2", Targets: []SummaryTarget{{Handle: "baseku", Count: 2}}},
	}
	state := NewState()
	state.MarkSummarized("awp_project") // already reported before
	state.MarkSummarized("baseku")      // already reported before

	ttl := 30 * 24 * time.Hour
	filtered, newTargets := filterUnsummarized(cats, state, ttl)

	// Only "newai" is new; the AI category survives, Layer 2 drops entirely.
	if len(newTargets) != 1 || newTargets[0] != "newai" {
		t.Fatalf("expected [newai], got %v", newTargets)
	}
	if len(filtered) != 1 || filtered[0].Name != "AI" || len(filtered[0].Targets) != 1 {
		t.Fatalf("expected only AI/newai kept, got %+v", filtered)
	}

	// After marking newai, a re-run yields nothing.
	state.MarkSummarized("newai")
	_, newTargets2 := filterUnsummarized(cats, state, ttl)
	if len(newTargets2) != 0 {
		t.Errorf("expected no new targets on re-run, got %v", newTargets2)
	}
}

func TestAggregateByCategory(t *testing.T) {
	events := []Event{
		{Watcher: "alice", Target: "awp_project", TargetLower: "awp_project", Category: "AI", Bio: "old bio"},
		{Watcher: "bob", Target: "awp_project", TargetLower: "awp_project", Category: "AI", Bio: "newest bio"},
		{Watcher: "bob", Target: "awp_project", TargetLower: "awp_project", Category: "AI"}, // dup watcher, empty bio must not clobber
		{Watcher: "carol", Target: "baseku", TargetLower: "baseku", Category: "Layer 2"},
	}
	cats := aggregateByCategory(events)

	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(cats))
	}
	// AI has more total activity, should sort first.
	if cats[0].Name != "AI" {
		t.Errorf("expected AI first, got %q", cats[0].Name)
	}
	if cats[0].Targets[0].Handle != "awp_project" || cats[0].Targets[0].Count != 2 {
		t.Errorf("expected awp_project count=2 (distinct watchers), got %+v", cats[0].Targets[0])
	}
	// Latest non-empty bio wins; a later empty bio must not clobber it.
	if cats[0].Targets[0].Bio != "newest bio" {
		t.Errorf("expected latest bio %q, got %q", "newest bio", cats[0].Targets[0].Bio)
	}
	if cats[1].Name != "Layer 2" || cats[1].Targets[0].Count != 1 {
		t.Errorf("unexpected second category: %+v", cats[1])
	}
}
