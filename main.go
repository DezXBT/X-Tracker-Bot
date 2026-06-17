package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// Load config
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	// Validate
	if err := validateConfig(cfg, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "config invalid: %v\n", err)
		os.Exit(1)
	}

	// Init logger
	initLogger(cfg.Logging.Level, cfg.Timezone())

	// Load watch accounts
	watchPath := cfg.WatchFile
	if !filepath.IsAbs(watchPath) {
		watchPath = filepath.Join(filepath.Dir(*configPath), watchPath)
	}
	accounts, err := loadWatchAccounts(watchPath)
	if err != nil {
		logError("load watch accounts: %v", err)
		os.Exit(1)
	}
	if len(accounts) == 0 {
		logError("no watch accounts found in %s", watchPath)
		os.Exit(1)
	}

	// State paths
	stateDir := filepath.Join(filepath.Dir(*configPath), "state")
	statePath := filepath.Join(stateDir, "state.json")
	eventsPath := filepath.Join(stateDir, "events.jsonl")

	// Load state
	state := LoadState(statePath)

	// One-time category cache reset after a taxonomy/prompt change, so stale
	// (possibly mis-categorized) entries get re-evaluated with the new logic.
	if n := state.MaybeResetCategoryCache(categoryCacheVersion); n > 0 {
		logInfo("category cache reset for updated taxonomy (%d entries cleared)", n)
		state.Save(statePath)
	}

	// Load OpenRouter API keys from file (merged with any set in config.yaml)
	llmPath := cfg.Categorization.KeysFile
	if !filepath.IsAbs(llmPath) {
		llmPath = filepath.Join(filepath.Dir(*configPath), llmPath)
	}
	if fileKeys, err := loadLLMKeys(llmPath); err != nil {
		logWarn("load llm keys: %v", err)
	} else if len(fileKeys) > 0 {
		cfg.Categorization.OpenRouter.APIKeys = mergeUniqueKeys(cfg.Categorization.OpenRouter.APIKeys, fileKeys)
		logInfo("loaded %d OpenRouter key(s) from %s", len(fileKeys), filepath.Base(llmPath))
	}

	// Load Frontrun session tokens from file (one per line), merged into the pool.
	if cfg.Frontrun.TokenFile != "" {
		frPath := cfg.Frontrun.TokenFile
		if !filepath.IsAbs(frPath) {
			frPath = filepath.Join(filepath.Dir(*configPath), frPath)
		}
		if fileTokens, err := loadLLMKeys(frPath); err != nil {
			logWarn("load frontrun tokens: %v", err)
		} else if len(fileTokens) > 0 {
			cfg.Frontrun.Tokens = mergeUniqueKeys(cfg.Frontrun.Tokens, fileTokens)
			logInfo("loaded %d Frontrun token(s) from %s", len(fileTokens), filepath.Base(frPath))
		}
	}

	// Print startup info
	logInfo("early-tracking online (Go)")
	logInfo("watch accounts: %v", accounts)
	logInfo("watch source: %s", watchPath)
	logInfo("track all follows: %v", cfg.Tracking.TrackAllFollows)
	logInfo("poll interval: %s", cfg.Tracking.PollInterval)
	logInfo("cookie pool: %d", len(cfg.Twitter.Cookies))
	logInfo("webhooks: %d", len(cfg.Discord.RawWebhooks))
	logInfo("categorization: %v (openrouter keys: %d, models: %d)",
		cfg.CategorizationEnabled(), len(cfg.Categorization.OpenRouter.APIKeys), len(cfg.Categorization.OpenRouter.Models))
	logInfo("summary webhook: %v (interval: %s)", cfg.Discord.SummaryWebhook != "", cfg.SummaryIntervalDuration())
	logInfo("frontrun enrichment: %v (tokens: %d)", cfg.FrontrunEnabled(), len(cfg.FrontrunTokens()))

	// Optionally refresh GraphQL query IDs + feature flags from x.com. Disabled
	// by default: the built-in IDs are proven to work; enable only if X has
	// rotated them out and scans start failing. Non-fatal on error.
	if cfg.Tracking.DynamicQueryIDs {
		if nIDs, nFeat, err := RefreshFromBundle(); err != nil {
			logWarn("bundle refresh failed: %v (using built-in IDs)", err)
		} else {
			logInfo("refreshed %d GraphQL query IDs and added %d feature flags from x.com", nIDs, nFeat)
		}
	} else {
		logInfo("using built-in GraphQL query IDs (dynamic_query_ids disabled)")
	}

	// Initialize X-Client-Transaction-Id generator (fetches x.com + ondemand JS).
	// Non-fatal: if it fails we fall back to random transaction IDs.
	if err := Init(); err != nil {
		logWarn("X-Client-Transaction-Id init failed: %v (continuing without)", err)
	}

	// Create categorizer + tracker
	categorizer := NewCategorizer(cfg)
	tracker := NewTracker(cfg, accounts, state, eventsPath, statePath, categorizer)

	// Warmup baseline
	tracker.Warmup()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	summaryDone := make(chan struct{})

	// Hourly categorized summary loop (no-op if no summary_webhook configured)
	go func() {
		defer close(summaryDone)
		tracker.RunSummaryLoop(ctx)
	}()

	// Daily digest loop (no-op unless digest_enabled)
	go tracker.RunDigestLoop(ctx)

	// Main loop
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			started := time.Now()
			// Recover from panics so a single bad scan can't kill the loop.
			func() {
				defer func() {
					if r := recover(); r != nil {
						logError("[loop] recovered from panic: %v", r)
					}
				}()
				tracker.ScanOnce()
			}()
			elapsed := time.Since(started)
			interval := cfg.PollIntervalDuration()
			wait := interval - elapsed
			if wait < time.Second {
				wait = time.Second
			}
			logInfo("[loop] done in %s; sleep %s", elapsed.Round(time.Second), wait.Round(time.Second))

			if sleepWithContext(ctx, wait) {
				return
			}
		}
	}()

	sig := <-sigCh
	logInfo("received %s, shutting down...", sig)
	cancel()
	// Wait for BOTH loops to stop before touching shared state — otherwise the
	// final Save can read the state maps while an in-flight sendSummary is still
	// writing them, which is an unrecoverable "concurrent map read and map write".
	<-done
	<-summaryDone
	state.Save(statePath)
	logInfo("state saved, exiting")
}

// sleepWithContext sleeps for d, logging a countdown, but returns early with
// true if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	const tick = 10 * time.Second
	remaining := d
	for remaining > 0 {
		step := tick
		if step > remaining {
			step = remaining
		}
		select {
		case <-ctx.Done():
			return true
		case <-time.After(step):
		}
		remaining -= step
		if remaining > 0 {
			logDebug("[loop] next scan in %s", remaining.Round(time.Second))
		}
	}
	return false
}
