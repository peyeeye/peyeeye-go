package peyeeye

import "fmt"

// Error is returned for any non-2xx response from the peyeeye API, and for
// transport-layer failures after retries are exhausted.
//
// Code is a stable machine-readable code from the API envelope, e.g.
// "rate_limited", "forbidden", "invalid_request", "server_error",
// "unauthorized", "not_found", "idempotency_conflict", "payload_too_large".
// Network and parse failures use "network_error" with Status=0.
//
// Status is the HTTP status (0 for transport errors). RequestID is the value
// of the X-Request-Id response header when present.
type Error struct {
	Code      string
	Status    int
	Message   string
	RequestID string
}

func (e *Error) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("peyeeye: %s (code=%s, status=%d, request_id=%s)",
			e.Message, e.Code, e.Status, e.RequestID)
	}
	return fmt.Sprintf("peyeeye: %s (code=%s, status=%d)", e.Message, e.Code, e.Status)
}
