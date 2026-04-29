package peyeeye

import "encoding/json"

// DetectedEntity is one entity span returned from /v1/redact.
type DetectedEntity struct {
	Token      string  `json:"token"`
	Type       string  `json:"type"`
	Span       [2]int  `json:"span"`
	Confidence float64 `json:"confidence"`
	Value      string  `json:"value,omitempty"`
}

// RedactResponse is the response from POST /v1/redact.
//
// Redacted is a string when the request was a single string and a list when
// the request was a list. Use SingleRedacted / ListRedacted for typed access.
//
// RehydrationKey is populated only in stateless mode (session="stateless");
// in that case Session is the literal string "stateless" and the skey_… blob
// lives in RehydrationKey. ExpiresAt is populated only for stateful (ses_…)
// sessions.
type RedactResponse struct {
	Redacted       json.RawMessage  `json:"redacted"`
	Session        string           `json:"session"`
	Entities       []DetectedEntity `json:"entities"`
	LatencyMs      int              `json:"latency_ms"`
	RehydrationKey string           `json:"rehydration_key,omitempty"`
	ExpiresAt      string           `json:"expires_at,omitempty"`
}

// SingleRedacted returns Redacted as a string. It returns an error if the
// underlying JSON is not a string (e.g. if you sent a batch of texts).
func (r *RedactResponse) SingleRedacted() (string, error) {
	var s string
	if err := json.Unmarshal(r.Redacted, &s); err != nil {
		return "", err
	}
	return s, nil
}

// ListRedacted returns Redacted as a string slice.
func (r *RedactResponse) ListRedacted() ([]string, error) {
	var ss []string
	if err := json.Unmarshal(r.Redacted, &ss); err != nil {
		return nil, err
	}
	return ss, nil
}

// RehydrateResponse is the response from POST /v1/rehydrate.
type RehydrateResponse struct {
	Text      string   `json:"text"`
	Replaced  int      `json:"replaced"`
	Unknown   []string `json:"unknown"`
	LatencyMs int      `json:"latency_ms"`
}

// SessionInfo is the response from GET /v1/sessions/:id.
type SessionInfo struct {
	ID               string `json:"id"`
	Locale           string `json:"locale"`
	Policy           string `json:"policy"`
	CharsProcessed   int    `json:"chars_processed"`
	EntitiesDetected int    `json:"entities_detected"`
	CreatedAt        string `json:"created_at,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	Expired          bool   `json:"expired"`
}

// BuiltinEntity describes one detector in the built-in catalog.
type BuiltinEntity struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Sample   string   `json:"sample"`
	Locales  []string `json:"locales"`
}

// CustomDetector is a custom detector registered on an org.
type CustomDetector struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Pattern         string   `json:"pattern,omitempty"`
	Enabled         bool     `json:"enabled"`
	ConfidenceFloor *float64 `json:"confidence_floor,omitempty"`
}

// EntitiesList is the response from GET /v1/entities.
type EntitiesList struct {
	Builtin []BuiltinEntity  `json:"builtin"`
	Custom  []CustomDetector `json:"custom"`
}

// EntityTemplate is a starter-template entry from GET /v1/entities/templates.
type EntityTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Pattern     string `json:"pattern"`
	Example     string `json:"example"`
	Category    string `json:"category"`
}

// PatternMatch is one hit from POST /v1/entities/test.
type PatternMatch struct {
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// TestPatternResponse is the response from POST /v1/entities/test.
type TestPatternResponse struct {
	Matches []PatternMatch `json:"matches"`
	Count   int            `json:"count"`
}

// StreamEvent is a single server-sent event from POST /v1/redact/stream.
//
// Event is one of "session", "redacted", or "done". Data is the parsed JSON
// payload — use json.Unmarshal on the relevant fields, or read it as a
// map[string]any for ad-hoc inspection.
type StreamEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}
