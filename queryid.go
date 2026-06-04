package main

import (
	"fmt"
	"regexp"
	"sync"
)

// queryIDs are GraphQL operation → queryId. The values here are built-in
// fallbacks; RefreshQueryIDs overrides them at startup with the live IDs
// extracted from x.com's main JS bundle (X rotates these regularly).
var (
	queryIDMu sync.RWMutex
	queryIDs  = map[string]string{
		"UserByScreenName": "IGgvgiOx4QZndDHuD3x9TQ",
		"UserByRestId":     "VQfQ9wwYdk6j_u2O4vt64Q",
		"Following":        "XRzHZz4sLnhSgz55WGMCbg",
		"Followers":        "IOh4aS6UdGWGJUYTqliQ7Q",
		"UserTweets":       "PNd0vlufvrcIwrAnBYKE9g",
	}
)

// queryID looks up a query ID under the read lock.
func queryID(operationName string) (string, bool) {
	queryIDMu.RLock()
	defer queryIDMu.RUnlock()
	id, ok := queryIDs[operationName]
	return id, ok
}

var (
	mainBundleRe = regexp.MustCompile(`https://abs\.twimg\.com/responsive-web/client-web/main\.[a-f0-9]+\.js`)
	queryIDRe    = regexp.MustCompile(`\{queryId:"([^"]+)",operationName:"([^"]+)"`)
)

// RefreshQueryIDs fetches x.com, locates the main JS bundle, and extracts the
// current GraphQL queryId for every operation, overriding the built-in
// fallbacks. Returns how many IDs were refreshed. Safe to ignore the error:
// on failure the built-in fallbacks remain in effect.
func RefreshQueryIDs() (int, error) {
	html, err := fetchURL("https://x.com", map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36",
		"Accept-Language": "en-US,en;q=0.9",
	})
	if err != nil {
		return 0, fmt.Errorf("fetch x.com: %w", err)
	}

	bundleURL := mainBundleRe.FindString(html)
	if bundleURL == "" {
		return 0, fmt.Errorf("main bundle URL not found in HTML")
	}

	js, err := fetchURL(bundleURL, nil)
	if err != nil {
		return 0, fmt.Errorf("fetch main bundle %s: %w", bundleURL, err)
	}

	matches := queryIDRe.FindAllStringSubmatch(js, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("no query IDs found in main bundle")
	}

	queryIDMu.Lock()
	defer queryIDMu.Unlock()
	count := 0
	for _, m := range matches {
		queryIDs[m[2]] = m[1] // operationName → queryId
		count++
	}
	return count, nil
}
