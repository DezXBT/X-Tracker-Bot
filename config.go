package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type CookiePair struct {
	AuthToken string `yaml:"auth_token"`
	Ct0       string `yaml:"ct0"`
}

type TrackingConfig struct {
	TrackAllFollows bool   `yaml:"track_all_follows"`
	PollInterval    string `yaml:"poll_interval"`
	PageSize        int    `yaml:"page_size"`
	MaxPages        int    `yaml:"max_pages"`
	PageDelay       string `yaml:"page_delay"`
	// DynamicQueryIDs, when true, refreshes GraphQL query IDs + feature flags
	// from x.com at startup. Off by default: the built-in IDs are proven to
	// work, and newer IDs can require feature flags that break scanning.
	DynamicQueryIDs bool `yaml:"dynamic_query_ids"`
}

type DiscordConfig struct {
	RawWebhooks     []string `yaml:"raw_webhooks"`
	SummaryWebhook  string   `yaml:"summary_webhook,omitempty"`
	SummaryInterval string   `yaml:"summary_interval,omitempty"`
	// SummaryDedupTTL is how long a target stays excluded from future summaries
	// after first appearing in one (default 30 days).
	SummaryDedupTTL string `yaml:"summary_dedup_ttl,omitempty"`
	// SummaryMaxFollowersEnabled toggles filtering the summary by follower count.
	SummaryMaxFollowersEnabled *bool `yaml:"summary_max_followers_enabled,omitempty"`
	// SummaryMaxFollowers is the max follower count a target may have to appear
	// in the summary (only applied when the filter is enabled).
	SummaryMaxFollowers int `yaml:"summary_max_followers,omitempty"`
}

type OpenRouterConfig struct {
	APIKeys []string `yaml:"api_keys"`
	Models  []string `yaml:"models"`
}

// FrontrunConfig configures the optional Frontrun enrichment of the summary
// (username-change marker + smart-followers count). Off unless Enabled is true.
type FrontrunConfig struct {
	Enabled            *bool    `yaml:"enabled"`
	BaseURL            string   `yaml:"base_url"`
	Tokens             []string `yaml:"tokens"`     // pool: requests rotate round-robin
	Token              string   `yaml:"token"`      // optional single token, merged into the pool
	TokenFile          string   `yaml:"token_file"` // optional file, one token per line
	ClientVersion      string   `yaml:"client_version"`
	ClientLanguage     string   `yaml:"client_language"`
	ShowUsernameChange *bool    `yaml:"show_username_change"`
	ShowSmartFollowers *bool    `yaml:"show_smart_followers"`
	CacheTTL           string   `yaml:"cache_ttl"`
}

type CategorizationConfig struct {
	// Enabled and UseTweets default to true when omitted (pointer lets us tell
	// "unset" apart from an explicit false).
	Enabled    *bool            `yaml:"enabled"`
	UseTweets  *bool            `yaml:"use_tweets"`
	TweetCount int              `yaml:"tweet_count"`
	KeysFile   string           `yaml:"keys_file"`
	Categories []string         `yaml:"categories"`
	CacheTTL   string           `yaml:"cache_ttl"`
	OpenRouter OpenRouterConfig `yaml:"openrouter"`
}

type LogConfig struct {
	Timezone string `yaml:"timezone"`
	Level    string `yaml:"level"`
}

type Config struct {
	Twitter struct {
		Cookies []CookiePair `yaml:"cookies"`
	} `yaml:"twitter"`
	WatchFile      string               `yaml:"watch_file"`
	Tracking       TrackingConfig       `yaml:"tracking"`
	Discord        DiscordConfig        `yaml:"discord"`
	Categorization CategorizationConfig `yaml:"categorization"`
	Frontrun       FrontrunConfig       `yaml:"frontrun"`
	Logging        LogConfig            `yaml:"logging"`
}

// defaultCategories is the base taxonomy used when none is configured. The LLM
// is asked to prefer these but may return a new category when nothing fits.
var defaultCategories = []string{
	"AI", "Layer 1", "Layer 2", "DeFi", "NFT", "Gaming",
	"Meme", "DePIN", "RWA", "Infra", "Social",
	"KOL", "Trading", "Other",
}

func (c *Config) PollIntervalDuration() time.Duration {
	d, err := time.ParseDuration(c.Tracking.PollInterval)
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

func (c *Config) PageDelayDuration() time.Duration {
	d, err := time.ParseDuration(c.Tracking.PageDelay)
	if err != nil {
		return 500 * time.Millisecond
	}
	return d
}

// SummaryIntervalDuration is how often the categorized summary is posted.
func (c *Config) SummaryIntervalDuration() time.Duration {
	d, err := time.ParseDuration(c.Discord.SummaryInterval)
	if err != nil {
		return time.Hour
	}
	return d
}

// SummaryDedupTTLDuration is how long a target stays excluded from future
// summaries after first appearing in one.
func (c *Config) SummaryDedupTTLDuration() time.Duration {
	d, err := time.ParseDuration(c.Discord.SummaryDedupTTL)
	if err != nil {
		return 30 * 24 * time.Hour
	}
	return d
}

// SummaryFollowerFilterEnabled reports whether the summary follower filter is
// on (default true — a fresh install filters the summary to small accounts).
func (c *Config) SummaryFollowerFilterEnabled() bool {
	if c.Discord.SummaryMaxFollowersEnabled == nil {
		return true
	}
	return *c.Discord.SummaryMaxFollowersEnabled
}

// SummaryMaxFollowersValue is the follower ceiling for the summary filter
// (default 1000 when unset/<=0).
func (c *Config) SummaryMaxFollowersValue() int {
	if c.Discord.SummaryMaxFollowers <= 0 {
		return 1000
	}
	return c.Discord.SummaryMaxFollowers
}

// FrontrunEnabled reports whether the optional Frontrun summary enrichment is
// on (default false / off).
func (c *Config) FrontrunEnabled() bool {
	if c.Frontrun.Enabled == nil {
		return false
	}
	return *c.Frontrun.Enabled
}

// FrontrunTokens is the round-robin token pool: the single token plus the
// tokens list (and any loaded from token_file by main), de-duplicated.
func (c *Config) FrontrunTokens() []string {
	return mergeUniqueKeys([]string{c.Frontrun.Token}, c.Frontrun.Tokens)
}

// FrontrunShowUsernameChange reports whether to show the username-change marker
// (default true when Frontrun is enabled).
func (c *Config) FrontrunShowUsernameChange() bool {
	if c.Frontrun.ShowUsernameChange == nil {
		return true
	}
	return *c.Frontrun.ShowUsernameChange
}

// FrontrunShowSmartFollowers reports whether to show the smart-followers count
// (default true when Frontrun is enabled).
func (c *Config) FrontrunShowSmartFollowers() bool {
	if c.Frontrun.ShowSmartFollowers == nil {
		return true
	}
	return *c.Frontrun.ShowSmartFollowers
}

// FrontrunCacheTTLDuration is how long a Frontrun enrichment result for a handle
// stays cached (default 7 days).
func (c *Config) FrontrunCacheTTLDuration() time.Duration {
	d, err := time.ParseDuration(c.Frontrun.CacheTTL)
	if err != nil {
		return 7 * 24 * time.Hour
	}
	return d
}

// CacheTTLDuration is how long a cached category for a handle stays valid.
func (c *Config) CacheTTLDuration() time.Duration {
	d, err := time.ParseDuration(c.Categorization.CacheTTL)
	if err != nil {
		return 7 * 24 * time.Hour
	}
	return d
}

// CategorizationEnabled reports whether categorization is on (default true).
func (c *Config) CategorizationEnabled() bool {
	if c.Categorization.Enabled == nil {
		return true
	}
	return *c.Categorization.Enabled
}

// UseTweetsEnabled reports whether recent tweets are used as a categorization
// signal (default true). Costs one extra API call per uncached target.
func (c *Config) UseTweetsEnabled() bool {
	if c.Categorization.UseTweets == nil {
		return true
	}
	return *c.Categorization.UseTweets
}

func (c *Config) Timezone() *time.Location {
	tz := c.Logging.Timezone
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return loc
}

var handleRe = regexp.MustCompile(`(?:https?://)?(?:x\.com|twitter\.com)/@?([A-Za-z0-9_]+)`)

func normalizeHandle(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}
	// Try URL pattern first
	m := handleRe.FindStringSubmatch(s)
	if m != nil && m[1] != "" {
		return m[1]
	}
	// Bare handle or @handle
	s = strings.TrimPrefix(s, "@")
	if regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(s) {
		return s
	}
	return ""
}

func loadWatchAccounts(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open watch file: %w", err)
	}
	defer f.Close()

	seen := make(map[string]bool)
	var accounts []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		h := normalizeHandle(scanner.Text())
		if h == "" {
			continue
		}
		key := strings.ToLower(h)
		if seen[key] {
			continue
		}
		seen[key] = true
		accounts = append(accounts, h)
	}
	return accounts, scanner.Err()
}

// loadLLMKeys reads OpenRouter API keys from a file, one per line. Blank lines
// and lines starting with '#' are ignored. A missing file is not an error —
// keys may instead be provided directly in config.yaml.
func loadLLMKeys(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open llm keys file: %w", err)
	}
	defer f.Close()

	var keys []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	return keys, scanner.Err()
}

// mergeUniqueKeys concatenates key slices, trimming blanks and dropping dupes
// while preserving order.
func mergeUniqueKeys(lists ...[]string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, list := range lists {
		for _, k := range list {
			k = strings.TrimSpace(k)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Defaults
	if cfg.WatchFile == "" {
		cfg.WatchFile = "twitter.txt"
	}
	if cfg.Tracking.PollInterval == "" {
		cfg.Tracking.PollInterval = "10m"
	}
	if cfg.Tracking.PageSize == 0 {
		cfg.Tracking.PageSize = 10
	}
	if cfg.Tracking.MaxPages == 0 {
		cfg.Tracking.MaxPages = 2
	}
	if cfg.Tracking.PageDelay == "" {
		cfg.Tracking.PageDelay = "500ms"
	}
	if cfg.Logging.Timezone == "" {
		cfg.Logging.Timezone = "Asia/Jakarta"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Discord.SummaryInterval == "" {
		cfg.Discord.SummaryInterval = "1h"
	}
	if cfg.Discord.SummaryDedupTTL == "" {
		cfg.Discord.SummaryDedupTTL = "720h" // 30 days
	}
	if cfg.Categorization.CacheTTL == "" {
		cfg.Categorization.CacheTTL = "168h" // 7 days
	}
	if len(cfg.Categorization.Categories) == 0 {
		cfg.Categorization.Categories = defaultCategories
	}
	if cfg.Categorization.TweetCount == 0 {
		cfg.Categorization.TweetCount = 8
	}
	if cfg.Categorization.KeysFile == "" {
		cfg.Categorization.KeysFile = "llm.txt"
	}
	// Default the Frontrun headers/host to the values the Frontrun web app sends.
	// Without these the API rejects the request: an empty X-Copilot-Client-Version
	// header (or pointing base_url at the wrong host) makes every call fail, so the
	// enrichment silently never appears. Keep these in sync if Frontrun changes them.
	if cfg.Frontrun.BaseURL == "" {
		cfg.Frontrun.BaseURL = "https://loadbalance.frontrun.pro"
	}
	if cfg.Frontrun.ClientVersion == "" {
		cfg.Frontrun.ClientVersion = "0.0.216"
	}
	if cfg.Frontrun.ClientLanguage == "" {
		cfg.Frontrun.ClientLanguage = "EN_US"
	}
	if cfg.Frontrun.CacheTTL == "" {
		cfg.Frontrun.CacheTTL = "168h" // 7 days
	}

	return cfg, nil
}

func validateConfig(cfg *Config, configPath string) error {
	if len(cfg.Twitter.Cookies) == 0 {
		return fmt.Errorf("no twitter cookies configured")
	}
	for i, c := range cfg.Twitter.Cookies {
		if c.AuthToken == "" || c.Ct0 == "" {
			return fmt.Errorf("cookie pair %d: auth_token and ct0 required", i+1)
		}
	}
	if len(cfg.Discord.RawWebhooks) == 0 {
		return fmt.Errorf("no discord raw_webhooks configured")
	}

	// Check watch file exists
	watchPath := cfg.WatchFile
	if !filepath.IsAbs(watchPath) {
		watchPath = filepath.Join(filepath.Dir(configPath), watchPath)
	}
	if _, err := os.Stat(watchPath); err != nil {
		return fmt.Errorf("watch_file not found: %s", watchPath)
	}

	return nil
}
