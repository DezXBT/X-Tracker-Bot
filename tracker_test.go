package main

import "testing"

func TestFilterByMaxFollowers(t *testing.T) {
	cats := []SummaryCategory{
		{
			Name: "AI",
			Targets: []SummaryTarget{
				{Handle: "small", Count: 2, Followers: 500},    // kept (<= max)
				{Handle: "exact", Count: 1, Followers: 1000},   // kept (== max)
				{Handle: "big", Count: 3, Followers: 50000},    // dropped (> max)
				{Handle: "unknown", Count: 1, Followers: 0},    // kept (unknown)
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
