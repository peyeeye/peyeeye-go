package peyeeye

import (
	"context"
	"regexp"
)

// partialTokenTail catches a placeholder that ends at a chunk boundary, e.g.
// "...[EMAIL" or "...[EMAIL_1" — we hold it back until the bracket closes
// rather than emit a half-formed token.
var partialTokenTail = regexp.MustCompile(`\[[A-Z_0-9]*$`)

// ShieldOptions configures Client.Shield. The zero value uses sensible
// defaults: locale "auto", stateful session, no entity filter, default
// placeholder template.
type ShieldOptions struct {
	Locale      string
	Policy      any
	Entities    []string
	Placeholder string

	// Stateless makes the shield request a sealed skey_… blob instead of a
	// server-held ses_… session. The blob is exposed via Shield.RehydrationKey
	// after the first Redact call and is what subsequent Rehydrate calls send
	// back to the server.
	Stateless bool
}

// Shield pairs Redact + Rehydrate inside a single session. Repeated calls
// share the same token map, so the same input value always resolves to the
// same placeholder. Streaming-safe Rehydrate is provided via RehydrateChunk
// + Flush.
//
// Shield holds mutable state (the captured session id and the streaming
// buffer) and is NOT safe for concurrent use across goroutines.
type Shield struct {
	client    *Client
	opts      ShieldOptions
	stateless bool

	// SessionID is populated after the first Redact call for stateful sessions.
	SessionID string
	// RehydrationKey is populated after the first Redact call when
	// ShieldOptions.Stateless is true.
	RehydrationKey string

	buf string
}

// Shield returns a new Shield bound to opts. Call Close to release the
// server-side session when you're done; on stateless shields Close is a no-op.
func (c *Client) Shield(_ context.Context, opts ...ShieldOptions) *Shield {
	var o ShieldOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	return &Shield{client: c, opts: o, stateless: o.Stateless}
}

func (s *Shield) baseRedactOptions() []RedactOption {
	var opts []RedactOption
	if s.opts.Locale != "" {
		opts = append(opts, WithLocale(s.opts.Locale))
	}
	if s.opts.Policy != nil {
		opts = append(opts, WithPolicy(s.opts.Policy))
	}
	if len(s.opts.Entities) > 0 {
		opts = append(opts, WithEntities(s.opts.Entities...))
	}
	if s.opts.Placeholder != "" {
		opts = append(opts, WithPlaceholder(s.opts.Placeholder))
	}
	switch {
	case s.SessionID != "":
		opts = append(opts, WithSession(s.SessionID))
	case s.stateless:
		opts = append(opts, WithSession("stateless"))
	}
	return opts
}

// Redact runs the shield's redact request and stashes the resulting session
// id (or stateless rehydration key) for subsequent calls.
func (s *Shield) Redact(ctx context.Context, text string) (string, error) {
	r, err := s.client.Redact(ctx, text, s.baseRedactOptions()...)
	if err != nil {
		return "", err
	}
	s.captureSession(r)
	return r.SingleRedacted()
}

// RedactBatch is the slice equivalent of Redact.
func (s *Shield) RedactBatch(ctx context.Context, texts []string) ([]string, error) {
	r, err := s.client.RedactBatch(ctx, texts, s.baseRedactOptions()...)
	if err != nil {
		return nil, err
	}
	s.captureSession(r)
	return r.ListRedacted()
}

func (s *Shield) captureSession(r *RedactResponse) {
	if !s.stateless && s.SessionID == "" && r.Session != "" && r.Session != "stateless" {
		s.SessionID = r.Session
	}
	if r.RehydrationKey != "" {
		s.RehydrationKey = r.RehydrationKey
	}
}

// Rehydrate swaps tokens back to their original values using the shield's
// captured session.
func (s *Shield) Rehydrate(ctx context.Context, text string, opts ...RehydrateOption) (string, error) {
	session := s.session()
	if session == "" {
		return "", &Error{
			Code:    "invalid_request",
			Message: "Shield.Rehydrate called before Redact; no session to use",
		}
	}
	r, err := s.client.Rehydrate(ctx, text, session, opts...)
	if err != nil {
		return "", err
	}
	return r.Text, nil
}

// RehydrateChunk is a streaming-safe Rehydrate: it buffers any trailing
// partial placeholder ("...[EMAIL" without the closing bracket) until the
// next chunk closes it, so the user terminal never sees a half-formed token.
//
// Call Flush exactly once after the upstream stream closes — calling it
// mid-stream emits a partial token.
func (s *Shield) RehydrateChunk(ctx context.Context, chunk string) (string, error) {
	s.buf += chunk
	loc := partialTokenTail.FindStringIndex(s.buf)
	var safe string
	if loc != nil {
		safe, s.buf = s.buf[:loc[0]], s.buf[loc[0]:]
	} else {
		safe, s.buf = s.buf, ""
	}
	if safe == "" {
		return "", nil
	}
	return s.Rehydrate(ctx, safe)
}

// Flush emits any remaining buffered text. Must be called once the upstream
// stream has closed; calling it earlier may emit a half-formed placeholder.
func (s *Shield) Flush(ctx context.Context) (string, error) {
	remainder := s.buf
	s.buf = ""
	if remainder == "" {
		return "", nil
	}
	return s.Rehydrate(ctx, remainder)
}

// Close drops the server-side session for stateful shields. It is a no-op for
// stateless shields and for shields that never made a Redact call. Errors are
// returned but are typically safe to ignore (sessions also expire on their
// own).
func (s *Shield) Close(ctx context.Context) error {
	if s.stateless || s.SessionID == "" {
		return nil
	}
	id := s.SessionID
	s.SessionID = ""
	return s.client.DeleteSession(ctx, id)
}

func (s *Shield) session() string {
	if s.RehydrationKey != "" {
		return s.RehydrationKey
	}
	return s.SessionID
}
