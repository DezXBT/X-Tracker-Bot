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
}

type DiscordConfig struct {
	RawWebhooks     []string `yaml:"raw_webhooks"`
	SummaryWebhook  string   `yaml:"summary_webhook,omitempty"`
	SummaryInterval string   `yaml:"summary_interval,omitempty"`
}

type LogConfig struct {
	Timezone string `yaml:"timezone"`
	Level    string `yaml:"level"`
}

type Config struct {
	Twitter struct {
		Cookies []CookiePair `yaml:"cookies"`
	} `yaml:"twitter"`
	WatchFile  string          `yaml:"watch_file"`
	Tracking   TrackingConfig  `yaml:"tracking"`
	Discord    DiscordConfig   `yaml:"discord"`
	Logging    LogConfig       `yaml:"logging"`
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
