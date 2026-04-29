// Package peyeeye is the official Go client for the peyeeye.ai PII redaction
// and rehydration API.
//
// Redact PII on the way into your LLM prompts, then rehydrate it on the way
// back out. Sessions are server-held by default (ses_…); pass the "stateless"
// session to receive an AES-GCM-sealed skey_… blob you can persist instead.
//
//	pe := peyeeye.New("pk_live_…")
//
//	shield, err := pe.Shield(ctx)
//	if err != nil { return err }
//	defer shield.Close(ctx)
//
//	safe, err := shield.Redact(ctx, "Hi, I'm Ada, ada@a-e.com")
//	if err != nil { return err }
//	reply := callYourLLM(safe)
//	out, err := shield.Rehydrate(ctx, reply)
//
// Single import path; no third-party runtime dependencies.
package peyeeye
