package main

import (
	"strings"
	"testing"
	"time"
)

func TestHumanizeSpan(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{-time.Second, "0s"},
		{45 * time.Second, "45s"},
		{8 * time.Minute, "8m"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{2*time.Hour + 5*time.Minute, "2h5m"},
	}
	for _, c := range cases {
		if got := humanizeSpan(c.d); got != c.want {
			t.Errorf("humanizeSpan(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestDigestRank(t *testing.T) {
	if digestRank(0) != "🥇" || digestRank(1) != "🥈" || digestRank(2) != "🥉" {
		t.Errorf("expected medals for top 3")
	}
	if got := digestRank(3); got != "` 4`" {
		t.Errorf("expected ` 4` for rank 4, got %q", got)
	}
}

func TestRenderTarget(t *testing.T) {
	tgt := SummaryTarget{
		Handle:      "proj",
		Count:       5,
		Followers:   1500,
		HeatMarker:  "🔥🔥🔥",
		IsBurst:     true,
		BurstSpan:   8 * time.Minute,
		IsFresh:     true,
		AgeLabel:    "10d",
		Mutual:      true,
		ActionLinks: []string{"[📈 chart](https://dexscreener.com/search?q=0xabc)", "`.first @proj`"},
		Bio:         "we are building",
	}
	var b strings.Builder
	renderTarget(&b, tgt)
	out := b.String()

	for _, want := range []string{"🔥🔥🔥", "`5×`", "[@proj](https://x.com/proj)", "⚡8m", "✨10d", "⭐", "👥 1.5K", "chart", ".first @proj", "we are building"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderTarget output missing %q\nfull: %s", want, out)
		}
	}
}

func TestRenderTargetMinimal(t *testing.T) {
	// A plain target with no signals should still render the core line.
	tgt := SummaryTarget{Handle: "plain", Count: 1}
	var b strings.Builder
	renderTarget(&b, tgt)
	out := b.String()
	if !strings.Contains(out, "`1×`") || !strings.Contains(out, "[@plain](https://x.com/plain)") {
		t.Errorf("expected core line for minimal target, got %q", out)
	}
	// No heat marker, no burst, no fresh.
	if strings.Contains(out, "🔥") || strings.Contains(out, "⚡") || strings.Contains(out, "✨") {
		t.Errorf("did not expect signal markers on a minimal target, got %q", out)
	}
}

func TestRenderTargetOldAccount(t *testing.T) {
	// Non-fresh account with an age label gets the 🕯️ marker, not ✨.
	tgt := SummaryTarget{Handle: "old", Count: 2, AgeLabel: "3y", IsFresh: false}
	var b strings.Builder
	renderTarget(&b, tgt)
	out := b.String()
	if !strings.Contains(out, "🕯️3y") {
		t.Errorf("expected 🕯️3y marker for old account, got %q", out)
	}
	if strings.Contains(out, "✨") {
		t.Errorf("did not expect ✨ on an old account, got %q", out)
	}
}
