package main

import (
	"fmt"
	"regexp"
	"sync"
)

// queryIDs are GraphQL operation → queryId. These built-in values are the
// proven-working IDs (same as the stable branch); X still serves them. They are
// only overridden when dynamic refresh is explicitly enabled (RefreshFromBundle),
// because newer IDs may require feature flags the built-in set doesn't include.
var (
	queryIDMu sync.RWMutex
	queryIDs  = map[string]string{
		"UserByScreenName": "1VOOyvKkiI3FMmkeDNxM9A",
		"UserByRestId":     "tD8zKvQzwY3kdx5yz6YmOw",
		"Following":        "zx6e-TLzRkeDO_a7p4b3JQ",
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
	mainBundleRe      = regexp.MustCompile(`https://abs\.twimg\.com/responsive-web/client-web/main\.[a-f0-9]+\.js`)
	queryIDRe         = regexp.MustCompile(`\{queryId:"([^"]+)",operationName:"([^"]+)"`)
	featureSwitchesRe = regexp.MustCompile(`featureSwitches:\[([^\]]*)\]`)
	quotedStringRe    = regexp.MustCompile(`"([^"]+)"`)
)

// RefreshFromBundle fetches x.com, locates the main JS bundle, and refreshes
// both the GraphQL query IDs and the required feature flags from it. X rotates
// query IDs and keeps adding new mandatory features; a request missing a
// required feature is rejected ("features cannot be null"), which is why this
// runs at startup. Returns how many query IDs were applied and how many new
// features were added. On error the built-in fallbacks remain in effect.
func RefreshFromBundle() (queryIDsApplied int, featuresAdded int, err error) {
	html, err := fetchURL("https://x.com", map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36",
		"Accept-Language": "en-US,en;q=0.9",
	})
	if err != nil {
		return 0, 0, fmt.Errorf("fetch x.com: %w", err)
	}

	bundleURL := mainBundleRe.FindString(html)
	if bundleURL == "" {
		return 0, 0, fmt.Errorf("main bundle URL not found in HTML")
	}

	js, err := fetchURL(bundleURL, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch main bundle %s: %w", bundleURL, err)
	}

	// Query IDs (operationName → queryId).
	if idMatches := queryIDRe.FindAllStringSubmatch(js, -1); len(idMatches) > 0 {
		queryIDMu.Lock()
		for _, m := range idMatches {
			queryIDs[m[2]] = m[1]
			queryIDsApplied++
		}
		queryIDMu.Unlock()
	}

	// Feature flags: add any required feature we don't already send (as true).
	// Existing values are preserved so deliberately-false flags stay false.
	featuresAdded = mergeBundleFeatures(js)

	if queryIDsApplied == 0 && featuresAdded == 0 {
		return 0, 0, fmt.Errorf("no query IDs or features found in main bundle")
	}
	return queryIDsApplied, featuresAdded, nil
}

// mergeBundleFeatures collects the union of featureSwitches across every
// operation in the bundle and adds the ones missing from defaultFeatures.
// It is called once at startup, before request goroutines run.
func mergeBundleFeatures(js string) int {
	added := 0
	seen := make(map[string]bool)
	for _, fs := range featureSwitchesRe.FindAllStringSubmatch(js, -1) {
		for _, q := range quotedStringRe.FindAllStringSubmatch(fs[1], -1) {
			name := q[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			if _, exists := defaultFeatures[name]; !exists {
				defaultFeatures[name] = true
				added++
			}
		}
	}
	return added
}
