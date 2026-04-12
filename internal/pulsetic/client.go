// Package pulsetic is a minimal read-only client for Pulsetic's public API.
//
// The client captures everything an audit writer needs: method, path, query,
// status, duration, raw body bytes, and a SHA-256 of those bytes. Typed
// parsing is kept deliberately thin (just IDs) so the tool is not coupled
// to Pulsetic's full response schema, which may evolve.
package pulsetic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// DefaultBaseURL is Pulsetic's public API root. Auth is a raw token sent
// in the Authorization header - no Bearer prefix (verified against the docs).
const DefaultBaseURL = "https://api.pulsetic.com/api/public"

// Client is a thin wrapper around http.Client. Zero values are not usable;
// construct via New.
type Client struct {
	BaseURL   string
	Token     string
	HTTP      *http.Client
	RetryMax  int
	RetryBase time.Duration
	UserAgent string

	now func() time.Time // injectable for tests
}

// Options configures New. Zero values pick sensible defaults.
type Options struct {
	BaseURL   string
	Token     string
	Timeout   time.Duration
	RetryMax  int
	RetryBase time.Duration
	UserAgent string
}

// New constructs a Client. Token is required; everything else defaults.
func New(opts Options) (*Client, error) {
	if opts.Token == "" {
		return nil, errors.New("pulsetic: token is required")
	}
	base := opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	retryMax := opts.RetryMax
	if retryMax == 0 {
		retryMax = 3
	}
	retryBase := opts.RetryBase
	if retryBase == 0 {
		retryBase = 500 * time.Millisecond
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "pulsetic-cli/0.1"
	}
	return &Client{
		BaseURL:   base,
		Token:     opts.Token,
		HTTP:      &http.Client{Timeout: timeout},
		RetryMax:  retryMax,
		RetryBase: retryBase,
		UserAgent: ua,
		now:       time.Now,
	}, nil
}

// Call is the outcome of one logical API request, including retries.
// It has everything an audit writer needs to build a record.
type Call struct {
	Method     string
	Path       string
	Query      map[string]string
	Status     int
	DurationMS int64
	Body       []byte
	BodySHA256 string
	Attempts   int
}

// Do executes an HTTP request against the given path. Retries on transport
// errors, 429, and 5xx up to RetryMax with exponential backoff, honouring
// Retry-After when present. Non-retryable HTTP responses (4xx other than
// 429) are returned as a successful Call with the real Status - the
// caller decides whether to treat that as fatal.
//
// Pass nil for reqBody on GET/DELETE; pass a JSON byte slice for POST/PUT.
func (c *Client) Do(ctx context.Context, method, path string, query map[string]string, reqBody []byte) (*Call, error) {
	fullURL, err := c.buildURL(path, query)
	if err != nil {
		return nil, fmt.Errorf("pulsetic: build url: %w", err)
	}

	var lastErr error
	start := c.now()

	for attempt := 1; attempt <= c.RetryMax+1; attempt++ {
		var bodyReader io.Reader
		if reqBody != nil {
			bodyReader = bytes.NewReader(reqBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("pulsetic: new request: %w", err)
		}
		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		// Pulsetic's docs show the token passed directly in Authorization,
		// with no "Bearer " prefix. Do not change this.
		req.Header.Set("Authorization", c.Token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if !c.shouldRetry(attempt, 0) {
				return nil, fmt.Errorf("pulsetic: transport: %w", err)
			}
			c.sleepBackoff(ctx, attempt, 0)
			continue
		}

		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			if !c.shouldRetry(attempt, 0) {
				return nil, fmt.Errorf("pulsetic: read body: %w", rerr)
			}
			c.sleepBackoff(ctx, attempt, 0)
			continue
		}

		if isRetryable(resp.StatusCode) && attempt <= c.RetryMax {
			c.sleepBackoff(ctx, attempt, retryAfterSeconds(resp.Header.Get("Retry-After")))
			continue
		}

		sum := sha256.Sum256(body)
		return &Call{
			Method:     method,
			Path:       path,
			Query:      query,
			Status:     resp.StatusCode,
			DurationMS: c.now().Sub(start).Milliseconds(),
			Body:       body,
			BodySHA256: hex.EncodeToString(sum[:]),
			Attempts:   attempt,
		}, nil
	}

	if lastErr == nil {
		lastErr = errors.New("pulsetic: exhausted retries")
	}
	return nil, lastErr
}

func (c *Client) buildURL(path string, query map[string]string) (string, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(rel)
	// Ensure the joined path keeps the /api/public prefix when callers
	// pass a leading slash.
	if rel.Path != "" && rel.Path[0] == '/' {
		u.Path = base.Path + rel.Path
	}
	if len(query) > 0 {
		q := u.Query()
		// Stable order is nice for human-readable logs and tests.
		keys := make([]string, 0, len(query))
		for k := range query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			q.Set(k, query[k])
		}
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

func (c *Client) shouldRetry(attempt int, _ int) bool {
	return attempt <= c.RetryMax
}

func (c *Client) sleepBackoff(ctx context.Context, attempt int, retryAfter int) {
	delay := c.RetryBase << (attempt - 1) // 500ms, 1s, 2s, ...
	if retryAfter > 0 {
		forced := time.Duration(retryAfter) * time.Second
		if forced > delay {
			delay = forced
		}
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func retryAfterSeconds(h string) int {
	if h == "" {
		return 0
	}
	n, err := strconv.Atoi(h)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
