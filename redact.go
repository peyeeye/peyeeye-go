package peyeeye

import "context"

// RedactOption tunes a single Redact / RedactBatch call.
type RedactOption func(*redactConfig)

type redactConfig struct {
	locale         string
	policy         any
	entities       []string
	placeholder    string
	session        string
	idempotencyKey string

	// only one of these flags is consulted by Redact / RedactBatch.
	hasLocale      bool
	hasPolicy      bool
	hasEntities    bool
	hasPlaceholder bool
	hasSession     bool
}

// WithLocale sets the BCP-47 language tag for detection. Defaults to "auto".
func WithLocale(s string) RedactOption {
	return func(c *redactConfig) { c.locale, c.hasLocale = s, true }
}

// WithPolicy sets a saved-policy name or an inline policy object (map / struct).
func WithPolicy(p any) RedactOption {
	return func(c *redactConfig) { c.policy, c.hasPolicy = p, true }
}

// WithEntities restricts detection to the given entity IDs.
func WithEntities(ids ...string) RedactOption {
	return func(c *redactConfig) {
		c.entities = append([]string(nil), ids...)
		c.hasEntities = true
	}
}

// WithPlaceholder overrides the token template, e.g. "<{TYPE}>".
func WithPlaceholder(s string) RedactOption {
	return func(c *redactConfig) { c.placeholder, c.hasPlaceholder = s, true }
}

// WithSession reuses an existing ses_… id, or pass "stateless" for a sealed
// skey_… response.
func WithSession(s string) RedactOption {
	return func(c *redactConfig) { c.session, c.hasSession = s, true }
}

// WithIdempotencyKey sends the value as the Idempotency-Key request header.
// The server replays the original response when the same key is reused with
// the same body within its window.
func WithIdempotencyKey(s string) RedactOption {
	return func(c *redactConfig) { c.idempotencyKey = s }
}

// Redact redacts PII from a single string.
func (c *Client) Redact(ctx context.Context, text string, opts ...RedactOption) (*RedactResponse, error) {
	return c.redact(ctx, text, opts)
}

// RedactBatch redacts a batch of strings within one session. The response's
// Redacted field will be a JSON array; use RedactResponse.ListRedacted to
// decode.
func (c *Client) RedactBatch(ctx context.Context, texts []string, opts ...RedactOption) (*RedactResponse, error) {
	return c.redact(ctx, texts, opts)
}

func (c *Client) redact(ctx context.Context, text any, opts []RedactOption) (*RedactResponse, error) {
	cfg := redactConfig{locale: "auto"}
	for _, opt := range opts {
		opt(&cfg)
	}

	body := map[string]any{"text": text}
	// locale is always sent (matches Python SDK default).
	if cfg.hasLocale {
		body["locale"] = cfg.locale
	} else {
		body["locale"] = "auto"
	}
	if cfg.hasPolicy {
		body["policy"] = cfg.policy
	}
	if cfg.hasEntities {
		body["entities"] = cfg.entities
	}
	if cfg.hasPlaceholder {
		body["placeholder"] = cfg.placeholder
	}
	if cfg.hasSession {
		body["session"] = cfg.session
	}

	var out RedactResponse
	err := c.do(ctx, requestOpts{
		method:         "POST",
		path:           "/v1/redact",
		body:           body,
		idempotencyKey: cfg.idempotencyKey,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RehydrateOption tunes a single Rehydrate call.
type RehydrateOption func(*rehydrateConfig)

type rehydrateConfig struct {
	strict bool
}

// WithStrict makes Rehydrate fail when an unknown placeholder is encountered.
// Default is lenient (unknown tokens are returned untouched in the Unknown
// list).
func WithStrict(b bool) RehydrateOption {
	return func(c *rehydrateConfig) { c.strict = b }
}

// Rehydrate substitutes redaction tokens back to their original values.
//
// session may be either a stateful ses_… id from a prior Redact response, or
// a stateless skey_… blob returned in RehydrationKey. Empty text short-
// circuits and does not issue an HTTP call.
func (c *Client) Rehydrate(ctx context.Context, text, session string, opts ...RehydrateOption) (*RehydrateResponse, error) {
	if text == "" {
		return &RehydrateResponse{Unknown: []string{}}, nil
	}
	cfg := rehydrateConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	body := struct {
		Text    string `json:"text"`
		Session string `json:"session"`
		Strict  bool   `json:"strict"`
	}{Text: text, Session: session, Strict: cfg.strict}

	var out RehydrateResponse
	err := c.do(ctx, requestOpts{
		method: "POST",
		path:   "/v1/rehydrate",
		body:   body,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Unknown == nil {
		out.Unknown = []string{}
	}
	return &out, nil
}
