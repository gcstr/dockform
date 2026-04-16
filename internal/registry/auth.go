package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
)

// tokenCache caches bearer tokens keyed by scope string.
type tokenCache struct {
	mu     sync.RWMutex
	tokens map[string]string
}

func newTokenCache() *tokenCache {
	return &tokenCache{tokens: make(map[string]string)}
}

func (c *tokenCache) get(scope string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tokens[scope]
	return t, ok
}

func (c *tokenCache) set(scope string, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[scope] = token
}

// challenge represents a parsed WWW-Authenticate challenge.
type challenge struct {
	Realm   string
	Service string
	Scope   string
}

// parseChallenge parses the WWW-Authenticate header value.
// Expected format: Bearer realm="...",service="...",scope="..."
func parseChallenge(header string) (challenge, error) {
	const op = "registry.parseChallenge"

	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "Bearer ") {
		return challenge{}, apperr.New(op, apperr.External, "unsupported auth scheme: %s", header)
	}

	header = strings.TrimPrefix(header, "Bearer ")
	var ch challenge

	for _, part := range splitParams(header) {
		part = strings.TrimSpace(part)
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		switch key {
		case "realm":
			ch.Realm = value
		case "service":
			ch.Service = value
		case "scope":
			ch.Scope = value
		}
	}

	if ch.Realm == "" {
		return challenge{}, apperr.New(op, apperr.External, "missing realm in WWW-Authenticate header")
	}

	return ch, nil
}

// splitParams splits a comma-separated header value, respecting quoted strings.
func splitParams(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false

	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// tokenResponse is the JSON structure returned by token endpoints.
type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

// fetchToken requests a bearer token from the auth realm.
func fetchToken(ctx context.Context, client *http.Client, ch challenge) (string, error) {
	const op = "registry.fetchToken"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ch.Realm, nil)
	if err != nil {
		return "", apperr.Wrap(op, apperr.Internal, err, "building token request")
	}

	q := req.URL.Query()
	if ch.Service != "" {
		q.Set("service", ch.Service)
	}
	if ch.Scope != "" {
		q.Set("scope", ch.Scope)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return "", apperr.Wrap(op, apperr.Unavailable, err, "requesting token from %s", ch.Realm)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", apperr.New(op, apperr.External, "token endpoint returned %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", apperr.Wrap(op, apperr.External, err, "decoding token response")
	}

	token := tr.Token
	if token == "" {
		token = tr.AccessToken
	}
	if token == "" {
		return "", apperr.New(op, apperr.External, "empty token in response from %s", ch.Realm)
	}

	return token, nil
}

// doWithAuth executes an HTTP request with automatic token-based authentication.
// If the initial request returns 401, it parses the WWW-Authenticate challenge,
// fetches a bearer token, and retries the request.
func doWithAuth(ctx context.Context, client *http.Client, cache *tokenCache, req *http.Request) (*http.Response, error) {
	const op = "registry.doWithAuth"

	// Build a scope key from the request for cache lookup.
	scopeKey := fmt.Sprintf("%s:%s", req.URL.Host, req.URL.Path)

	// Try with cached token first.
	if token, ok := cache.get(scopeKey); ok {
		req2 := req.Clone(ctx)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req2)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.Unavailable, err, "executing authenticated request")
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		_ = resp.Body.Close()
		// Cached token is stale, fall through to re-auth.
	}

	// Try unauthenticated.
	resp, err := client.Do(req)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.Unavailable, err, "executing request to %s", req.URL.String())
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	_ = resp.Body.Close()

	// Parse challenge and fetch token.
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth == "" {
		return nil, apperr.New(op, apperr.External, "received 401 but no WWW-Authenticate header")
	}

	ch, err := parseChallenge(wwwAuth)
	if err != nil {
		return nil, err
	}

	token, err := fetchToken(ctx, client, ch)
	if err != nil {
		return nil, err
	}

	// Cache the token using the challenge scope if available, otherwise use our key.
	cacheKey := scopeKey
	if ch.Scope != "" {
		cacheKey = ch.Scope
	}
	cache.set(cacheKey, token)
	// Also cache under scopeKey so subsequent requests to the same path hit cache.
	if cacheKey != scopeKey {
		cache.set(scopeKey, token)
	}

	// Retry with token.
	req2 := req.Clone(ctx)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.Unavailable, err, "executing authenticated retry")
	}

	return resp2, nil
}
