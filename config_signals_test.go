package main

import (
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

func TestSummaryToggleDefaults(t *testing.T) {
	// All toggles default ON when unset.
	c := &Config{}
	if !c.SummaryShowHeat() || !c.SummaryShowAge() || !c.SummaryShowContract() ||
		!c.SummaryShowLinks() || !c.SummaryShowMutual() {
		t.Errorf("expected all summary signal toggles to default true")
	}

	// Explicit false is honoured.
	c.Discord.SummaryShowHeat = boolPtr(false)
	c.Discord.SummaryShowLinks = boolPtr(false)
	if c.SummaryShowHeat() {
		t.Errorf("expected SummaryShowHeat=false to be honoured")
	}
	if c.SummaryShowLinks() {
		t.Errorf("expected SummaryShowLinks=false to be honoured")
	}
	if !c.SummaryShowAge() {
		t.Errorf("expected SummaryShowAge to remain default true")
	}
}

func TestBurstConfig(t *testing.T) {
	c := &Config{}
	// Defaults.
	if c.SummaryBurstMinValue() != 3 {
		t.Errorf("expected default burst min 3, got %d", c.SummaryBurstMinValue())
	}
	if c.SummaryBurstWindowDuration() != 30*time.Minute {
		t.Errorf("expected default burst window 30m, got %s", c.SummaryBurstWindowDuration())
	}
	// Floor: anything < 2 falls back to default.
	c.Discord.SummaryBurstMin = 1
	if c.SummaryBurstMinValue() != 3 {
		t.Errorf("expected burst min floor to default to 3, got %d", c.SummaryBurstMinValue())
	}
	c.Discord.SummaryBurstMin = 4
	if c.SummaryBurstMinValue() != 4 {
		t.Errorf("expected burst min 4, got %d", c.SummaryBurstMinValue())
	}
	// Bad/empty duration -> default.
	c.Discord.SummaryBurstWindow = "garbage"
	if c.SummaryBurstWindowDuration() != 30*time.Minute {
		t.Errorf("expected garbage window to default 30m")
	}
	c.Discord.SummaryBurstWindow = "15m"
	if c.SummaryBurstWindowDuration() != 15*time.Minute {
		t.Errorf("expected 15m window, got %s", c.SummaryBurstWindowDuration())
	}
}

func TestFreshMaxDaysConfig(t *testing.T) {
	c := &Config{}
	// Unset -> default 30.
	if c.SummaryFreshMaxDaysValue() != 30 {
		t.Errorf("expected default fresh max 30, got %d", c.SummaryFreshMaxDaysValue())
	}
	// Negative -> disabled (0).
	c.Discord.SummaryFreshMaxDays = -1
	if c.SummaryFreshMaxDaysValue() != 0 {
		t.Errorf("expected negative to disable (0), got %d", c.SummaryFreshMaxDaysValue())
	}
	// Explicit positive value.
	c.Discord.SummaryFreshMaxDays = 7
	if c.SummaryFreshMaxDaysValue() != 7 {
		t.Errorf("expected 7, got %d", c.SummaryFreshMaxDaysValue())
	}
}

func TestMuteSet(t *testing.T) {
	c := &Config{}
	c.Discord.SummaryMuteCategories = []string{"Meme", " NFT ", "", "gaming"}
	set := c.SummaryMuteSet()
	if !set["meme"] || !set["nft"] || !set["gaming"] {
		t.Errorf("expected lower-cased trimmed mute set, got %v", set)
	}
	if len(set) != 3 {
		t.Errorf("expected 3 entries (blank dropped), got %d", len(set))
	}
}

func TestThresholdAlertConfig(t *testing.T) {
	c := &Config{}
	if c.ThresholdAlertEnabled() {
		t.Errorf("expected threshold alert disabled by default")
	}
	c.Discord.ThresholdAlertEnabled = boolPtr(true)
	if !c.ThresholdAlertEnabled() {
		t.Errorf("expected threshold alert enabled")
	}
	// Watcher floor.
	if c.ThresholdAlertWatchersValue() != 3 {
		t.Errorf("expected default 3 watchers, got %d", c.ThresholdAlertWatchersValue())
	}
	c.Discord.ThresholdAlertWatchers = 1
	if c.ThresholdAlertWatchersValue() != 3 {
		t.Errorf("expected floor to default 3, got %d", c.ThresholdAlertWatchersValue())
	}
	c.Discord.ThresholdAlertWatchers = 5
	if c.ThresholdAlertWatchersValue() != 5 {
		t.Errorf("expected 5 watchers, got %d", c.ThresholdAlertWatchersValue())
	}
	// Window default + override.
	if c.ThresholdAlertWindowDuration() != 30*time.Minute {
		t.Errorf("expected default 30m window")
	}
	c.Discord.ThresholdAlertWindow = "10m"
	if c.ThresholdAlertWindowDuration() != 10*time.Minute {
		t.Errorf("expected 10m window")
	}
	// Webhook falls back to summary webhook.
	c.Discord.SummaryWebhook = "https://summary"
	if c.ThresholdAlertWebhookURL() != "https://summary" {
		t.Errorf("expected fallback to summary webhook")
	}
	c.Discord.ThresholdAlertWebhook = "https://alert"
	if c.ThresholdAlertWebhookURL() != "https://alert" {
		t.Errorf("expected dedicated alert webhook to win")
	}
}

func TestDigestConfig(t *testing.T) {
	c := &Config{}
	if c.DigestEnabled() {
		t.Errorf("expected digest disabled by default")
	}
	c.Discord.DigestEnabled = boolPtr(true)
	if !c.DigestEnabled() {
		t.Errorf("expected digest enabled")
	}
	if c.DigestIntervalDuration() != 24*time.Hour {
		t.Errorf("expected default 24h interval, got %s", c.DigestIntervalDuration())
	}
	c.Discord.DigestInterval = "12h"
	if c.DigestIntervalDuration() != 12*time.Hour {
		t.Errorf("expected 12h interval")
	}
	if c.DigestTopNValue() != 10 {
		t.Errorf("expected default top 10, got %d", c.DigestTopNValue())
	}
	c.Discord.DigestTopN = 5
	if c.DigestTopNValue() != 5 {
		t.Errorf("expected top 5, got %d", c.DigestTopNValue())
	}
	// Webhook fallback.
	c.Discord.SummaryWebhook = "https://summary"
	if c.DigestWebhookURL() != "https://summary" {
		t.Errorf("expected fallback to summary webhook")
	}
	c.Discord.DigestWebhook = "https://digest"
	if c.DigestWebhookURL() != "https://digest" {
		t.Errorf("expected dedicated digest webhook to win")
	}
}
