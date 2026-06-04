package main

import "testing"

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
		got := c.classifyKeyword(tc.name, tc.screen, tc.bio)
		if got != tc.want {
			t.Errorf("classifyKeyword(%q,%q,%q) = %q, want %q", tc.name, tc.screen, tc.bio, got, tc.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	c := newTestCategorizer()
	cases := []struct{ in, want string }{
		{"AI", "AI"},
		{"ai", "AI"},          // snaps to canonical casing
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

func TestAggregateByCategory(t *testing.T) {
	events := []Event{
		{Watcher: "alice", Target: "awp_project", TargetLower: "awp_project", Category: "AI"},
		{Watcher: "bob", Target: "awp_project", TargetLower: "awp_project", Category: "AI"},
		{Watcher: "bob", Target: "awp_project", TargetLower: "awp_project", Category: "AI"}, // dup watcher
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
	if cats[1].Name != "Layer 2" || cats[1].Targets[0].Count != 1 {
		t.Errorf("unexpected second category: %+v", cats[1])
	}
}
