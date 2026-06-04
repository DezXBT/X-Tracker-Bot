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
			"STEP 1 — Is this an individual PERSON or a PROJECT?\n"+
			"PERSON = a human's personal account: founder, builder, developer, marketer, "+
			"creator, influencer, commentator, trader, writer, \"anon\". Signals: first-person "+
			"voice (\"I build\", \"marketing for...\"), a personal name/face, a role at some org "+
			"(\"Core Member @x\", \"building @y\"), \"DM for collabs\".\n"+
			"PROJECT = a product, protocol, company, token, DAO, or official team account. "+
			"Signals: \"we/our\", \"the first...\", \"decentralized...\", a $TICKER, "+
			"\"mainnet/testnet\", a community or product being promoted.\n"+
			"The RECENT TWEETS reveal this strongly via WRITING STYLE: personal opinions, "+
			"replies, jokes, \"gm\", \"I/my\" => a PERSON; product announcements, \"we shipped\", "+
			"release notes, \"join our community\" => a PROJECT. How much they tweet about a "+
			"topic does NOT change this — a person who tweets about AI all day is still KOL.\n\n"+
			"STEP 2 — Pick the category:\n"+
			"- If it is a PERSON -> answer KOL. The topic does NOT matter: an individual who "+
			"works on AI, DeFi, gaming, etc. is still KOL, NOT that topic.\n"+
			"- If it is a PROJECT -> pick the topical category: "+
			"AI (AI/ML project or agent); Layer 1 / Layer 2 (blockchain / scaling); "+
			"DeFi (dex/lending/perps/yield/stablecoin); NFT; Gaming/GameFi; Meme; DePIN; RWA; "+
			"Infra (oracle/bridge/node/tooling); Social; Trading (trading service/desk/signals); "+
			"Other (real crypto project fitting none above).\n\n"+
			"Judge from the BIO and RECENT TWEETS. The USERNAME is only a weak hint: words like "+
			"\"nft\", \"jpeg\", \"ai\", \"eth\" inside a handle do NOT decide the category.\n\n"+
			"DO NOT GUESS. If the bio and tweets do not clearly indicate a category — vague hype, "+
			"a countdown/teaser, \"loading...\", or just a $TICKER with no described product — "+
			"answer Other. A $TICKER cashtag alone does NOT mean DeFi; judge what the project "+
			"actually does, and if that is unclear, answer Other.\n\n"+
			"Examples:\n"+
			"- Bio \"marketing for ambitious founders @x | building @y\" -> KOL (a person)\n"+
			"- Bio \"AI Builder | daily AI coding tips, Core Member @club\" -> KOL (a person)\n"+
			"- Bio \"crypto comedy & shitposting\" -> KOL (a person)\n"+
			"- Bio \"Decentralized perpetuals exchange, up to 50x\" -> DeFi (a project)\n"+
			"- Bio \"The first zk-rollup scaling Ethereum\" -> Layer 2 (a project)\n"+
			"- Bio \"v4 loading... the gate is opening, $SV4 is waiting\" -> Other (vague teaser, no described product)\n\n"+
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
			{Role: "system", Content: "You categorize crypto X/Twitter accounts. First decide if it's an individual person or a project. An individual person is ALWAYS KOL, regardless of their topic (AI, DeFi, etc.); only projects get a topical category. Judge by bio and tweets, not the username. Output only a single category name."},
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
