package main

import (
	"testing"
	"time"
)

func TestHeatLevel(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{0, ""},
		{1, ""},
		{2, "🔥"},
		{3, "🔥🔥"},
		{4, "🔥🔥"},
		{5, "🔥🔥🔥"},
		{12, "🔥🔥🔥"},
	}
	for _, c := range cases {
		if got := heatLevel(c.count); got != c.want {
			t.Errorf("heatLevel(%d) = %q, want %q", c.count, got, c.want)
		}
	}
}

func TestBurstWindowFor(t *testing.T) {
	base := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	mk := func(mins ...int) []time.Time {
		ts := make([]time.Time, len(mins))
		for i, m := range mins {
			ts[i] = base.Add(time.Duration(m) * time.Minute)
		}
		return ts
	}

	// 3 follows within 10 min, threshold 3, span 30m -> burst (span 10m).
	span, ok := burstWindowFor(mk(0, 5, 10), 3, 30*time.Minute)
	if !ok || span != 10*time.Minute {
		t.Errorf("expected burst span=10m ok=true, got span=%s ok=%v", span, ok)
	}

	// Same follows but maxSpan 5m -> not a burst (tightest 3-window is 10m).
	if _, ok := burstWindowFor(mk(0, 5, 10), 3, 5*time.Minute); ok {
		t.Errorf("expected no burst when span exceeds maxSpan")
	}

	// Unsorted input still finds the tight cluster: 3 within 4 min.
	span, ok = burstWindowFor(mk(100, 2, 50, 0, 4), 3, 10*time.Minute)
	if !ok || span != 4*time.Minute {
		t.Errorf("expected burst span=4m ok=true on unsorted input, got span=%s ok=%v", span, ok)
	}

	// Fewer events than threshold -> never a burst.
	if _, ok := burstWindowFor(mk(0, 1), 3, time.Hour); ok {
		t.Errorf("expected no burst with fewer events than threshold")
	}

	// Threshold < 2 is rejected.
	if _, ok := burstWindowFor(mk(0, 1, 2), 1, time.Hour); ok {
		t.Errorf("expected no burst when minFollows < 2")
	}

	// Exactly threshold events at the same instant -> span 0, burst.
	span, ok = burstWindowFor(mk(0, 0, 0), 3, time.Minute)
	if !ok || span != 0 {
		t.Errorf("expected burst span=0 ok=true for simultaneous follows, got span=%s ok=%v", span, ok)
	}
}

func TestAccountAge(t *testing.T) {
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)

	// 3 days old.
	created := now.Add(-3 * 24 * time.Hour).Format(twitterTimeLayout)
	days, label := accountAge(created, now)
	if days != 3 || label != "3d" {
		t.Errorf("expected 3d, got days=%d label=%q", days, label)
	}

	// ~3 months -> "3mo".
	created = now.Add(-90 * 24 * time.Hour).Format(twitterTimeLayout)
	days, label = accountAge(created, now)
	if label != "3mo" {
		t.Errorf("expected 3mo for 90d account, got %q (days=%d)", label, days)
	}

	// ~2 years -> "2y".
	created = now.Add(-2 * 365 * 24 * time.Hour).Format(twitterTimeLayout)
	_, label = accountAge(created, now)
	if label != "2y" {
		t.Errorf("expected 2y, got %q", label)
	}

	// Real X-format string parses.
	days, label = accountAge("Wed Oct 10 20:19:24 +0000 2018", now)
	if days <= 0 || label == "" {
		t.Errorf("expected a positive age for a real created_at, got days=%d label=%q", days, label)
	}

	// Empty / unparseable -> zero value, no marker.
	if d, l := accountAge("", now); d != 0 || l != "" {
		t.Errorf("expected (0,\"\") for empty input, got (%d,%q)", d, l)
	}
	if d, l := accountAge("not a date", now); d != 0 || l != "" {
		t.Errorf("expected (0,\"\") for garbage input, got (%d,%q)", d, l)
	}
}

func TestHumanizeAge(t *testing.T) {
	cases := []struct {
		days int
		want string
	}{
		{0, "0d"},
		{1, "1d"},
		{59, "59d"},
		{60, "2mo"},
		{90, "3mo"},
		{729, "24mo"},
		{730, "2y"},
		{1095, "3y"},
	}
	for _, c := range cases {
		if got := humanizeAge(c.days); got != c.want {
			t.Errorf("humanizeAge(%d) = %q, want %q", c.days, got, c.want)
		}
	}
}

func TestIsYoungAccount(t *testing.T) {
	if !isYoungAccount(5, 30) {
		t.Errorf("expected 5d account to be young (max 30)")
	}
	if isYoungAccount(45, 30) {
		t.Errorf("expected 45d account to NOT be young (max 30)")
	}
	if !isYoungAccount(30, 30) {
		// boundary: 30 <= 30 is young
		t.Errorf("expected exactly-30d account to be young (max 30)")
	}
	if isYoungAccount(0, 30) {
		t.Errorf("expected age 0 (unknown) to NOT be flagged young")
	}
	if isYoungAccount(5, 0) {
		t.Errorf("expected maxDays=0 to disable the young flag")
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 5: "5", 42: "42", -7: "-7", 1000: "1000"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectContractEVM(t *testing.T) {
	addr := "0x1234567890abcdef1234567890abcdef12345678"
	cases := []string{
		"buy now " + addr + " to the moon",
		addr,
		"CA: " + addr,
		"contract " + addr + " pump",
	}
	for _, text := range cases {
		got, chain := detectContract(text)
		if got != addr || chain != "evm" {
			t.Errorf("detectContract(%q) = (%q,%q), want (%q,evm)", text, got, chain, addr)
		}
	}

	// A 40-hex inside a longer hex blob should NOT match (boundary guard).
	long := "0x1234567890abcdef1234567890abcdef1234567890ab"
	if got, _ := detectContract(long); got != "" {
		t.Errorf("expected no EVM match inside a longer hex blob, got %q", got)
	}
}

func TestDetectContractSolana(t *testing.T) {
	// Realistic-looking Solana address (mixed case + digits), 43 chars.
	sol := "3yYuW2UjLNYki79HSGj37XUMAyLkr6kxFwkLBYypHEgq"

	// Without a cue, the conservative matcher stays silent.
	if got, _ := detectContract(sol); got != "" {
		t.Errorf("expected no Solana match without a contract cue, got %q", got)
	}

	// With a cue it fires.
	got, chain := detectContract("CA: " + sol + " pump")
	if got != sol || chain != "sol" {
		t.Errorf("detectContract with cue = (%q,%q), want (%q,sol)", got, chain, sol)
	}

	// All-lowercase base58-ish run is rejected even with a cue.
	if got, _ := detectContract("contract abcdefghijkmnpqrstuvwxyzabcdefghijkmnpq address"); got != "" {
		t.Errorf("expected all-lowercase run to be rejected, got %q", got)
	}
}

func TestDetectContractEmpty(t *testing.T) {
	if got, chain := detectContract(""); got != "" || chain != "" {
		t.Errorf("expected empty result for empty text, got (%q,%q)", got, chain)
	}
	if got, chain := detectContract("just a normal bio with no address"); got != "" || chain != "" {
		t.Errorf("expected empty result for plain bio, got (%q,%q)", got, chain)
	}
}

func TestDexScreenerURL(t *testing.T) {
	if got := dexScreenerURL(""); got != "" {
		t.Errorf("expected empty url for empty addr, got %q", got)
	}
	addr := "0xabc"
	want := "https://dexscreener.com/search?q=0xabc"
	if got := dexScreenerURL(addr); got != want {
		t.Errorf("dexScreenerURL(%q) = %q, want %q", addr, got, want)
	}
}

func TestLooksLikeBase58Address(t *testing.T) {
	if !looksLikeBase58Address("3yYuW2UjLN9HSGj37") {
		t.Errorf("expected mixed-case-with-digit string to look like an address")
	}
	if looksLikeBase58Address("abcdefghij") {
		t.Errorf("expected all-lowercase to be rejected")
	}
	if looksLikeBase58Address("1234567890") {
		t.Errorf("expected all-digit to be rejected")
	}
	if looksLikeBase58Address("ABCDEFGHIJ") {
		t.Errorf("expected all-uppercase to be rejected")
	}
}
