package main

import (
	"testing"
	"time"
)

// helper: build an event at a given minute offset from base.
func ev(base time.Time, mins int, watcher, target, category, bio, createdAt string, followers int) Event {
	e := MakeEvent(watcher, target, bio, target, category, followers, "", createdAt)
	e.Ts = base.Add(time.Duration(mins) * time.Minute).UTC().Format(time.RFC3339)
	return e
}

func TestAggregateByCategoryCollectsTimesAndCreated(t *testing.T) {
	base := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	created := base.Add(-5 * 24 * time.Hour).Format(twitterTimeLayout)
	events := []Event{
		ev(base, 0, "alice", "proj", "AI", "we build ai", created, 500),
		ev(base, 3, "bob", "proj", "AI", "we build ai", created, 500),
		ev(base, 6, "carol", "proj", "AI", "we build ai", created, 500),
		// duplicate watcher should not double-count or add a second timestamp.
		ev(base, 50, "alice", "proj", "AI", "we build ai", created, 500),
	}
	cats := aggregateByCategory(events)
	if len(cats) != 1 || len(cats[0].Targets) != 1 {
		t.Fatalf("expected 1 category / 1 target, got %d cats", len(cats))
	}
	tgt := cats[0].Targets[0]
	if tgt.Count != 3 {
		t.Errorf("expected 3 distinct watchers, got %d", tgt.Count)
	}
	if len(tgt.FollowTimes) != 3 {
		t.Errorf("expected 3 distinct-watcher timestamps, got %d", len(tgt.FollowTimes))
	}
	if tgt.createdAt != created {
		t.Errorf("expected createdAt carried, got %q", tgt.createdAt)
	}
}

func TestComputeSignals(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	base := now.Add(-time.Hour)
	created := now.Add(-10 * 24 * time.Hour).Format(twitterTimeLayout) // 10 days -> fresh
	cats := []SummaryCategory{
		{
			Name: "AI",
			Targets: []SummaryTarget{
				{
					Handle: "proj",
					Count:  3,
					Bio:    "join now 0x1234567890abcdef1234567890abcdef12345678",
					FollowTimes: []time.Time{
						base, base.Add(2 * time.Minute), base.Add(5 * time.Minute),
					},
					createdAt: created,
				},
			},
		},
	}
	computeSignals(cats, now, 3, 30*time.Minute, 30)
	tgt := cats[0].Targets[0]

	if !tgt.IsBurst || tgt.BurstSpan != 5*time.Minute {
		t.Errorf("expected burst span=5m, got burst=%v span=%s", tgt.IsBurst, tgt.BurstSpan)
	}
	if tgt.FirstSeen != base || tgt.LastSeen != base.Add(5*time.Minute) {
		t.Errorf("first/last seen wrong: first=%s last=%s", tgt.FirstSeen, tgt.LastSeen)
	}
	if tgt.AgeDays != 10 || !tgt.IsFresh {
		t.Errorf("expected age 10d fresh, got days=%d fresh=%v", tgt.AgeDays, tgt.IsFresh)
	}
	if tgt.ContractAddr != "0x1234567890abcdef1234567890abcdef12345678" || tgt.ContractChain != "evm" {
		t.Errorf("expected EVM contract detected, got %q/%q", tgt.ContractAddr, tgt.ContractChain)
	}
}

func TestComputeSignalsNoBurstWhenSpread(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	base := now.Add(-2 * time.Hour)
	cats := []SummaryCategory{
		{Name: "AI", Targets: []SummaryTarget{{
			Handle: "slow", Count: 3,
			FollowTimes: []time.Time{base, base.Add(40 * time.Minute), base.Add(90 * time.Minute)},
		}}},
	}
	computeSignals(cats, now, 3, 30*time.Minute, 30)
	if cats[0].Targets[0].IsBurst {
		t.Errorf("expected no burst when follows are spread beyond window")
	}
}

func TestMarkMutuals(t *testing.T) {
	s := NewState()
	s.MarkSummarized("recurring")
	cats := []SummaryCategory{
		{Name: "AI", Targets: []SummaryTarget{
			{Handle: "recurring"},
			{Handle: "brandnew"},
		}},
	}
	markMutuals(cats, s, time.Hour)
	if !cats[0].Targets[0].Mutual {
		t.Errorf("expected 'recurring' to be flagged mutual")
	}
	if cats[0].Targets[1].Mutual {
		t.Errorf("expected 'brandnew' NOT flagged mutual")
	}
}

func TestFilterMutedCategories(t *testing.T) {
	cats := []SummaryCategory{
		{Name: "Meme", Targets: []SummaryTarget{{Handle: "doge"}}},
		{Name: "AI", Targets: []SummaryTarget{{Handle: "proj"}}},
		{Name: "NFT", Targets: []SummaryTarget{{Handle: "ape"}}},
	}
	muted := map[string]bool{"meme": true, "nft": true}
	out := filterMutedCategories(cats, muted)
	if len(out) != 1 || out[0].Name != "AI" {
		t.Fatalf("expected only AI to remain, got %+v", out)
	}

	// Empty mute set is a no-op.
	if got := filterMutedCategories(cats, nil); len(got) != 3 {
		t.Errorf("expected no-op for empty mute set, got %d", len(got))
	}
}

func TestBuildActionLinks(t *testing.T) {
	// With contract -> chart link + .first command.
	links := buildActionLinks("proj", "0xabc")
	if len(links) != 2 {
		t.Fatalf("expected 2 links with contract, got %d: %v", len(links), links)
	}
	if links[0] != "[📈 chart](https://dexscreener.com/search?q=0xabc)" {
		t.Errorf("unexpected chart link: %q", links[0])
	}
	if links[1] != "`.first @proj`" {
		t.Errorf("unexpected first command: %q", links[1])
	}

	// Without contract -> only .first.
	links = buildActionLinks("proj", "")
	if len(links) != 1 || links[0] != "`.first @proj`" {
		t.Errorf("expected only .first link, got %v", links)
	}

	// No handle -> empty.
	if links = buildActionLinks("", ""); len(links) != 0 {
		t.Errorf("expected no links for empty handle, got %v", links)
	}
}

func TestTopTargets(t *testing.T) {
	cats := []SummaryCategory{
		{Name: "AI", Targets: []SummaryTarget{
			{Handle: "a", Count: 2, Followers: 100},
			{Handle: "b", Count: 5, Followers: 50},
		}},
		{Name: "DeFi", Targets: []SummaryTarget{
			{Handle: "c", Count: 5, Followers: 900},
			{Handle: "d", Count: 1, Followers: 10},
		}},
	}
	top := topTargets(cats, 3)
	if len(top) != 3 {
		t.Fatalf("expected top 3, got %d", len(top))
	}
	// c (count 5, followers 900) > b (count 5, followers 50) > a (count 2).
	if top[0].Handle != "c" || top[1].Handle != "b" || top[2].Handle != "a" {
		t.Errorf("unexpected ranking: %s,%s,%s", top[0].Handle, top[1].Handle, top[2].Handle)
	}

	// n=0 returns everything.
	if all := topTargets(cats, 0); len(all) != 4 {
		t.Errorf("expected all 4 when n=0, got %d", len(all))
	}
}

func TestAlertedTargetsState(t *testing.T) {
	s := NewState()
	if s.WasAlerted("proj", time.Hour) {
		t.Errorf("expected not-yet-alerted to be false")
	}
	s.MarkAlerted("Proj")
	if !s.WasAlerted("proj", time.Hour) {
		t.Errorf("expected case-insensitive alerted hit")
	}
	if s.WasAlerted("proj", -time.Hour) {
		t.Errorf("expected expired alert to be a miss")
	}
	s.PruneAlerted(-time.Hour)
	if s.WasAlerted("proj", time.Hour) {
		t.Errorf("expected pruned alert entry to be gone")
	}
}
