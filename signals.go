package main

import (
	"regexp"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Signal helpers: pure functions that turn raw follow data into the fast-scan
// markers shown in the summary (heat, burst, account age, contract address,
// mutual overlap). Kept side-effect-free so they are trivially unit-testable.
// ──────────────────────────────────────────────────────────────────────────────

// heatLevel maps the number of distinct watchlist accounts that followed a
// target within the window to a fire-emoji urgency marker. More watchers piling
// into the same account in one window is the strongest "look at this now" signal.
//
//	>= 5 watchers -> 🔥🔥🔥 (very hot consensus)
//	3-4 watchers  -> 🔥🔥
//	2 watchers    -> 🔥
//	<= 1 watcher  -> "" (no marker; a single follow isn't consensus)
func heatLevel(count int) string {
	switch {
	case count >= 5:
		return "🔥🔥🔥"
	case count >= 3:
		return "🔥🔥"
	case count >= 2:
		return "🔥"
	default:
		return ""
	}
}

// burstWindowFor returns the tightest time span that still contains at least
// minFollows of the given timestamps, plus whether that qualifies as a burst
// (span <= maxSpan and at least minFollows events). A burst means several
// watchers followed the same account in quick succession — usually a sign
// something is being shared in DMs/alpha groups right now.
//
// timestamps need not be sorted; a copy is sorted internally.
func burstWindowFor(timestamps []time.Time, minFollows int, maxSpan time.Duration) (time.Duration, bool) {
	if minFollows < 2 || len(timestamps) < minFollows {
		return 0, false
	}
	ts := make([]time.Time, len(timestamps))
	copy(ts, timestamps)
	sortTimes(ts)

	// Sliding window of width minFollows over the sorted timestamps; track the
	// smallest span of any such window.
	best := time.Duration(1<<63 - 1)
	for i := 0; i+minFollows-1 < len(ts); i++ {
		span := ts[i+minFollows-1].Sub(ts[i])
		if span < best {
			best = span
		}
	}
	if best <= maxSpan {
		return best, true
	}
	return best, false
}

// sortTimes sorts a slice of time.Time ascending (insertion sort — slices here
// are tiny, a few follows per target per window).
func sortTimes(ts []time.Time) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Before(ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// twitterTimeLayout is X's legacy created_at format, e.g.
// "Wed Oct 10 20:19:24 +0000 2018".
const twitterTimeLayout = "Mon Jan 02 15:04:05 -0700 2006"

// accountAge parses an X legacy created_at string and returns the account age in
// days plus a compact human label ("3d", "5mo", "2y"). On an unparseable or
// empty input it returns (0, "") so callers can simply skip the marker.
func accountAge(createdAt string, now time.Time) (int, string) {
	createdAt = strings.TrimSpace(createdAt)
	if createdAt == "" {
		return 0, ""
	}
	t, err := time.Parse(twitterTimeLayout, createdAt)
	if err != nil {
		return 0, ""
	}
	days := int(now.Sub(t).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, humanizeAge(days)
}

// humanizeAge renders a day count compactly: days < 60 -> "Nd",
// < 730 -> "Nmo", else "Ny".
func humanizeAge(days int) string {
	switch {
	case days < 60:
		return itoa(days) + "d"
	case days < 730:
		return itoa(days/30) + "mo"
	default:
		return itoa(days/365) + "y"
	}
}

// itoa is a tiny strconv.Itoa replacement kept local to avoid widening imports
// in callers that already work in this file's vocabulary.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// isYoungAccount reports whether an account younger than maxDays should be
// flagged as "fresh" (a new account that already attracts watchlist follows is
// an early-opportunity signal). maxDays <= 0 disables the flag.
func isYoungAccount(days, maxDays int) bool {
	if maxDays <= 0 {
		return false
	}
	return days > 0 && days <= maxDays
}

// ──────────────────────────────────────────────────────────────────────────────
// Contract-address auto-detection
// ──────────────────────────────────────────────────────────────────────────────

// evmAddrRe matches a 40-hex EVM contract/wallet address. Word-ish boundaries on
// both sides avoid pulling 40-hex substrings out of longer hex blobs.
var evmAddrRe = regexp.MustCompile(`(?:^|[^0-9a-fA-Fx])(0x[0-9a-fA-F]{40})(?:$|[^0-9a-fA-F])`)

// solAddrRe matches a base58 string 32-44 chars long (the Solana address shape).
// Base58 excludes 0, O, I, l to avoid visual ambiguity.
var solAddrRe = regexp.MustCompile(`(?:^|[^1-9A-HJ-NP-Za-km-z])([1-9A-HJ-NP-Za-km-z]{32,44})(?:$|[^1-9A-HJ-NP-Za-km-z])`)

// detectContract scans free text (bio, pinned tweet, …) for the first contract
// address it can find and returns the address plus a chain hint ("evm" or
// "sol"). EVM is tried first because its 0x-prefixed 40-hex shape is far less
// ambiguous than base58. Returns ("","") when nothing matches.
//
// The Solana matcher is deliberately conservative: it only fires when the
// candidate is flanked by an explicit CA cue ("ca", "contract", "address",
// "$TICKER", or a "pump"/"bonk" suffix) somewhere in the text, because a bare
// 32-44 base58 run is too easy to hit by accident (hashes, URLs, IDs).
func detectContract(text string) (addr, chain string) {
	if text == "" {
		return "", ""
	}
	if m := evmAddrRe.FindStringSubmatch(text); m != nil {
		return m[1], "evm"
	}
	if hasContractCue(text) {
		if m := solAddrRe.FindStringSubmatch(text); m != nil {
			cand := m[1]
			// Reject all-lowercase or all-digit runs that are almost certainly
			// not addresses (Solana addresses mix case and digits).
			if looksLikeBase58Address(cand) {
				return cand, "sol"
			}
		}
	}
	return "", ""
}

// hasContractCue reports whether the text contains a hint that a contract
// address is present, used to gate the noisier Solana matcher.
func hasContractCue(text string) bool {
	l := strings.ToLower(text)
	cues := []string{"ca:", "ca ", "contract", "address", "pump", "bonk", "$"}
	for _, c := range cues {
		if strings.Contains(l, c) {
			return true
		}
	}
	return false
}

// looksLikeBase58Address applies cheap heuristics to weed out base58 matches
// that are unlikely to be real addresses: it requires at least one digit and a
// mix of upper and lower case.
func looksLikeBase58Address(s string) bool {
	var hasDigit, hasUpper, hasLower bool
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		}
	}
	return hasDigit && hasUpper && hasLower
}

// dexScreenerURL builds a DexScreener search link for a detected contract
// address. The /search?q= endpoint resolves across every chain DexScreener
// indexes, so we don't need to know the exact chain for EVM hits.
func dexScreenerURL(addr string) string {
	if addr == "" {
		return ""
	}
	return "https://dexscreener.com/search?q=" + addr
}
