package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	logMu       sync.Mutex
	logLevel    = "info"
	logTimezone *time.Location
)

var logLevels = map[string]int{
	"debug": 0,
	"info":  1,
	"warn":  2,
	"error": 3,
}

func initLogger(level string, tz *time.Location) {
	logLevel = level
	logTimezone = tz
}

func shouldLog(level string) bool {
	return logLevels[level] >= logLevels[logLevel]
}

func logMsg(level, format string, args ...interface{}) {
	if !shouldLog(level) {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	loc := logTimezone
	if loc == nil {
		// Logger not initialised (e.g. in tests) — fall back to UTC instead of
		// panicking inside time.Time.In on a nil Location.
		loc = time.UTC
	}
	ts := time.Now().In(loc).Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[%s] [%s] %s\n", ts, level, msg)
}

func logDebug(format string, args ...interface{}) { logMsg("debug", format, args...) }
func logInfo(format string, args ...interface{})  { logMsg("info", format, args...) }
func logWarn(format string, args ...interface{})  { logMsg("warn", format, args...) }
func logError(format string, args ...interface{}) { logMsg("error", format, args...) }
