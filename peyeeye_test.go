package peyeeye

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a Client wired to s and a quick-failing HTTP timeout.
func newTestClient(t *testing.T, s *httptest.Server, opts ...Option) *Client {
	t.Helper()
	defaults := []Option{
		WithBaseURL(s.URL),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
		WithMaxRetries(0),
	}
	defaults = append(defaults, opts...)
	return New("pk_test_abc", defaults...)
}

func readBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

// ---------------------------------------------------------------- New

func TestNew_PanicsOnEmptyKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty api key")
		}
	}()
	_ = New("")
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	c := New("pk_test_x", WithBaseURL("https://api.peyeeye.ai///"))
	if c.baseURL != "https://api.peyeeye.ai" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
}

// -------------------------------------------------------------- Redact

func TestRedact_SendsExpectedRequest(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotIdem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/redact" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		gotAuth = r.Header.Get("Authorization")
		gotIdem = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"redacted": "Hi, I'm [PERSON_1].",
			"session": "ses_abc",
			"entities": [{"token":"[PERSON_1]","type":"PERSON","span":[8,20],"confidence":0.98}],
			"latency_ms": 42,
			"expires_at": "2026-05-01T14:27:03Z"
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	r, err := c.Redact(context.Background(), "Hi, I'm Ada Lovelace.",
		WithLocale("en-US"),
		WithPolicy("default"),
		WithIdempotencyKey("req_a1b2c3"),
	)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}

	if gotAuth != "Bearer pk_test_abc" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotIdem != "req_a1b2c3" {
		t.Errorf("Idempotency-Key = %q", gotIdem)
	}
	if gotBody["text"] != "Hi, I'm Ada Lovelace." {
		t.Errorf("text = %v", gotBody["text"])
	}
	if gotBody["locale"] != "en-US" {
		t.Errorf("locale = %v", gotBody["locale"])
	}
	if gotBody["policy"] != "default" {
		t.Errorf("policy = %v", gotBody["policy"])
	}
	if r.Session != "ses_abc" {
		t.Errorf("session = %q", r.Session)
	}
	got, err := r.SingleRedacted()
	if err != nil || got != "Hi, I'm [PERSON_1]." {
		t.Errorf("SingleRedacted = %q, err=%v", got, err)
	}
	if len(r.Entities) != 1 || r.Entities[0].Token != "[PERSON_1]" || r.Entities[0].Span != [2]int{8, 20} {
		t.Errorf("entities = %+v", r.Entities)
	}
}

func TestRedactBatch_AcceptsList(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"redacted": ["[PERSON_1]","[EMAIL_1]"],
			"session": "ses_abc",
			"entities": [],
			"latency_ms": 1
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	r, err := c.RedactBatch(context.Background(), []string{"Ada", "ada@a-e.com"})
	if err != nil {
		t.Fatalf("RedactBatch: %v", err)
	}
	got, err := r.ListRedacted()
	if err != nil {
		t.Fatalf("ListRedacted: %v", err)
	}
	if len(got) != 2 || got[0] != "[PERSON_1]" || got[1] != "[EMAIL_1]" {
		t.Errorf("redacted = %v", got)
	}
	textField, _ := gotBody["text"].([]any)
	if len(textField) != 2 || textField[0] != "Ada" {
		t.Errorf("body.text = %v", gotBody["text"])
	}
}

func TestRedact_OptionalFieldsOnlySentWhenSet(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"redacted":"x","session":"ses_x","entities":[],"latency_ms":0}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.Redact(context.Background(), "hi"); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if _, ok := gotBody["entities"]; ok {
		t.Errorf("entities should not be set in body when not requested")
	}
	if _, ok := gotBody["placeholder"]; ok {
		t.Errorf("placeholder should not be set in body when not requested")
	}
	if _, ok := gotBody["session"]; ok {
		t.Errorf("session should not be set in body when not requested")
	}
	if gotBody["locale"] != "auto" {
		t.Errorf("default locale = %v, want auto", gotBody["locale"])
	}
}

func TestRedact_StatelessReturnsRehydrationKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"redacted":"Email [EMAIL_1]",
			"session":"stateless",
			"entities":[],
			"latency_ms":5,
			"rehydration_key":"skey_deadbeef"
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	r, err := c.Redact(context.Background(), "Email ada@a-e.com", WithSession("stateless"))
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if r.RehydrationKey != "skey_deadbeef" {
		t.Errorf("RehydrationKey = %q", r.RehydrationKey)
	}
}

// ----------------------------------------------------------- Rehydrate

func TestRehydrate_EmptyTextSkipsHTTP(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	r, err := c.Rehydrate(context.Background(), "", "ses_abc")
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}
	if r.Text != "" || r.Replaced != 0 {
		t.Errorf("expected zero response, got %+v", r)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("expected no HTTP call, got %d", hits)
	}
}

func TestRehydrate_SendsSessionAndStrict(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"Hi Ada.","replaced":1,"unknown":[],"latency_ms":7}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	r, err := c.Rehydrate(context.Background(), "Hi [PERSON_1].", "ses_abc", WithStrict(true))
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}
	if r.Text != "Hi Ada." || r.Replaced != 1 {
		t.Errorf("got %+v", r)
	}
	if gotBody["session"] != "ses_abc" || gotBody["strict"] != true {
		t.Errorf("body = %v", gotBody)
	}
}

// ------------------------------------------------------------ Errors

func TestError_4xxMappedAndNotRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("X-Request-Id", "req_xyz")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"code":"forbidden","message":"plan does not allow streaming"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithMaxRetries(3))
	_, err := c.Redact(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*Error)
	if !ok {
		t.Fatalf("err = %T %v", err, err)
	}
	if pe.Code != "forbidden" || pe.Status != 403 || pe.RequestID != "req_xyz" {
		t.Errorf("got %+v", pe)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 hit (no retry on 403), got %d", hits)
	}
}

func TestError_429Retries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"code":"rate_limited","message":"slow down"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"redacted":"x","session":"ses_x","entities":[],"latency_ms":0}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithMaxRetries(5))
	if _, err := c.Redact(context.Background(), "hi"); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("hits = %d, want 3", got)
	}
}

func TestError_500MaxRetriesExceeded(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"code":"server_error","message":"boom"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithMaxRetries(2))
	_, err := c.Redact(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("hits = %d, want 3 (initial + 2 retries)", got)
	}
}

// ----------------------------------------------------------- Sessions

func TestGetSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/ses_abc" || r.Method != http.MethodGet {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ses_abc","locale":"en","policy":"default","chars_processed":42,"entities_detected":3,"created_at":"now","expires_at":"later","expired":false}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s, err := c.GetSession(context.Background(), "ses_abc")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if s.ID != "ses_abc" || s.CharsProcessed != 42 {
		t.Errorf("got %+v", s)
	}
}

func TestDeleteSession_Empty204(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteSession(context.Background(), "ses_abc"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if method != "DELETE" || path != "/v1/sessions/ses_abc" {
		t.Errorf("got %s %s", method, path)
	}
}

// ----------------------------------------------------------- Entities

func TestCreateEntity(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ORDER_ID","kind":"regex","pattern":"#A-\\d+","enabled":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	floor := 0.9
	d, err := c.CreateEntity(context.Background(), CreateEntityParams{
		ID:              "ORDER_ID",
		Pattern:         `#A-\d+`,
		Examples:        []string{"#A-1"},
		ConfidenceFloor: &floor,
	})
	if err != nil {
		t.Fatalf("CreateEntity: %v", err)
	}
	if d.ID != "ORDER_ID" || !d.Enabled {
		t.Errorf("got %+v", d)
	}
	if gotBody["kind"] != "regex" {
		t.Errorf("default kind not sent: %v", gotBody["kind"])
	}
	if gotBody["confidence_floor"] != 0.9 {
		t.Errorf("confidence_floor = %v", gotBody["confidence_floor"])
	}
}

func TestUpdateEntity_OnlySendsSetFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"X","kind":"regex","enabled":false}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	enabled := false
	if _, err := c.UpdateEntity(context.Background(), "X", UpdateEntityParams{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateEntity: %v", err)
	}
	if _, ok := gotBody["pattern"]; ok {
		t.Errorf("pattern should not be in body")
	}
	if gotBody["enabled"] != false {
		t.Errorf("enabled = %v", gotBody["enabled"])
	}
}

// ------------------------------------------------------------ Shield

func TestShield_CapturesSessionAndReuses(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"redacted":"safe","session":"ses_abc","entities":[],"latency_ms":1}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := c.Shield(context.Background())
	if _, err := s.Redact(context.Background(), "hi"); err != nil {
		t.Fatalf("first Redact: %v", err)
	}
	if s.SessionID != "ses_abc" {
		t.Errorf("SessionID = %q", s.SessionID)
	}
	if _, err := s.Redact(context.Background(), "hi again"); err != nil {
		t.Fatalf("second Redact: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 bodies, got %d", len(bodies))
	}
	if _, ok := bodies[0]["session"]; ok {
		t.Errorf("first redact should NOT carry session id")
	}
	if bodies[1]["session"] != "ses_abc" {
		t.Errorf("second redact should reuse ses_abc, got %v", bodies[1]["session"])
	}
}

func TestShield_StatelessKeepsRehydrationKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		switch r.URL.Path {
		case "/v1/redact":
			if b["session"] != "stateless" {
				t.Errorf("redact session = %v", b["session"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"redacted":"safe","session":"stateless","entities":[],"latency_ms":1,"rehydration_key":"skey_xx"}`))
		case "/v1/rehydrate":
			if b["session"] != "skey_xx" {
				t.Errorf("rehydrate session = %v", b["session"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"clean","replaced":1,"unknown":[],"latency_ms":1}`))
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := c.Shield(context.Background(), ShieldOptions{Stateless: true})
	if _, err := s.Redact(context.Background(), "hi"); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if s.RehydrationKey != "skey_xx" {
		t.Errorf("RehydrationKey = %q", s.RehydrationKey)
	}
	out, err := s.Rehydrate(context.Background(), "[PER_1]")
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}
	if out != "clean" {
		t.Errorf("Rehydrate text = %q", out)
	}
}

func TestShield_RehydrateChunkBuffersPartialToken(t *testing.T) {
	var receivedTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path == "/v1/rehydrate" {
			receivedTexts = append(receivedTexts, b["text"].(string))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"` + b["text"].(string) + `","replaced":0,"unknown":[],"latency_ms":1}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"redacted":"safe","session":"ses_abc","entities":[],"latency_ms":1}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := c.Shield(context.Background())
	if _, err := s.Redact(context.Background(), "hi"); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	// Chunk 1 ends mid-token: "...[EMAI" — must be held back.
	out1, err := s.RehydrateChunk(context.Background(), "Reply: [EMAI")
	if err != nil {
		t.Fatalf("Chunk 1: %v", err)
	}
	if out1 != "Reply: " {
		t.Errorf("Chunk 1 emitted %q, want %q", out1, "Reply: ")
	}
	out2, err := s.RehydrateChunk(context.Background(), "L_1] sent.")
	if err != nil {
		t.Fatalf("Chunk 2: %v", err)
	}
	if !strings.Contains(out2, "[EMAIL_1]") {
		t.Errorf("Chunk 2 = %q (expected [EMAIL_1] inside)", out2)
	}
	out3, err := s.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if out3 != "" {
		t.Errorf("Flush = %q, want empty", out3)
	}
	if len(receivedTexts) < 2 {
		t.Errorf("expected ≥2 rehydrate calls, got %d (%v)", len(receivedTexts), receivedTexts)
	}
}

func TestShield_CloseDeletesStatefulSession(t *testing.T) {
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/redact":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"redacted":"x","session":"ses_zzz","entities":[],"latency_ms":1}`))
		default:
			if r.Method == http.MethodDelete {
				deleted = r.URL.Path
				w.WriteHeader(204)
			}
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := c.Shield(context.Background())
	if _, err := s.Redact(context.Background(), "hi"); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if deleted != "/v1/sessions/ses_zzz" {
		t.Errorf("delete path = %q", deleted)
	}
}

// ------------------------------------------------------------ Stream

func TestRedactStream_ParsesSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		if !strings.Contains(string(body), `"chunks":["a","b"]`) {
			t.Errorf("unexpected body: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("event: session\ndata: {\"session\":\"ses_x\"}\n\n"))
		_, _ = w.Write([]byte("event: redacted\ndata: {\"text\":\"hi\",\"entities\":0}\n\n"))
		_, _ = w.Write([]byte("event: done\ndata: {\"chars\":2}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var events []StreamEvent
	err := c.RedactStream(context.Background(), []string{"a", "b"}, func(e StreamEvent) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("RedactStream: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Event != "session" || events[2].Event != "done" {
		t.Errorf("event names: %+v", events)
	}
	var s map[string]string
	if err := json.Unmarshal(events[0].Data, &s); err != nil || s["session"] != "ses_x" {
		t.Errorf("session payload = %v / %v", s, err)
	}
}
