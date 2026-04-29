package peyeeye

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamOption tunes a RedactStream call. Reuses the redact-side options for
// locale and policy; entities, placeholder, session, and idempotency_key are
// not accepted by /v1/redact/stream.
type StreamOption func(*streamConfig)

type streamConfig struct {
	locale    string
	policy    any
	hasLocale bool
	hasPolicy bool
}

// StreamWithLocale sets the BCP-47 language tag.
func StreamWithLocale(s string) StreamOption {
	return func(c *streamConfig) { c.locale, c.hasLocale = s, true }
}

// StreamWithPolicy sets a saved-policy name or inline policy object.
func StreamWithPolicy(p any) StreamOption {
	return func(c *streamConfig) { c.policy, c.hasPolicy = p, true }
}

// RedactStream stream-redacts an iterable of chunks over Server-Sent Events.
// fn is invoked once per parsed event; return a non-nil error from fn to stop
// the stream early.
//
// The first event is always {Event: "session", Data: {"session": "ses_…"}};
// subsequent events are "redacted" (per chunk) and a final "done". Requires
// the Build plan or higher; lower plans return 403.
func (c *Client) RedactStream(ctx context.Context, chunks []string, fn func(StreamEvent) error, opts ...StreamOption) error {
	cfg := streamConfig{locale: "auto"}
	for _, opt := range opts {
		opt(&cfg)
	}
	body := map[string]any{"chunks": chunks, "locale": cfg.locale}
	if cfg.hasPolicy {
		body["policy"] = cfg.policy
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("peyeeye: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/redact/stream", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("peyeeye: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, vs := range c.defaultHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Code: "network_error", Message: err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errorFromResponse(resp)
	}
	defer resp.Body.Close()

	return parseSSE(resp.Body, fn)
}

func parseSSE(r io.Reader, fn func(StreamEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var event string
	var dataLines []string
	emit := func() error {
		if event == "" || len(dataLines) == 0 {
			event, dataLines = "", nil
			return nil
		}
		raw := strings.Join(dataLines, "\n")
		ev := StreamEvent{Event: event, Data: json.RawMessage(raw)}
		if !json.Valid([]byte(raw)) {
			// Encode as a JSON string so callers can still consume it.
			b, _ := json.Marshal(map[string]string{"raw": raw})
			ev.Data = b
		}
		event, dataLines = "", nil
		return fn(ev)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := emit(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if v, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimPrefix(v, " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return &Error{Code: "network_error", Message: err.Error()}
	}
	return emit()
}
