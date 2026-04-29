# AGENTS.md — peyeeye-go

Orientation for AI coding agents working on the official Go SDK for peyeeye.ai. Humans: read `README.md` first.

## What this is

`github.com/peyeeye/peyeeye-go` — synchronous Go client for the peyeeye PII redaction / rehydration API. Mirrors the full `/v1/*` surface: redact, rehydrate, streaming SSE, sessions, custom detectors, stateless sealed mode. Module path is `github.com/peyeeye/peyeeye-go`; current version `1.0.0`.

## Layout

```
sdks/go/
  doc.go             Package doc
  client.go          Client + options + HTTP plumbing + retry
  errors.go          *Error type
  types.go           Response shapes (RedactResponse, RehydrateResponse, …)
  redact.go          Redact / RedactBatch / Rehydrate
  sessions.go        GetSession / DeleteSession
  entities.go        Entity CRUD + TestPattern + EntityTemplates
  shield.go          Shield helper (paired Redact + Rehydrate inside one session)
  stream.go          RedactStream (SSE)
  peyeeye_test.go    Tests using net/http/httptest fakes
  go.mod             Single module; no third-party deps
```

## Run the tests

```
go test ./...
```

Tests use `net/http/httptest`. No network. No fixtures. Each test stands up its own server.

## Load-bearing invariants

- **Zero runtime deps.** Standard library only. Do not add `httpx`-equivalents like `resty`, `go-resty`, or `oapi-codegen`. Users install this into LLM pipelines where fewer deps is strictly better.
- **Streaming partial-token buffer.** `partialTokenTail = regexp.MustCompile(\`\\[[A-Z_0-9]*$\`)` in `shield.go`. When an LLM streams back `...[EMAIL` and the `_1]` arrives in the next chunk, `Shield.RehydrateChunk` holds the tail until the bracket closes. Breaking this prints literal `[EMAIL_1]` to end users. `Flush` must only be called after the upstream stream closes — calling it mid-stream emits a partial token.
- **`Shield` is NOT goroutine-safe.** It holds `SessionID`, `RehydrationKey`, and the streaming buffer as mutable state. The underlying `*Client` IS goroutine-safe (it just wraps `http.Client`).
- **Retry policy.** `retryableStatuses = {429, 500, 502, 503, 504}` (in `client.go`). Honors `Retry-After` (seconds) when present, otherwise exponential backoff with jitter capped at 15s. `WithMaxRetries` default is 3. Never retry 4xx other than 429 — those are caller errors.
- **`Idempotency-Key` is caller-supplied** via `WithIdempotencyKey`, never auto-generated. Mutating the body between retries with the same key is the caller's problem, not ours.
- **`New` panics on empty `apiKey`.** Mirrors Python's `ValueError`. Surfaces config bugs at startup rather than as 401s in production. Don't soften this to a sentinel error.
- **`*Error` is the only error type returned from API calls.** Callers do `errors.As(err, &pe)`. Don't wrap it in `fmt.Errorf("...: %w", err)` from inside the SDK — that breaks the type-assertion ergonomics.

## Auth

Bearer API key in the `Authorization` header. `pk_live_…` = prod, `pk_test_…` = test. `apiKey` is required at construction. Never log or echo the key.

## Stateless sealed mode

`pe.Redact(ctx, text, peyeeye.WithSession("stateless"))` → response includes `RehydrationKey: "skey_…"`. Pass the `skey_…` back as the `session` arg of `Rehydrate`. The blob is AES-GCM-sealed server-side; the SDK just pipes bytes. Don't try to parse or validate `skey_…` — treat it as opaque.

## Versioning

SemVer. Breaking changes → major bump. The `v1` in the URL is the HTTP API version, not the SDK version — they're independent. The `Version` constant in `client.go` must match the published Go module tag and is reflected in the User-Agent.

## What NOT to do

- **Never** add a runtime dep beyond the standard library. Test deps are fine if needed but currently we don't have any.
- **Never** include `apiKey` in error messages — it's not in `*Error.Error()`.
- **Never** stash `Shield` state on the `*Client` — the client is shared across goroutines; per-call state belongs on `Shield`.
- **Do not** add an async/await variant — Go is already concurrent. Callers can `go pe.Redact(...)` themselves.
- **Do not** introduce a code generator (oapi-codegen, etc.). The hand-written SDK is the product; generated clients drift from the language idioms.
- **Do not** invent endpoints. <https://peyeeye.ai/docs> and the backend's `api/urls.py` are the source of truth.

## Where to look

- Client + retry + HTTP plumbing: `client.go`
- Typed errors: `errors.go`
- Response models: `types.go`
- Public API docs: <https://peyeeye.ai/docs>
- Sister SDKs (consult for behavioural parity): `../python/`, `../typescript/`
