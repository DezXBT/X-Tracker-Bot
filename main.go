package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var cfgFile string

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// Load config
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	cfgFile = *configPath

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

	// Print startup info
	logInfo("early-tracking online (Go)")
	logInfo("watch accounts: %v", accounts)
	logInfo("watch source: %s", watchPath)
	logInfo("track all follows: %v", cfg.Tracking.TrackAllFollows)
	logInfo("poll interval: %s", cfg.Tracking.PollInterval)
	logInfo("cookie pool: %d", len(cfg.Twitter.Cookies))
	logInfo("webhooks: %d", len(cfg.Discord.RawWebhooks))

	// Create tracker
	tracker := NewTracker(cfg, accounts, state, eventsPath, statePath)

	// Warmup baseline
	tracker.Warmup()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Main loop
	go func() {
		for {
			started := time.Now()
			tracker.ScanOnce()
			elapsed := time.Since(started)
			interval := cfg.PollIntervalDuration()
			wait := interval - elapsed
			if wait < time.Second {
				wait = time.Second
			}
			logInfo("[loop] done in %s; sleep %s", elapsed.Round(time.Second), wait.Round(time.Second))

			// Sleep with countdown
			tick := 10 * time.Second
			remaining := wait
			for remaining > 0 {
				d := tick
				if d > remaining {
					d = remaining
				}
				time.Sleep(d)
				remaining -= d
				if remaining > 0 {
					logDebug("[loop] next scan in %s", remaining.Round(time.Second))
				}
			}
		}
	}()

	sig := <-sigCh
	logInfo("received %s, saving state...", sig)
	state.Save(statePath)
	logInfo("state saved, exiting")
}
