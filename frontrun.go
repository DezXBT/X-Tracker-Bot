package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type UsernameHistoryEntry struct {
	OldUsername string `json:"oldTwitterUsername"`
	ChangedAt   string `json:"changedAt"`
}

type BioHistoryEntry struct {
	Bio         string `json:"bio"`
	LastChecked string `json:"last_checked"`
}

type SmartFollower struct {
	TwitterID           string `json:"twitterId"`
	Name                string `json:"name"`
	Twitter             string `json:"twitter"`
	Bio                 string `json:"bio"`
	ProfilePhoto        string `json:"profilePhoto"`
	FollowersCount      int    `json:"followersCount"`
	SmartFollowersCount int    `json:"smartFollowersCount"`
	FollowedAt          string `json:"followedAt"`
}

type FrontrunClient struct {
	baseURL    string
	tokens     []string
	idx        int
	mu         sync.Mutex
	clientVer  string
	clientLang string
	httpClient *http.Client
}

// NewFrontrunClient builds a client backed by a pool of session tokens. Requests
// are rotated round-robin across the pool, and a request that fails with an
// auth/rate-limit status retries on the next token, so a single throttled or
// expired token doesn't fail the whole call.
func NewFrontrunClient(baseURL string, tokens []string, clientVer, clientLang string) *FrontrunClient {
	return &FrontrunClient{
		baseURL:    baseURL,
		tokens:     tokens,
		clientVer:  clientVer,
		clientLang: clientLang,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// nextToken returns the next token in round-robin order.
func (fc *FrontrunClient) nextToken() string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	tok := fc.tokens[fc.idx]
	fc.idx = (fc.idx + 1) % len(fc.tokens)
	return tok
}

func (fc *FrontrunClient) request(path string) ([]byte, error) {
	u := fmt.Sprintf("%s%s", fc.baseURL, path)

	var lastErr error
	// Try each token at most once, rotating on transport errors or auth/rate-limit
	// statuses so one bad or throttled token doesn't sink the request.
	for attempt := 0; attempt < len(fc.tokens); attempt++ {
		token := fc.nextToken()

		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Cookie", fmt.Sprintf("__Secure-frontrun.session_token=%s; __Secure-frontrun.session_token_domain_migrated=1", token))
		req.Header.Set("X-Copilot-Client-Version", fc.clientVer)
		req.Header.Set("X-Copilot-Client-Language", fc.clientLang)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36")

		resp, err := fc.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http request: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		if resp.StatusCode == 200 {
			return body, nil
		}

		lastErr = fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, u, string(body))
		// Only an auth/rate-limit failure is worth retrying on another token;
		// other statuses (e.g. 404) won't change with a different token.
		if resp.StatusCode != 401 && resp.StatusCode != 403 && resp.StatusCode != 429 {
			return nil, lastErr
		}
	}

	return nil, lastErr
}

// GetUsernameHistory fetches username history for a handle.
func (fc *FrontrunClient) GetUsernameHistory(handle string) ([]UsernameHistoryEntry, error) {
	path := fmt.Sprintf("/api/v1/twitter/%s/username-history", handle)
	body, err := fc.request(path)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			UsernameHistory []UsernameHistoryEntry `json:"usernameHistory"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse username history: %w", err)
	}
	if resp.Data.UsernameHistory == nil {
		return []UsernameHistoryEntry{}, nil
	}
	return resp.Data.UsernameHistory, nil
}

// GetBioHistory fetches bio history for a handle.
func (fc *FrontrunClient) GetBioHistory(handle string) ([]BioHistoryEntry, error) {
	path := fmt.Sprintf("/api/v1/twitter/%s/bio-history", handle)
	body, err := fc.request(path)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			BioHistory []BioHistoryEntry `json:"bioHistory"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse bio history: %w", err)
	}
	if resp.Data.BioHistory == nil {
		return []BioHistoryEntry{}, nil
	}
	return resp.Data.BioHistory, nil
}

// GetSmartFollowers fetches smart followers for a handle.
func (fc *FrontrunClient) GetSmartFollowers(handle string) ([]SmartFollower, error) {
	path := fmt.Sprintf("/api/v1/twitter/%s/smart-followers", handle)
	body, err := fc.request(path)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			SmartFollowers []SmartFollower `json:"smartFollowers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse smart followers: %w", err)
	}
	if resp.Data.SmartFollowers == nil {
		return []SmartFollower{}, nil
	}
	return resp.Data.SmartFollowers, nil
}

// GetUserInfo fetches user info from Frontrun.
func (fc *FrontrunClient) GetUserInfo(handle string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/v3/twitter/%s/info", handle)
	body, err := fc.request(path)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse user info: %w", err)
	}
	return result, nil
}
