package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TxGen is the global transaction ID generator, initialized once at startup.
var TxGen *TransactionGenerator

// TransactionGenerator produces X-Client-Transaction-Id header values
// by reverse-engineering the animation-based key derivation used by X/Twitter.
type TransactionGenerator struct {
	keyBytes        []int  // base64-decoded bytes from twitter-site-verification meta tag
	rowIndex        int    // first index extracted from ondemand JS
	keyBytesIndices []int  // remaining indices from ondemand JS
	animationKey    string // computed from SVG animation frames
	keyword         string // static keyword used in hash
	mu              sync.Mutex
	initialized     bool
}

// Init fetches x.com, parses the site verification key and animation frames,
// fetches the ondemand JS to extract byte indices, and computes the animation key.
// Thread-safe; subsequent calls are no-ops.
func Init() error {
	if TxGen != nil && TxGen.initialized {
		return nil
	}

	tg := &TransactionGenerator{keyword: "obfiowerehiring"}
	if err := tg.init(); err != nil {
		return err
	}

	TxGen = tg
	return nil
}

// Generate returns an X-Client-Transaction-Id value for the given HTTP method and path.
// Returns "" if TxGen is not initialized.
func Generate(method, path string) string {
	if TxGen == nil || !TxGen.initialized {
		return ""
	}
	return TxGen.generate(method, path)
}

// ──────────────────────────────────────────────────────────────────────────────
// Initialization
// ──────────────────────────────────────────────────────────────────────────────

func (tg *TransactionGenerator) init() error {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	if tg.initialized {
		return nil
	}

	// Step 1: Fetch x.com HTML
	html, err := fetchURL("https://x.com", map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36",
		"Accept-Language": "en-US,en;q=0.9",
		"Referer":         "https://x.com",
	})
	if err != nil {
		return fmt.Errorf("fetch x.com: %w", err)
	}

	// Step 2: Extract twitter-site-verification meta content (base64 key)
	key, err := extractKey(html)
	if err != nil {
		return fmt.Errorf("extract key: %w", err)
	}

	// Step 3: Extract ondemand.s JS file URL
	jsURL, err := extractOndemandURL(html)
	if err != nil {
		return fmt.Errorf("extract ondemand URL: %w", err)
	}

	// Step 4: Fetch the ondemand JS
	jsContent, err := fetchURL(jsURL, nil)
	if err != nil {
		return fmt.Errorf("fetch ondemand JS: %w", err)
	}

	// Step 5: Extract key_byte_indices from JS
	rowIdx, indices, err := extractKeyByteIndices(jsContent)
	if err != nil {
		return fmt.Errorf("extract key_byte_indices: %w", err)
	}
	tg.rowIndex = rowIdx
	tg.keyBytesIndices = indices

	// Step 6: Decode key to bytes
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("decode key: %w", err)
	}
	tg.keyBytes = make([]int, len(decoded))
	for i, b := range decoded {
		tg.keyBytes[i] = int(b)
	}

	// Step 7: Compute animation key
	animKey, err := tg.computeAnimationKey(html)
	if err != nil {
		return fmt.Errorf("compute animation key: %w", err)
	}
	tg.animationKey = animKey

	tg.initialized = true
	logInfo("X-Client-Transaction-Id generator initialized (animationKey=%s)", animKey)
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Extraction helpers
// ──────────────────────────────────────────────────────────────────────────────

// extractKey finds the twitter-site-verification meta tag content attribute.
func extractKey(html string) (string, error) {
	re := regexp.MustCompile(`<meta\s+name="twitter-site-verification"\s+content="([^"]+)"`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		return "", fmt.Errorf("twitter-site-verification meta tag not found")
	}
	return m[1], nil
}

// extractOndemandURL finds the ondemand.s JS bundle URL from the HTML.
func extractOndemandURL(html string) (string, error) {
	// First regex: find the index for "ondemand.s"
	re1 := regexp.MustCompile(`,(\d+):["']ondemand\.s["']`)
	m1 := re1.FindStringSubmatch(html)
	if len(m1) < 2 {
		return "", fmt.Errorf("ondemand.s index not found in HTML")
	}
	index := m1[1]

	// Second regex: find the hex hash for that index
	re2 := regexp.MustCompile(`,` + regexp.QuoteMeta(index) + `:"([0-9a-f]+)"`)
	m2 := re2.FindStringSubmatch(html)
	if len(m2) < 2 {
		return "", fmt.Errorf("ondemand.s hash not found for index %s", index)
	}
	hash := m2[1]

	return fmt.Sprintf("https://abs.twimg.com/responsive-web/client-web/ondemand.s.%sa.js", hash), nil
}

// extractKeyByteIndices parses the ondemand JS for byte index patterns.
// The regex matches patterns like (X[N], 16) where N is the index.
func extractKeyByteIndices(js string) (int, []int, error) {
	re := regexp.MustCompile(`\(\w{1}\[(\d{1,2})\],\s*16\)`)
	matches := re.FindAllStringSubmatch(js, -1)
	if len(matches) < 2 {
		return 0, nil, fmt.Errorf("expected at least 2 key_byte_indices matches, got %d", len(matches))
	}

	rowIndex, err := strconv.Atoi(matches[0][1])
	if err != nil {
		return 0, nil, fmt.Errorf("parse rowIndex: %w", err)
	}

	var indices []int
	for _, m := range matches[1:] {
		idx, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, nil, fmt.Errorf("parse index: %w", err)
		}
		indices = append(indices, idx)
	}

	return rowIndex, indices, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// SVG animation key computation
// ──────────────────────────────────────────────────────────────────────────────

// computeAnimationKey parses SVG animation frames from HTML and derives
// the animation key string used in the transaction ID hash.
func (tg *TransactionGenerator) computeAnimationKey(html string) (string, error) {
	svgFrames, err := parseSVGFrames(html)
	if err != nil {
		return "", fmt.Errorf("parse SVG frames: %w", err)
	}

	// All key-byte indices below come from remote (HTML/JS) data, so validate
	// every access to avoid an index-out-of-range panic during init.
	if len(tg.keyBytes) <= 5 {
		return "", fmt.Errorf("key bytes too short: %d", len(tg.keyBytes))
	}
	if tg.rowIndex >= len(tg.keyBytes) {
		return "", fmt.Errorf("row index %d out of range for %d key bytes", tg.rowIndex, len(tg.keyBytes))
	}

	frameIndex := tg.keyBytes[5] % 4
	selectedFrame, ok := svgFrames[frameIndex]
	if !ok {
		return "", fmt.Errorf("SVG frame %d not found", frameIndex)
	}

	rowIdx := tg.keyBytes[tg.rowIndex] % 16
	if rowIdx >= len(selectedFrame) {
		return "", fmt.Errorf("row index %d out of range (frame %d has %d rows)", rowIdx, frameIndex, len(selectedFrame))
	}

	// frameTime = product of (keyBytes[idx] % 16 for idx in keyBytesIndices)
	frameTime := 1
	for _, idx := range tg.keyBytesIndices {
		if idx >= len(tg.keyBytes) {
			return "", fmt.Errorf("key byte index %d out of range for %d key bytes", idx, len(tg.keyBytes))
		}
		frameTime *= tg.keyBytes[idx] % 16
	}

	// Apply JS Math.round behavior
	frameTimeF := jsRound(float64(frameTime)/10.0) * 10
	targetTime := float64(frameTimeF) / 4096.0

	frameRow := selectedFrame[rowIdx]
	// animate() reads frameRow[0..6] and frameRow[7:]; ensure it is long enough.
	if len(frameRow) < 7 {
		return "", fmt.Errorf("frame row %d has too few values: %d", rowIdx, len(frameRow))
	}

	return animate(frameRow, targetTime), nil
}

// parseSVGFrames extracts animation frame data from the HTML.
// Returns frames[0..3], each being the rows from one loading-x-anim-N SVG.
func parseSVGFrames(html string) (map[int][][]int, error) {
	allFrames := make(map[int][][]int)

	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("loading-x-anim-%d", i)
		svgRe := regexp.MustCompile(`id="` + id + `"[^>]*>(.*?)</svg>`)
		svgMatch := svgRe.FindStringSubmatch(html)
		if len(svgMatch) < 2 {
			continue
		}
		svgContent := svgMatch[1]

		// Find ALL path d attributes — first is logo, second is animation data
		pathRe := regexp.MustCompile(`d="([^"]+)"`)
		pathMatches := pathRe.FindAllStringSubmatch(svgContent, -1)
		if len(pathMatches) < 2 {
			continue
		}
		d := pathMatches[1][1] // second path = animation data

		if len(d) < 10 {
			continue
		}
		d = d[9:] // skip M command prefix
		segments := strings.Split(d, "C")

		var rows [][]int
		for _, seg := range segments {
			nums := extractIntegers(seg)
			rows = append(rows, nums)
		}
		allFrames[i] = rows
	}

	if len(allFrames) == 0 {
		return nil, fmt.Errorf("no SVG animation frames found")
	}

	return allFrames, nil
}

// extractIntegers extracts all integer values from a string segment.
//
// Matches the reference implementation (re.sub(r"[^\d]+", " ", item)): every
// non-digit character — including minus signs — is treated as a separator, so
// all parsed numbers are non-negative. Keeping minus signs here would change
// the frame data and produce a different (wrong) animation key.
func extractIntegers(s string) []int {
	re := regexp.MustCompile(`[^0-9]+`)
	cleaned := re.ReplaceAllString(s, " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil
	}

	parts := strings.Fields(cleaned)
	var nums []int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		nums = append(nums, n)
	}
	return nums
}

// ──────────────────────────────────────────────────────────────────────────────
// Animation math
// ──────────────────────────────────────────────────────────────────────────────

// animate computes the animation key string from frame data and target time.
func animate(frames []int, targetTime float64) string {
	fromColor := []float64{float64(frames[0]), float64(frames[1]), float64(frames[2]), 1.0}
	toColor := []float64{float64(frames[3]), float64(frames[4]), float64(frames[5]), 1.0}
	fromRotation := []float64{0.0}
	toRotation := []float64{solve(float64(frames[6]), 60.0, 360.0, true)}

	remainingFrames := frames[7:]
	curves := make([]float64, len(remainingFrames))
	isOddVals := []float64{0.0, -1.0, 0.0, -1.0}

	for i, v := range remainingFrames {
		oddVal := isOddVals[i%len(isOddVals)]
		curves[i] = solve(float64(v), oddVal, 1.0, false)
	}

	// Pad curves to at least 4 elements
	for len(curves) < 4 {
		curves = append(curves, 0.0)
	}

	cubic := NewCubic([4]float64{curves[0], curves[1], curves[2], curves[3]})
	val := cubic.GetValue(targetTime)

	color := interpolate(fromColor, toColor, val)
	rotation := interpolate(fromRotation, toRotation, val)
	matrix := convertRotationToMatrix(rotation[0])

	// Build string array
	var parts []string

	// First 3 color components as hex (clamp 0-255, then round).
	// The reference uses format(round(value), 'x') — Python's round() is
	// round-half-to-even, so use math.RoundToEven (not truncation) to match.
	for i := 0; i < 3; i++ {
		c := color[i]
		if c < 0 {
			c = 0
		}
		if c > 255 {
			c = 255
		}
		parts = append(parts, fmt.Sprintf("%x", int(math.RoundToEven(c))))
	}

	// 4 matrix values. The reference builds these as
	//   f"0{hex}".lower() if hex.startswith(".") else hex or "0"
	// where its float_to_hex omits the leading "0" for fractions. Our
	// floatToHex already includes that leading "0" (e.g. "0.1EB8…"), so the
	// net result is simply the lowercased hex — which is what the reference's
	// animation key ends up being.
	for _, m := range matrix {
		rounded := math.Round(m*100) / 100
		if rounded < 0 {
			rounded = -rounded // abs
		}
		hexStr := floatToHex(rounded)
		if hexStr == "" {
			hexStr = "0"
		}
		parts = append(parts, strings.ToLower(hexStr))
	}

	// Two trailing zeros
	parts = append(parts, "0", "0")

	// Join and remove dots and dashes
	result := strings.Join(parts, "")
	result = strings.ReplaceAll(result, ".", "")
	result = strings.ReplaceAll(result, "-", "")

	return result
}

// solve maps a 0-255 value to the range [minVal, maxVal].
// If rounding is true, floors the result; otherwise rounds to 2 decimal places.
func solve(value, minVal, maxVal float64, rounding bool) float64 {
	result := value*(maxVal-minVal)/255.0 + minVal
	if rounding {
		return math.Floor(result)
	}
	return math.Round(result*100) / 100
}

// floatToHex converts a float to a base-16 numeric representation (NOT IEEE 754).
// Example: 255 → "FF", 0.5 → "0.8", 16.0 → "10"
func floatToHex(x float64) string {
	intPart := int64(x)
	fracPart := x - float64(intPart)

	// Convert integer part
	var intStr string
	if intPart == 0 {
		intStr = "0"
	} else {
		for intPart > 0 {
			rem := intPart % 16
			if rem < 10 {
				intStr = string(rune('0'+rem)) + intStr
			} else {
				intStr = string(rune('A'+rem-10)) + intStr
			}
			intPart /= 16
		}
	}

	if fracPart < 1e-10 {
		return intStr
	}

	// Convert fractional part
	var fracStr string
	for i := 0; i < 16 && fracPart > 1e-10; i++ {
		fracPart *= 16
		digit := int64(fracPart)
		fracPart -= float64(digit)
		if digit < 10 {
			fracStr += string(rune('0' + digit))
		} else {
			fracStr += string(rune('A' + digit - 10))
		}
	}

	return intStr + "." + fracStr
}

// interpolate performs linear interpolation between two vectors.
func interpolate(from, to []float64, f float64) []float64 {
	result := make([]float64, len(from))
	for i := range from {
		result[i] = from[i]*(1-f) + to[i]*f
	}
	return result
}

// convertRotationToMatrix converts a rotation angle (degrees) to a 2D rotation matrix.
func convertRotationToMatrix(rotation float64) [4]float64 {
	rad := rotation * math.Pi / 180.0
	return [4]float64{math.Cos(rad), -math.Sin(rad), math.Sin(rad), math.Cos(rad)}
}

// ──────────────────────────────────────────────────────────────────────────────
// Cubic Bézier
// ──────────────────────────────────────────────────────────────────────────────

// Cubic implements a cubic Bézier easing curve.
type Cubic struct {
	curves [4]float64
}

// NewCubic creates a new cubic Bézier with the given control points.
func NewCubic(curves [4]float64) *Cubic {
	return &Cubic{curves: curves}
}

// GetValue returns the y value of the Bézier curve at the given time (x value).
//
// Ported from the reference Cubic.get_value: outside [0,1] it extrapolates
// using the start/end gradients (rather than clamping to 0/1), and inside it
// binary-searches for the parametric t with an early exit once x is within
// 1e-5 of the target. The iteration cap is a safety net against the float
// edge case where (start+end)/2 stops changing; it is high enough never to
// affect the result in practice.
func (c *Cubic) GetValue(t float64) float64 {
	startGradient := 0.0
	endGradient := 0.0
	start := 0.0
	mid := 0.0
	end := 1.0

	if t <= 0.0 {
		if c.curves[0] > 0.0 {
			startGradient = c.curves[1] / c.curves[0]
		} else if c.curves[1] == 0.0 && c.curves[2] > 0.0 {
			startGradient = c.curves[3] / c.curves[2]
		}
		return startGradient * t
	}

	if t >= 1.0 {
		if c.curves[2] < 1.0 {
			endGradient = (c.curves[3] - 1.0) / (c.curves[2] - 1.0)
		} else if c.curves[2] == 1.0 && c.curves[0] < 1.0 {
			endGradient = (c.curves[1] - 1.0) / (c.curves[0] - 1.0)
		}
		return 1.0 + endGradient*(t-1.0)
	}

	for i := 0; start < end && i < 100; i++ {
		mid = (start + end) / 2.0
		xEst := calcBezier(c.curves[0], c.curves[2], mid)
		if math.Abs(t-xEst) < 0.00001 {
			return calcBezier(c.curves[1], c.curves[3], mid)
		}
		if xEst < t {
			start = mid
		} else {
			end = mid
		}
	}

	return calcBezier(c.curves[1], c.curves[3], mid)
}

// calcBezier computes the cubic Bézier value for a single axis.
func calcBezier(a, b, m float64) float64 {
	return 3.0*a*(1-m)*(1-m)*m + 3.0*b*(1-m)*m*m + m*m*m
}

// ──────────────────────────────────────────────────────────────────────────────
// Transaction ID generation
// ──────────────────────────────────────────────────────────────────────────────

// generate produces the X-Client-Transaction-Id header value.
func (tg *TransactionGenerator) generate(method, path string) string {
	timeNow := time.Now().Unix() - 1682924400

	// 4 bytes little-endian
	timeNowBytes := make([]byte, 4)
	for i := 0; i < 4; i++ {
		timeNowBytes[i] = byte((timeNow >> (i * 8)) & 0xFF)
	}

	// SHA-256 hash of the input string
	shaInput := fmt.Sprintf("%s!%s!%d%s%s", method, path, timeNow, tg.keyword, tg.animationKey)
	shaHash := sha256.Sum256([]byte(shaInput))

	// Build payload: keyBytes + timeNowBytes + first 16 bytes of SHA hash + constant 3
	payload := make([]int, 0, len(tg.keyBytes)+4+16+1)
	for _, b := range tg.keyBytes {
		payload = append(payload, b)
	}
	for _, b := range timeNowBytes {
		payload = append(payload, int(b))
	}
	for i := 0; i < 16; i++ {
		payload = append(payload, int(shaHash[i]))
	}
	payload = append(payload, 3) // additional random number constant

	// XOR with random byte
	randomByte := make([]byte, 1)
	if _, err := rand.Read(randomByte); err != nil {
		randomByte[0] = byte(time.Now().UnixNano())
	}
	rb := int(randomByte[0])

	encoded := make([]byte, len(payload)+1)
	encoded[0] = byte(rb)
	for i, p := range payload {
		encoded[i+1] = byte(p ^ rb)
	}

	// Base64 encode and strip trailing "="
	result := base64.StdEncoding.EncodeToString(encoded)
	result = strings.TrimRight(result, "=")

	return result
}

// ──────────────────────────────────────────────────────────────────────────────
// Utility functions
// ──────────────────────────────────────────────────────────────────────────────

// jsRound implements JavaScript's Math.round (0.5 rounds up, not banker's rounding).
func jsRound(num float64) int {
	x := math.Floor(num)
	if num-x >= 0.5 {
		x = math.Ceil(num)
	}
	return int(math.Copysign(x, num))
}

// fetchURL performs an HTTP GET with optional custom headers and returns the body as string.
func fetchURL(url string, headers map[string]string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	if headers != nil {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return string(body), nil
}
