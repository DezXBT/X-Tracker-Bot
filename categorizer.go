package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// UncategorizedLabel is used when no category can be determined.
const UncategorizedLabel = "Uncategorized"

// Categorizer assigns a project category to an X account. It primarily uses an
// OpenRouter LLM (rotating across multiple free-tier API keys and models on
// failure) and falls back to keyword matching when the LLM is unavailable.
type Categorizer struct {
	enabled    bool
	categories []string
	apiKeys    []string
	models     []string
	client     *http.Client
	keyIdx     int
	mu         sync.Mutex
}

func NewCategorizer(cfg *Config) *Categorizer {
	return &Categorizer{
		enabled:    cfg.CategorizationEnabled(),
		categories: cfg.Categorization.Categories,
		apiKeys:    cfg.Categorization.OpenRouter.APIKeys,
		models:     cfg.Categorization.OpenRouter.Models,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// nextKey returns the next OpenRouter API key in round-robin order.
func (c *Categorizer) nextKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.apiKeys) == 0 {
		return ""
	}
	k := c.apiKeys[c.keyIdx]
	c.keyIdx = (c.keyIdx + 1) % len(c.apiKeys)
	return k
}

// HasLLM reports whether the LLM path is usable (enabled with at least one key
// and model). When false, categorization relies on keyword matching only.
func (c *Categorizer) HasLLM() bool {
	return c.enabled && len(c.apiKeys) > 0 && len(c.models) > 0
}

// Categorize returns the best category for an account. It tries the LLM first,
// then keyword matching, and finally returns UncategorizedLabel. tweets is
// optional recent tweet text used as an extra signal.
func (c *Categorizer) Categorize(name, screenName, bio, tweets string) string {
	if !c.enabled {
		return UncategorizedLabel
	}
	if cat := c.classifyLLM(name, screenName, bio, tweets); cat != "" {
		return cat
	}
	if cat := c.classifyKeyword(name, screenName, bio, tweets); cat != "" {
		return cat
	}
	return UncategorizedLabel
}

// ──────────────────────────────────────────────────────────────────────────────
// LLM classification (OpenRouter)
// ──────────────────────────────────────────────────────────────────────────────

func (c *Categorizer) classifyLLM(name, screenName, bio, tweets string) string {
	if len(c.apiKeys) == 0 || len(c.models) == 0 {
		return ""
	}

	prompt := c.buildPrompt(name, screenName, bio, tweets)

	// Try each API key once; for each key walk the model list until one works.
	for attempt := 0; attempt < len(c.apiKeys); attempt++ {
		key := c.nextKey()
		for _, model := range c.models {
			cat, err := c.callOpenRouter(key, model, prompt)
			if err != nil {
				logWarn("[categorize] openrouter (%s) failed: %v", model, err)
				continue
			}
			if norm := c.normalize(cat); norm != "" {
				return norm
			}
		}
	}
	return ""
}

func (c *Categorizer) buildPrompt(name, screenName, bio, tweets string) string {
	if bio == "" {
		bio = "(no bio)"
	}
	tweetLine := "\nRecent tweets: (none)"
	if tweets != "" {
		tweetLine = fmt.Sprintf("\nRecent tweets: %s", tweets)
	}
	return fmt.Sprintf(
		"Classify this X (Twitter) crypto account into exactly ONE category.\n\n"+
			"Decide from the BIO and RECENT TWEETS — what the account is actually about.\n"+
			"The USERNAME is only a weak hint: words like \"nft\", \"jpeg\", \"ai\", \"eth\", "+
			"\"defi\" inside a handle do NOT decide the category.\n\n"+
			"Category guide:\n"+
			"- KOL: an individual PERSON — influencer, alpha caller, commentator, comedian, "+
			"trader personality, or someone's personal account (NOT a product/protocol).\n"+
			"- Trading: trading-focused service/desk/signal group/market analysis.\n"+
			"- AI: AI/ML projects or AI agents. Layer 1 / Layer 2: blockchains / scaling.\n"+
			"- DeFi: dex, lending, perps, yield, stablecoins. NFT: NFT collections/marketplaces.\n"+
			"- Gaming: games/GameFi. Meme: meme coin or meme-culture project.\n"+
			"- DePIN, RWA, Infra (tooling/oracle/bridge/node), Social: as named.\n"+
			"- Other: a real crypto account fitting none of the above.\n\n"+
			"If the account is a person rather than a project, choose KOL.\n"+
			"Allowed categories: %s.\n"+
			"Reply with ONLY the category name, nothing else.\n\n"+
			"Username: @%s\nName: %s\nBio: %s%s",
		strings.Join(c.categories, ", "), screenName, name, bio, tweetLine,
	)
}

type orRequest struct {
	Model       string      `json:"model"`
	Messages    []orMessage `json:"messages"`
	Temperature float64     `json:"temperature"`
	MaxTokens   int         `json:"max_tokens"`
}

type orMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type orResponse struct {
	Choices []struct {
		Message orMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Categorizer) callOpenRouter(apiKey, model, prompt string) (string, error) {
	reqBody := orRequest{
		Model: model,
		Messages: []orMessage{
			{Role: "system", Content: "You categorize crypto X/Twitter accounts. Distinguish individual people (KOL) from projects/protocols. Judge by bio and tweets, not the username. Output only a single category name."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0,
		MaxTokens:   20,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	// Optional attribution headers recommended by OpenRouter.
	req.Header.Set("HTTP-Referer", "https://github.com/DezXBT/X-Tracker-Bot")
	req.Header.Set("X-Title", "X-Tracker-Bot")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var parsed orResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode response (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != 200 {
		if parsed.Error != nil {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// normalize cleans an LLM answer and snaps it to the canonical category casing
// when it matches a known one. Returns "" if the answer looks unusable.
func (c *Categorizer) normalize(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Take the first line only, strip surrounding quotes/punctuation.
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(s, " \t\"'`.*•-")
	if s == "" {
		return ""
	}
	// Reject obviously bad answers (model refusing / explaining).
	if len(s) > 30 || len(strings.Fields(s)) > 3 {
		return ""
	}
	// Snap to canonical casing if it's a known category.
	for _, cat := range c.categories {
		if strings.EqualFold(cat, s) {
			return cat
		}
	}
	return s // a new category proposed by the model
}

// ──────────────────────────────────────────────────────────────────────────────
// Keyword fallback
// ──────────────────────────────────────────────────────────────────────────────

// keywordRule maps a regex (word-boundary matched, case-insensitive) to a
// canonical category. Earlier entries win, so order more specific rules first.
type keywordRule struct {
	re  *regexp.Regexp
	cat string
}

var keywordRules = func() []keywordRule {
	specs := []struct{ kw, cat string }{
		{`layer ?2`, "Layer 2"}, {`l2`, "Layer 2"}, {`rollup`, "Layer 2"}, {`zk`, "Layer 2"},
		{`layer ?1`, "Layer 1"}, {`l1`, "Layer 1"},
		{`depin`, "DePIN"},
		{`rwa`, "RWA"}, {`real world asset`, "RWA"},
		{`defi`, "DeFi"}, {`dex`, "DeFi"}, {`lending`, "DeFi"}, {`perp`, "DeFi"}, {`yield`, "DeFi"}, {`staking`, "DeFi"},
		{`nft`, "NFT"}, {`pfp`, "NFT"},
		{`gamefi`, "Gaming"}, {`gaming`, "Gaming"}, {`game`, "Gaming"},
		{`meme`, "Meme"},
		{`socialfi`, "Social"}, {`social`, "Social"},
		{`oracle`, "Infra"}, {`infra`, "Infra"}, {`node`, "Infra"}, {`bridge`, "Infra"},
		{`\bai\b`, "AI"}, {`artificial intelligence`, "AI"}, {`agent`, "AI"}, {`\bllm\b`, "AI"},
		{`trading`, "Trading"}, {`trader`, "Trading"}, {`signals?`, "Trading"},
	}
	rules := make([]keywordRule, 0, len(specs))
	for _, s := range specs {
		rules = append(rules, keywordRule{
			re:  regexp.MustCompile(`(?i)` + s.kw),
			cat: s.cat,
		})
	}
	return rules
}()

func (c *Categorizer) classifyKeyword(name, screenName, bio, tweets string) string {
	// Deliberately exclude the username: a handle like "imho_nft" or "h1jpeg"
	// would otherwise force a wrong category. Match on display name + bio + tweets.
	_ = screenName
	text := strings.ToLower(name + " " + bio + " " + tweets)
	for _, r := range keywordRules {
		if r.re.MatchString(text) {
			return r.cat
		}
	}
	return ""
}
