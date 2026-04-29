# peyeeye-go

[![Go Reference](https://pkg.go.dev/badge/github.com/peyeeye/peyeeye-go.svg)](https://pkg.go.dev/github.com/peyeeye/peyeeye-go)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Homepage](https://img.shields.io/badge/homepage-peyeeye.ai-E8B4FF.svg)](https://peyeeye.ai)

Official Go client for [**peyeeye.ai**](https://peyeeye.ai) — redact PII on
the way _into_ your LLM prompts and rehydrate it on the way out.

- **Homepage**: <https://peyeeye.ai>
- **API reference**: <https://peyeeye.ai/docs>
- **Go reference**: <https://pkg.go.dev/github.com/peyeeye/peyeeye-go>

```bash
go get github.com/peyeeye/peyeeye-go
```

Go 1.22+. Zero third-party runtime dependencies — just standard library.

## Get an API key

1. Sign up at **<https://peyeeye.ai/signup>** (free plan, no card required —
   1 M characters / month, all 30+ built-in detectors).
2. Head to **<https://peyeeye.ai/dashboard/keys>** → **New key**.
3. Copy the full `pk_live_…` (or `pk_test_…`) token. Export it where your app
   reads env vars:

```bash
export PEYEEYE_KEY=pk_live_...
```

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/peyeeye/peyeeye-go"
)

func main() {
	pe := peyeeye.New(os.Getenv("PEYEEYE_KEY"))
	ctx := context.Background()

	shield := pe.Shield(ctx)
	defer shield.Close(ctx)

	safe, err := shield.Redact(ctx, "Hi, I'm Ada, ada@a-e.com")
	if err != nil { panic(err) }

	// ... send `safe` to the LLM, get `reply` back ...
	reply := callYourLLM(safe)

	out, err := shield.Rehydrate(ctx, reply)
	if err != nil { panic(err) }
	fmt.Println(out)
}
```

`Shield` opens a session, redacts, and tears it down on `Close`. Inside the
shield the same real value always maps to the same token — `Ada Lovelace` is
always `[PERSON_1]` — and tokens never leak across sessions.

## Low-level calls

Skip the `Shield` helper when you need finer control:

```go
r, err := pe.Redact(ctx, "Card: 4242 4242 4242 4242")
// r.Session    → "ses_…"
// r.Entities   → []DetectedEntity{{Token: "[CARD_1]", Type: "CARD", Span: [2]int{6, 25}, Confidence: 0.99}}
text, _ := r.SingleRedacted()  // "Card: [CARD_1]"

clean, err := pe.Rehydrate(ctx, "Confirmation for [CARD_1].", r.Session)
// clean.Text → "Confirmation for 4242 4242 4242 4242."
```

`Redact` returns a single string; use `RedactBatch` when you need multiple
texts processed inside one session:

```go
r, _ := pe.RedactBatch(ctx, []string{"Ada", "ada@a-e.com"})
list, _ := r.ListRedacted()  // []string{"[PERSON_1]", "[EMAIL_1]"}
```

## Stateless sealed mode

Set `Stateless: true` and peyeeye never stores the mapping — the redact
response carries an AES-GCM-sealed `skey_…` blob you hand back to rehydrate.
Nothing lives on the server between calls.

```go
shield := pe.Shield(ctx, peyeeye.ShieldOptions{Stateless: true})
safe, _ := shield.Redact(ctx, "Email ada@a-e.com")
// shield.RehydrationKey is the skey_... blob — persist it if you need to.
clean, _ := shield.Rehydrate(ctx, "Reply: [EMAIL_1]")
```

Or with raw calls:

```go
r, _ := pe.Redact(ctx, "Email ada@a-e.com", peyeeye.WithSession("stateless"))
// r.RehydrationKey → "skey_…"
clean, _ := pe.Rehydrate(ctx, "[EMAIL_1] received.", r.RehydrationKey)
```

## Streaming rehydration

When piping LLM tokens straight to a user, naive rehydration breaks on
mid-token boundaries. `RehydrateChunk` buffers partial placeholders across
chunks; call `Flush` once upstream closes.

```go
shield := pe.Shield(ctx)
defer shield.Close(ctx)

safe, _ := shield.Redact(ctx, prompt)
for chunk := range yourLLMStream(safe) {
	out, _ := shield.RehydrateChunk(ctx, chunk)
	fmt.Print(out)
}
tail, _ := shield.Flush(ctx)
fmt.Print(tail)
```

Never call `Flush` while the stream is still delivering chunks — you'll emit
a half-formed placeholder.

## Streaming redact (SSE)

For the `/v1/redact/stream` endpoint (Build plan and higher):

```go
err := pe.RedactStream(ctx, []string{"Hi, I'm Ada", " — card 4242 4242 4242 4242"},
	func(ev peyeeye.StreamEvent) error {
		switch ev.Event {
		case "session":
			var d struct{ Session string `json:"session"` }
			_ = json.Unmarshal(ev.Data, &d)
		case "redacted":
			var d struct{ Text string; Entities int }
			_ = json.Unmarshal(ev.Data, &d)
			fmt.Println(d.Text)
		case "done":
			// stream is closing
		}
		return nil
	},
)
```

## Custom detectors

```go
floor := 0.9
pe.CreateEntity(ctx, peyeeye.CreateEntityParams{
	ID:              "ORDER_ID",
	Kind:            "regex",
	Pattern:         `#A-\d{6,}`,
	Examples:        []string{"#A-884217", "#A-007431"},
	ConfidenceFloor: &floor,
})

// dry-run a pattern before saving
pe.TestPattern(ctx, `#A-\d{6,}`, "ref #A-884217 and #A-1")
//   → TestPatternResponse{Count: 1, Matches: []PatternMatch{...}}

// inspect / update / retire
pe.ListEntities(ctx)
disabled := false
pe.UpdateEntity(ctx, "ORDER_ID", peyeeye.UpdateEntityParams{Enabled: &disabled})
pe.DeleteEntity(ctx, "ORDER_ID")

// starter templates (Twilio SIDs, Stripe keys, AWS access keys, etc.)
templates, _ := pe.EntityTemplates(ctx)
for _, t := range templates {
	fmt.Println(t.ID, t.Pattern)
}
```

## Sessions

```go
pe.GetSession(ctx, "ses_…")     // *SessionInfo
pe.DeleteSession(ctx, "ses_…")  // drop immediately
```

## Errors

Every non-2xx response (and any transport failure after retries are exhausted)
returns `*peyeeye.Error` with `.Code`, `.Status`, `.Message`, and `.RequestID`.
429 and 5xx responses are retried with exponential backoff (`Retry-After`
honoured); terminal 4xx errors are returned immediately.

```go
r, err := pe.Redact(ctx, text)
if err != nil {
	var pe *peyeeye.Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case "rate_limited":
			// back off
		case "forbidden":
			// plan does not allow this feature
		}
	}
	return err
}
```

## Configuration

```go
pe := peyeeye.New("pk_live_…",
	peyeeye.WithBaseURL("https://api.peyeeye.ai"),
	peyeeye.WithMaxRetries(3),
	peyeeye.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
)
```

`WithHTTPClient` is the right hook for tracing, custom transports, or
test fakes — pass an `*http.Client` whose `Transport` does what you need.

## Method reference

| Method | HTTP | Purpose |
| --- | --- | --- |
| `pe.Redact(ctx, text, ...)` | `POST /v1/redact` | Redact PII; returns token stream + session. |
| `pe.RedactBatch(ctx, texts, ...)` | `POST /v1/redact` | Same, batched in one session. |
| `pe.Rehydrate(ctx, text, session, ...)` | `POST /v1/rehydrate` | Substitute tokens back. Accepts `ses_…` or `skey_…`. |
| `pe.RedactStream(ctx, chunks, fn, ...)` | `POST /v1/redact/stream` (SSE) | Stream-safe redact. |
| `pe.GetSession(ctx, id)` | `GET /v1/sessions/{id}` | Inspect mapping metadata. |
| `pe.DeleteSession(ctx, id)` | `DELETE /v1/sessions/{id}` | Evict a session. |
| `pe.ListEntities(ctx)` | `GET /v1/entities` | Built-ins + your custom detectors. |
| `pe.CreateEntity(ctx, p)` | `POST /v1/entities` | Custom detector. |
| `pe.UpdateEntity(ctx, id, p)` | `PATCH /v1/entities/{id}` | Toggle / tweak. |
| `pe.DeleteEntity(ctx, id)` | `DELETE /v1/entities/{id}` | Retire. |
| `pe.TestPattern(ctx, pattern, text)` | `POST /v1/entities/test` | Dry-run a regex. |
| `pe.EntityTemplates(ctx)` | `GET /v1/entities/templates` | Starter patterns. |
| `pe.Shield(ctx, ...)` | (helper) | Stateful session that pairs Redact + Rehydrate. |

Full request / response schemas: <https://peyeeye.ai/docs>.

## Using this SDK from an AI coding assistant

Drop these into your agent's context. Each snippet is self-contained and
compiles as-is.

```go
// Install
// go get github.com/peyeeye/peyeeye-go

import (
	"context"
	"errors"
	"os"

	"github.com/peyeeye/peyeeye-go"
)

pe := peyeeye.New(os.Getenv("PEYEEYE_KEY"))
ctx := context.Background()

// Round-trip: redact → call LLM → rehydrate (session-scoped)
shield := pe.Shield(ctx)
defer shield.Close(ctx)
safe, _   := shield.Redact(ctx, "Hi, I'm Ada, ada@a-e.com")
out, _    := shield.Rehydrate(ctx, replyFromLLM)

// Stateless (zero server-side state; key is yours to persist)
sh := pe.Shield(ctx, peyeeye.ShieldOptions{Stateless: true})
safe, _   = sh.Redact(ctx, "...")
key      := sh.RehydrationKey  // skey_...
clean, _ := sh.Rehydrate(ctx, "[EMAIL_1] confirmed.")

// Low-level one-shot
r, _     := pe.Redact(ctx, "Card 4242 4242 4242 4242")
text, _  := r.SingleRedacted()
clean2, _ := pe.Rehydrate(ctx, "Receipt: [CARD_1].", r.Session)

// Error handling
_, err := pe.Redact(ctx, "...")
var pee *peyeeye.Error
if errors.As(err, &pee) {
	// pee.Code ∈ {"rate_limited","forbidden","invalid_request","server_error", ...}
	// pee.Status, pee.Message, pee.RequestID
}
```

**Endpoint envelope**: all requests use `Authorization: Bearer <api_key>` against
`https://api.peyeeye.ai/v1/*`. Errors follow `{code, message, request_id}` and
surface as `*peyeeye.Error`. Responses are plain JSON decoded into typed
structs.

**Do**: reuse one `*peyeeye.Client` per process — the underlying `http.Client`
is goroutine-safe.
**Don't**: open a new client per request, call `Flush` mid-stream, or parse
`skey_` blobs yourself — the API opens them.

## License

MIT.
