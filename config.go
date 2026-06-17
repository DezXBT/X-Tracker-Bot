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
	// SummaryShowBio toggles showing each target's latest bio in the summary
	// (default true).
	SummaryShowBio *bool `yaml:"summary_show_bio,omitempty"`

	// ── Signal markers (all default-on unless noted) ──────────────────────────
	// SummaryShowHeat toggles the 🔥 consensus heat marker (default true).
	SummaryShowHeat *bool `yaml:"summary_show_heat,omitempty"`
	// SummaryBurstMin is how many distinct watchers within SummaryBurstWindow
	// constitute a burst (default 3). SummaryBurstWindow is the span (default 30m).
	SummaryBurstMin    int    `yaml:"summary_burst_min,omitempty"`
	SummaryBurstWindow string `yaml:"summary_burst_window,omitempty"`
	// SummaryShowAge toggles the account-age marker (default true).
	SummaryShowAge *bool `yaml:"summary_show_age,omitempty"`
	// SummaryFreshMaxDays flags accounts younger than this many days as fresh
	// (default 30; <=0 disables the ✨ fresh marker).
	SummaryFreshMaxDays int `yaml:"summary_fresh_max_days,omitempty"`
	// SummaryShowContract toggles bio contract-address detection + chart link
	// (default true).
	SummaryShowContract *bool `yaml:"summary_show_contract,omitempty"`
	// SummaryShowLinks toggles the inline DexScreener/.first action links
	// (default true).
	SummaryShowLinks *bool `yaml:"summary_show_links,omitempty"`
	// SummaryShowMutual toggles the ⭐ recurring-target marker (default true).
	SummaryShowMutual *bool `yaml:"summary_show_mutual,omitempty"`
	// SummaryMuteCategories lists categories to exclude from the summary entirely
	// (case-insensitive; e.g. ["Meme","NFT"]). Empty means mute nothing.
	SummaryMuteCategories []string `yaml:"summary_mute_categories,omitempty"`

	// ── Threshold instant-alert ──────────────────────────────────────────────
	// ThresholdAlertEnabled turns on the separate instant alert fired when a
	// target crosses ThresholdAlertWatchers distinct watchers inside
	// ThresholdAlertWindow (default off).
	ThresholdAlertEnabled  *bool  `yaml:"threshold_alert_enabled,omitempty"`
	ThresholdAlertWatchers int    `yaml:"threshold_alert_watchers,omitempty"`
	ThresholdAlertWindow   string `yaml:"threshold_alert_window,omitempty"`
	// ThresholdAlertWebhook is where instant alerts go; falls back to
	// SummaryWebhook when empty.
	ThresholdAlertWebhook string `yaml:"threshold_alert_webhook,omitempty"`
	// ThresholdAlertMention is an optional Discord mention prepended to the alert
	// (e.g. "<@123>" or "@here") so it pings.
	ThresholdAlertMention string `yaml:"threshold_alert_mention,omitempty"`

	// ── Daily digest ──────────────────────────────────────────────────────────
	// DigestEnabled turns on a once-daily top-N recap (default off).
	DigestEnabled *bool `yaml:"digest_enabled,omitempty"`
	// DigestInterval is how often the digest runs (default 24h).
	DigestInterval string `yaml:"digest_interval,omitempty"`
	// DigestTopN caps how many targets the digest lists (default 10).
	DigestTopN int `yaml:"digest_top_n,omitempty"`
	// DigestWebhook is where the digest goes; falls back to SummaryWebhook.
	DigestWebhook string `yaml:"digest_webhook,omitempty"`
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

// SummaryShowBio reports whether each summary target shows its latest bio
// (default true when unset).
func (c *Config) SummaryShowBio() bool {
	if c.Discord.SummaryShowBio == nil {
		return true
	}
	return *c.Discord.SummaryShowBio
}

// boolOrTrue returns *p when set, else true (shared default-on helper).
func boolOrTrue(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

// SummaryShowHeat reports whether the 🔥 consensus heat marker is shown
// (default true).
func (c *Config) SummaryShowHeat() bool { return boolOrTrue(c.Discord.SummaryShowHeat) }

// SummaryShowAge reports whether the account-age marker is shown (default true).
func (c *Config) SummaryShowAge() bool { return boolOrTrue(c.Discord.SummaryShowAge) }

// SummaryShowContract reports whether bio contract detection is shown
// (default true).
func (c *Config) SummaryShowContract() bool { return boolOrTrue(c.Discord.SummaryShowContract) }

// SummaryShowLinks reports whether inline action links are shown (default true).
func (c *Config) SummaryShowLinks() bool { return boolOrTrue(c.Discord.SummaryShowLinks) }

// SummaryShowMutual reports whether the ⭐ recurring-target marker is shown
// (default true).
func (c *Config) SummaryShowMutual() bool { return boolOrTrue(c.Discord.SummaryShowMutual) }

// SummaryBurstMinValue is the distinct-watcher count that constitutes a burst
// (default 3, floor 2).
func (c *Config) SummaryBurstMinValue() int {
	if c.Discord.SummaryBurstMin < 2 {
		return 3
	}
	return c.Discord.SummaryBurstMin
}

// SummaryBurstWindowDuration is the span within which SummaryBurstMin follows
// count as a burst (default 30m).
func (c *Config) SummaryBurstWindowDuration() time.Duration {
	d, err := time.ParseDuration(c.Discord.SummaryBurstWindow)
	if err != nil || d <= 0 {
		return 30 * time.Minute
	}
	return d
}

// SummaryFreshMaxDaysValue is the account-age ceiling (days) for the ✨ fresh
// marker (default 30; <=0 disables — preserved so the user can turn it off).
func (c *Config) SummaryFreshMaxDaysValue() int {
	// A negative value explicitly disables; 0 (unset) falls back to the default.
	if c.Discord.SummaryFreshMaxDays < 0 {
		return 0
	}
	if c.Discord.SummaryFreshMaxDays == 0 {
		return 30
	}
	return c.Discord.SummaryFreshMaxDays
}

// SummaryMuteSet returns the set of muted category names, lower-cased for
// case-insensitive matching.
func (c *Config) SummaryMuteSet() map[string]bool {
	set := make(map[string]bool, len(c.Discord.SummaryMuteCategories))
	for _, name := range c.Discord.SummaryMuteCategories {
		n := strings.ToLower(strings.TrimSpace(name))
		if n != "" {
			set[n] = true
		}
	}
	return set
}

// ThresholdAlertEnabled reports whether the instant threshold alert is on
// (default false).
func (c *Config) ThresholdAlertEnabled() bool {
	if c.Discord.ThresholdAlertEnabled == nil {
		return false
	}
	return *c.Discord.ThresholdAlertEnabled
}

// ThresholdAlertWatchersValue is the distinct-watcher count that triggers an
// instant alert (default 3, floor 2).
func (c *Config) ThresholdAlertWatchersValue() int {
	if c.Discord.ThresholdAlertWatchers < 2 {
		return 3
	}
	return c.Discord.ThresholdAlertWatchers
}

// ThresholdAlertWindowDuration is the span the threshold-watcher count must be
// reached within (default 30m).
func (c *Config) ThresholdAlertWindowDuration() time.Duration {
	d, err := time.ParseDuration(c.Discord.ThresholdAlertWindow)
	if err != nil || d <= 0 {
		return 30 * time.Minute
	}
	return d
}

// ThresholdAlertWebhookURL is where instant alerts go, falling back to the
// summary webhook when unset.
func (c *Config) ThresholdAlertWebhookURL() string {
	if c.Discord.ThresholdAlertWebhook != "" {
		return c.Discord.ThresholdAlertWebhook
	}
	return c.Discord.SummaryWebhook
}

// DigestEnabled reports whether the daily digest is on (default false).
func (c *Config) DigestEnabled() bool {
	if c.Discord.DigestEnabled == nil {
		return false
	}
	return *c.Discord.DigestEnabled
}

// DigestIntervalDuration is how often the digest runs (default 24h).
func (c *Config) DigestIntervalDuration() time.Duration {
	d, err := time.ParseDuration(c.Discord.DigestInterval)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// DigestTopNValue caps how many targets the digest lists (default 10, floor 1).
func (c *Config) DigestTopNValue() int {
	if c.Discord.DigestTopN < 1 {
		return 10
	}
	return c.Discord.DigestTopN
}

// DigestWebhookURL is where the digest goes, falling back to the summary
// webhook when unset.
func (c *Config) DigestWebhookURL() string {
	if c.Discord.DigestWebhook != "" {
		return c.Discord.DigestWebhook
	}
	return c.Discord.SummaryWebhook
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
