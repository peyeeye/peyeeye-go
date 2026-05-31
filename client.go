package peyeeye

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production peyeeye API host.
const DefaultBaseURL = "https://api.peyeeye.ai"

// Version of this SDK. Reflected in the User-Agent header.
const Version = "1.1.3"

const userAgent = "peyeeye-go/" + Version

// Client is the entry point for the peyeeye API. It is safe for concurrent
// use by multiple goroutines.
type Client struct {
	apiKey         string
	baseURL        string
	httpClient     *http.Client
	maxRetries     int
	defaultHeaders http.Header
}

// Option configures a Client passed to New.
type Option func(*Client)

// WithBaseURL overrides the API host. Trailing slashes are stripped.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient supplies a custom *http.Client. Useful for adding tracing,
// custom transports, or per-request timeouts via http.Client.Timeout. The
// default client has a 30-second timeout.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithMaxRetries sets the retry ceiling for 429 / 5xx responses and transport
// errors. Default is 3.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithDefaultHeaders merges extra headers onto every request. Authorization
// and User-Agent are always set by the SDK and cannot be overridden here.
func WithDefaultHeaders(h http.Header) Option {
	return func(c *Client) { c.defaultHeaders = h.Clone() }
}

// New returns a Client. apiKey is required and must be a pk_live_… or
// pk_test_… token from https://peyeeye.ai/dashboard/keys. It panics if apiKey
// is empty — that mirrors the Python and TypeScript SDKs and surfaces config
// bugs at startup rather than as 401s in production.
func New(apiKey string, opts ...Option) *Client {
	if apiKey == "" {
		panic("peyeeye: api key is required")
	}
	c := &Client{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: 3,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	return c
}

// retryableStatuses are HTTP statuses we re-issue with backoff. Other 4xx are
// caller errors and surface immediately.
var retryableStatuses = map[int]bool{
	429: true, 500: true, 502: true, 503: true, 504: true,
}

// requestOpts holds per-call switches for the internal request method.
type requestOpts struct {
	method         string
	path           string
	body           any
	headers        http.Header
	idempotencyKey string
	allowEmpty     bool
}

// do executes a JSON request with retry/backoff and decodes the response into
// out (which may be nil if the caller does not need the body).
func (c *Client) do(ctx context.Context, opts requestOpts, out any) error {
	var bodyBytes []byte
	if opts.body != nil {
		var err error
		bodyBytes, err = json.Marshal(opts.body)
		if err != nil {
			return fmt.Errorf("peyeeye: marshal body: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, opts.method, opts.path, bodyBytes, opts.headers, opts.idempotencyKey)
		if err != nil {
			return err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < c.maxRetries {
				if sleepErr := sleepBackoff(ctx, attempt, ""); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return &Error{Code: "network_error", Message: err.Error()}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return decodeSuccess(resp, out, opts.allowEmpty)
		}

		retryable := retryableStatuses[resp.StatusCode]
		if retryable && attempt < c.maxRetries {
			retryAfter := resp.Header.Get("Retry-After")
			drainAndClose(resp)
			if sleepErr := sleepBackoff(ctx, attempt, retryAfter); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		return errorFromResponse(resp)
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte, headers http.Header, idempotencyKey string) (*http.Request, error) {
	url := c.baseURL + path
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, fmt.Errorf("peyeeye: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range c.defaultHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return req, nil
}

func decodeSuccess(resp *http.Response, out any, allowEmpty bool) error {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Error{Code: "network_error", Status: resp.StatusCode, Message: err.Error()}
	}
	if len(body) == 0 {
		if allowEmpty {
			return nil
		}
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{
			Code:    "invalid_response",
			Status:  resp.StatusCode,
			Message: fmt.Sprintf("decode response: %v", err),
		}
	}
	return nil
}

func errorFromResponse(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Detail    string `json:"detail"`
		RequestID string `json:"request_id"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &env)
	}
	code := env.Code
	if code == "" {
		code = codeForStatus(resp.StatusCode)
	}
	msg := env.Message
	if msg == "" {
		msg = env.Detail
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	if msg == "" {
		msg = "Error"
	}
	requestID := resp.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = env.RequestID
	}
	return &Error{Code: code, Status: resp.StatusCode, Message: msg, RequestID: requestID}
}

func codeForStatus(s int) string {
	switch s {
	case 400:
		return "invalid_request"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "idempotency_conflict"
	case 413:
		return "payload_too_large"
	case 429:
		return "rate_limited"
	default:
		if s >= 500 {
			return "server_error"
		}
		return "error"
	}
}

func drainAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// sleepBackoff implements the same exponential-with-jitter backoff used by the
// Python and TypeScript SDKs, capped at 15s. A Retry-After header (seconds)
// overrides when present.
func sleepBackoff(ctx context.Context, attempt int, retryAfter string) error {
	var delay time.Duration
	if retryAfter != "" {
		if secs, err := strconv.ParseFloat(retryAfter, 64); err == nil && secs >= 0 {
			delay = time.Duration(secs * float64(time.Second))
		}
	}
	if delay == 0 {
		base := 250 * time.Millisecond * (1 << attempt)
		jitter := time.Duration(rand.Float64() * float64(base) * 0.1)
		delay = base + jitter
	}
	if delay > 15*time.Second {
		delay = 15 * time.Second
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
