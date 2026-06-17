package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const bearerToken = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

// Query IDs live in queryid.go and are refreshed from x.com at startup.

var defaultFeatures = map[string]bool{
	"rweb_tipjar_consumption_enabled":                                         true,
	"responsive_web_graphql_exclude_directive_enabled":                        true,
	"verified_phone_label_enabled":                                            false,
	"creator_subscriptions_tweet_preview_api_enabled":                         true,
	"responsive_web_graphql_timeline_navigation_enabled":                      true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled":       false,
	"communities_web_enable_tweet_community_results_fetch":                    true,
	"c9s_tweet_anatomy_moderator_badge_enabled":                               true,
	"articles_preview_enabled":                                                true,
	"tweetypie_unmention_optimization_enabled":                                true,
	"responsive_web_edit_tweet_api_enabled":                                   true,
	"graphql_is_translatable_rweb_tweet_is_translatable_enabled":              true,
	"view_counts_everywhere_api_enabled":                                      true,
	"longform_notetweets_consumption_enabled":                                 true,
	"responsive_web_twitter_article_tweet_consumption_enabled":                true,
	"tweet_awards_web_tipping_enabled":                                        false,
	"creator_subscriptions_quote_tweet_preview_enabled":                       false,
	"freedom_of_speech_not_reach_fetch_enabled":                               true,
	"standardized_nudges_misinfo":                                             true,
	"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled": true,
	"rweb_video_timestamps_enabled":                                           true,
	"longform_notetweets_rich_text_read_enabled":                              true,
	"longform_notetweets_inline_media_enabled":                                true,
	"responsive_web_enhance_cards_enabled":                                    false,
	"responsive_web_twitter_article_notes_tab_enabled":                        true,
	"subscriptions_verification_info_verified_since_enabled":                  true,
	"subscriptions_verification_info_is_identity_verified_enabled":            true,
	"highlights_tweets_tab_ui_enabled":                                        true,
	"profile_label_improvements_pcf_label_in_post_enabled":                    true,
	"hidden_profile_subscriptions_enabled":                                    true,
	"subscriptions_feature_can_gift_premium":                                  true,
	"responsive_web_grok_show_grok_translated_post":                           true,
	"responsive_web_grok_analyze_post_followups_enabled":                      true,
	"premium_content_api_read_enabled":                                        true,
	"responsive_web_grok_image_annotation_enabled":                            true,
	"responsive_web_grok_share_attachment_enabled":                            true,
	"responsive_web_grok_analysis_button_from_backend":                        true,
	"responsive_web_grok_analyze_button_fetch_trends_enabled":                 true,
	"rweb_video_screen_enabled":                                               true,
	"responsive_web_jetfuel_frame":                                            true,
	// Added so newer operations (e.g. UserTweets) aren't rejected for missing
	// required features. Extra features are ignored by operations that don't use
	// them, so this is safe for Following/UserByScreenName too.
	"rweb_cashtags_enabled":                                          true,
	"responsive_web_profile_redirect_enabled":                        true,
	"rweb_cashtags_composer_attachment_enabled":                      true,
	"responsive_web_grok_annotations_enabled":                        true,
	"rweb_conversational_replies_downvote_enabled":                   true,
	"content_disclosure_indicator_enabled":                           true,
	"content_disclosure_ai_generated_indicator_enabled":              true,
	"post_ctas_fetch_enabled":                                        true,
	"responsive_web_grok_imagine_annotation_enabled":                 true,
	"responsive_web_grok_community_note_auto_translation_is_enabled": true,
}

// User represents a Twitter user profile
type User struct {
	ID              string `json:"id"`
	RestID          string `json:"restId"`
	Name            string `json:"name"`
	ScreenName      string `json:"screenName"`
	Description     string `json:"description"`
	FollowersCount  int    `json:"followersCount"`
	ProfileImageURL string `json:"profileImageUrl"`
	// CreatedAt is X's legacy account-creation timestamp, e.g.
	// "Wed Oct 10 20:19:24 +0000 2018". Used to compute account age for the
	// summary "fresh account" marker. Empty when unavailable.
	CreatedAt string `json:"createdAt"`
}

type PaginatedResult struct {
	Items   []User
	Cursor  string
	HasMore bool
}

// TwitterClient talks to X's internal GraphQL API using cookie auth.
type TwitterClient struct {
	cookies   CookiePair
	client    *http.Client
	rateLimit map[string]rateInfo
}

type rateInfo struct {
	remaining int
	reset     time.Time
}

func NewTwitterClient(cookies CookiePair) *TwitterClient {
	return &TwitterClient{
		cookies: cookies,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimit: make(map[string]rateInfo),
	}
}

func (tc *TwitterClient) getHeaders() http.Header {
	h := http.Header{}
	h.Set("authorization", "Bearer "+bearerToken)
	h.Set("x-twitter-auth-type", "OAuth2Session")
	h.Set("x-twitter-active-user", "yes")
	h.Set("x-csrf-token", tc.cookies.Ct0)
	h.Set("cookie", fmt.Sprintf("auth_token=%s; ct0=%s", tc.cookies.AuthToken, tc.cookies.Ct0))
	h.Set("content-type", "application/json")
	h.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	h.Set("x-twitter-client-language", "en")
	h.Set("accept", "*/*")
	h.Set("accept-language", "en-US,en;q=0.9")
	return h
}

// fallbackTransactionID generates a basic random transaction ID when
// the real generator is unavailable. X requires the header on every request.
func fallbackTransactionID() string {
	randPart := make([]byte, 32)
	if _, err := rand.Read(randPart); err != nil {
		// crypto/rand should never fail; degrade to a time-seeded value.
		binary.LittleEndian.PutUint64(randPart, uint64(time.Now().UnixNano()))
	}
	return base64.RawURLEncoding.EncodeToString(randPart)
}

// GetUser fetches a user profile by screen name.
func (tc *TwitterClient) GetUser(screenName string) (*User, error) {
	variables := map[string]interface{}{
		"screen_name":              screenName,
		"withSafetyModeUserFields": true,
	}
	data, err := tc.graphql("UserByScreenName", variables, false)
	if err != nil {
		return nil, err
	}

	userObj, ok := data["user"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing user in response for %s", screenName)
	}
	result, ok := userObj["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing user.result in response for %s", screenName)
	}
	return parseUser(result)
}

// GetFollowing fetches the following list for a user by their rest_id.
func (tc *TwitterClient) GetFollowing(userID string, count int, cursor string) (*PaginatedResult, error) {
	variables := map[string]interface{}{
		"userId":                 userID,
		"count":                  count,
		"includePromotedContent": false,
	}
	if cursor != "" {
		variables["cursor"] = cursor
	}
	data, err := tc.graphql("Following", variables, false)
	if err != nil {
		return nil, err
	}
	return parseUserList(data)
}

// GetUserTweets fetches recent tweet text for a user by rest_id and returns it
// joined into a single string (used as extra signal for categorization).
func (tc *TwitterClient) GetUserTweets(userID string, count int) (string, error) {
	if count <= 0 {
		count = 5
	}
	variables := map[string]interface{}{
		"userId":                                 userID,
		"count":                                  count,
		"includePromotedContent":                 false,
		"withQuickPromoteEligibilityTweetFields": false,
		"withVoice":                              true,
		"withV2Timeline":                         true,
	}
	data, err := tc.graphql("UserTweets", variables, false)
	if err != nil {
		return "", err
	}
	return parseTweetsText(data, count), nil
}

// parseTweetsText recursively collects up to maxTweets "full_text" values from a
// UserTweets response. Walking the tree (rather than the exact timeline path)
// keeps this resilient to X's frequent structure changes.
func parseTweetsText(data map[string]interface{}, maxTweets int) string {
	var texts []string
	collectFullText(data, &texts, maxTweets)
	joined := strings.Join(texts, " | ")
	return truncateRunes(joined, 1600)
}

func collectFullText(v interface{}, out *[]string, limit int) {
	if len(*out) >= limit {
		return
	}
	switch t := v.(type) {
	case map[string]interface{}:
		// Skip retweets: a "RT @user: ..." entry is someone else's content and
		// pollutes the categorization signal — we only want the account's own voice.
		if ft, ok := t["full_text"].(string); ok && ft != "" && !strings.HasPrefix(ft, "RT @") {
			*out = append(*out, ft)
			if len(*out) >= limit {
				return
			}
		}
		for _, val := range t {
			collectFullText(val, out, limit)
		}
	case []interface{}:
		for _, val := range t {
			collectFullText(val, out, limit)
		}
	}
}

// graphql sends a GraphQL request to X's API.
func (tc *TwitterClient) graphql(operationName string, variables map[string]interface{}, usePost bool) (map[string]interface{}, error) {
	qid, ok := queryID(operationName)
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s", operationName)
	}

	// Rate limit check
	if rl, exists := tc.rateLimit[operationName]; exists {
		if rl.remaining <= 0 && time.Now().Before(rl.reset) {
			wait := time.Until(rl.reset) + time.Second
			logWarn("rate limited on %s, waiting %s", operationName, wait.Round(time.Second))
			time.Sleep(wait)
		}
	}

	// Add jitter
	time.Sleep(time.Duration(50+randInt(200)) * time.Millisecond)

	variablesJSON, _ := json.Marshal(variables)
	featuresJSON, _ := json.Marshal(defaultFeatures)

	var req *http.Request
	var err error

	if usePost {
		body := map[string]interface{}{
			"features": defaultFeatures,
			"queryId":  qid,
		}
		bodyJSON, _ := json.Marshal(body)
		u := fmt.Sprintf("https://x.com/i/api/graphql/%s/%s?variables=%s",
			qid, operationName, url.QueryEscape(string(variablesJSON)))
		req, err = http.NewRequest("POST", u, strings.NewReader(string(bodyJSON)))
	} else {
		u := fmt.Sprintf("https://x.com/i/api/graphql/%s/%s?variables=%s&features=%s",
			qid, operationName, url.QueryEscape(string(variablesJSON)), url.QueryEscape(string(featuresJSON)))
		req, err = http.NewRequest("GET", u, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header = tc.getHeaders()

	// Add X-Client-Transaction-Id based on actual request method and path
	if TxGen != nil {
		txID := Generate(req.Method, req.URL.Path)
		if txID != "" {
			req.Header.Set("x-client-transaction-id", txID)
		}
	}
	if req.Header.Get("x-client-transaction-id") == "" {
		req.Header.Set("x-client-transaction-id", fallbackTransactionID())
	}

	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Update rate limits
	if rl := resp.Header.Get("x-rate-limit-remaining"); rl != "" {
		var remaining int
		fmt.Sscanf(rl, "%d", &remaining)
		if resetStr := resp.Header.Get("x-rate-limit-reset"); resetStr != "" {
			var resetUnix int64
			fmt.Sscanf(resetStr, "%d", &resetUnix)
			tc.rateLimit[operationName] = rateInfo{
				remaining: remaining,
				reset:     time.Unix(resetUnix, 0),
			}
		}
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("rate limited (429) on %s", operationName)
	}
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("unauthorized (401) — cookie may be expired")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d on %s: %s", resp.StatusCode, operationName, string(bodyBytes[:min(len(bodyBytes), 200)]))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if errs, ok := result["errors"].([]interface{}); ok && len(errs) > 0 {
		return nil, fmt.Errorf("GraphQL error: %v", errs[0])
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response structure")
	}
	return data, nil
}

func parseUser(result map[string]interface{}) (*User, error) {
	legacy, ok := result["legacy"].(map[string]interface{})
	if !ok || legacy == nil {
		return nil, fmt.Errorf("missing legacy data (suspended/deactivated?)")
	}

	user := &User{
		Name:            getString(legacy, "name"),
		ScreenName:      getString(legacy, "screen_name"),
		Description:     getString(legacy, "description"),
		FollowersCount:  getInt(legacy, "followers_count"),
		ProfileImageURL: getString(legacy, "profile_image_url_https"),
		CreatedAt:       getString(legacy, "created_at"),
	}
	if id, ok := result["rest_id"].(string); ok {
		user.RestID = id
	}
	if id, ok := result["id"].(string); ok {
		user.ID = id
	}
	return user, nil
}

func parseUserList(data map[string]interface{}) (*PaginatedResult, error) {
	timeline := data
	// Navigate: user.result.timeline.timeline.instructions
	if u, ok := timeline["user"].(map[string]interface{}); ok {
		if r, ok := u["result"].(map[string]interface{}); ok {
			if t, ok := r["timeline"].(map[string]interface{}); ok {
				if t2, ok := t["timeline"].(map[string]interface{}); ok {
					timeline = t2
				}
			}
		}
	}

	instructions, _ := timeline["instructions"].([]interface{})
	if len(instructions) == 0 {
		return &PaginatedResult{}, nil
	}

	var entries []interface{}
	for _, inst := range instructions {
		m, ok := inst.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == "TimelineAddEntries" {
			if e, ok := m["entries"].([]interface{}); ok {
				entries = e
			}
		}
	}

	result := &PaginatedResult{}
	for _, entry := range entries {
		e, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}

		content, _ := e["content"].(map[string]interface{})
		if content == nil {
			continue
		}

		// Check for cursor
		if cursorType, _ := content["cursorType"].(string); cursorType == "Bottom" {
			if val, ok := content["value"].(string); ok {
				result.Cursor = val
				result.HasMore = true
			}
			continue
		}

		// Parse user entry
		itemContent, _ := content["itemContent"].(map[string]interface{})
		if itemContent == nil {
			continue
		}
		userResults, _ := itemContent["user_results"].(map[string]interface{})
		if userResults == nil {
			continue
		}
		userResult, _ := userResults["result"].(map[string]interface{})
		if userResult == nil {
			continue
		}

		user, err := parseUser(userResult)
		if err != nil {
			// Skip bad entries (suspended/deactivated) — don't crash
			continue
		}
		result.Items = append(result.Items, *user)
	}

	return result, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func randInt(max int) int {
	if max <= 0 {
		return 0
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return 0
	}
	return int(binary.BigEndian.Uint32(b) % uint32(max))
}
