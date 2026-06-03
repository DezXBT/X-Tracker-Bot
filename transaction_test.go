package main

import "testing"

// TestExtractIntegersStripsMinus locks in Bug #1: the SVG integer extraction
// must treat '-' as a separator (matching the reference re.sub(r"[^\d]+", ...)),
// so every parsed number is non-negative.
func TestExtractIntegersStripsMinus(t *testing.T) {
	cases := map[string][]int{
		"12 34 56":  {12, 34, 56},
		"12-34-56":  {12, 34, 56},
		"-7 8":      {7, 8},
		"4.5 6":     {4, 5, 6},
		"  ":        nil,
		"10C20 -30": {10, 20, 30},
	}
	for in, want := range cases {
		got := extractIntegers(in)
		if len(got) != len(want) {
			t.Fatalf("extractIntegers(%q) = %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("extractIntegers(%q) = %v, want %v", in, got, want)
			}
		}
	}
}

func TestFloatToHex(t *testing.T) {
	cases := map[float64]string{
		255.0: "FF",
		16.0:  "10",
		0.0:   "0",
		1.0:   "1",
		0.5:   "0.8",
	}
	for in, want := range cases {
		if got := floatToHex(in); got != want {
			t.Errorf("floatToHex(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestSolve(t *testing.T) {
	if got := solve(255, 60, 360, true); got != 360 {
		t.Errorf("solve(255,60,360,true) = %v, want 360", got)
	}
	if got := solve(0, 60, 360, true); got != 60 {
		t.Errorf("solve(0,60,360,true) = %v, want 60", got)
	}
}

func TestJSRound(t *testing.T) {
	cases := map[float64]int{0.5: 1, 2.4: 2, 2.5: 3, 1.49: 1}
	for in, want := range cases {
		if got := jsRound(in); got != want {
			t.Errorf("jsRound(%v) = %d, want %d", in, got, want)
		}
	}
}

// TestCubicBoundaries checks the ported boundary behaviour of Cubic.GetValue.
func TestCubicBoundaries(t *testing.T) {
	c := NewCubic([4]float64{0.42, 0.0, 0.58, 1.0}) // standard ease curve
	if got := c.GetValue(0.0); got != 0.0 {
		t.Errorf("GetValue(0) = %v, want 0", got)
	}
	if got := c.GetValue(1.0); got != 1.0 {
		t.Errorf("GetValue(1) = %v, want 1", got)
	}
	mid := c.GetValue(0.5)
	if mid <= 0.0 || mid >= 1.0 {
		t.Errorf("GetValue(0.5) = %v, want in (0,1)", mid)
	}
}

func TestFormatNumber(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"}
	for in, want := range cases {
		if got := formatNumber(in); got != want {
			t.Errorf("formatNumber(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeHandle(t *testing.T) {
	cases := map[string]string{
		"https://x.com/Foo":        "Foo",
		"https://twitter.com/@bar": "bar",
		"@baz":                     "baz",
		"plainHandle":              "plainHandle",
		"# a comment":              "",
		"":                         "",
		"not a handle!":            "",
	}
	for in, want := range cases {
		if got := normalizeHandle(in); got != want {
			t.Errorf("normalizeHandle(%q) = %q, want %q", in, got, want)
		}
	}
}
