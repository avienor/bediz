# 02: Poll one Anima execution to a completed Execution Receipt

**What to build:** Make `generate` wait by default for the single queue item accepted by ticket 01, polling InvokeAI through read-only requests until a terminal state. A completed item returns an Execution Receipt whose ordered output associates the resolved seed with a normalized Image Reference and accessible InvokeAI URLs. A conclusive failed or cancelled item returns a structured operation failure without exposing a raw traceback. `--no-wait` retains the accepted-receipt behavior. Waiting has no total timeout unless `--timeout` is supplied; timeout or local interruption stops only Bediz and never cancels the queue item.

**Blocked by:** 01: Submit one exact Anima Generation Request.

**Execution route:** `worker + independent review` — the contract is resolved and fake queue transitions plus a live baseline can reliably detect incorrect polling, cancellation, or receipt behavior.

**Verification gate:** Public tests exercise pending, in-progress, completed, failed, cancelled, malformed, timeout, and context-cancelled states. They prove polling is read-only and retryable, enqueue is not repeated, no cancellation endpoint is called, JSON stdout remains one final envelope, completed image hydration is normalized, and errors contain queue identifiers without server tracebacks. A live one-output generation must reach completion and return an accessible Image Reference. All applicable project verification commands must pass.

**Escalate when:** InvokeAI exposes an undocumented terminal status; completed items do not expose image outputs through the tested queue result; output metadata contradicts the resolved seed; timeout cannot be separated from bounded per-request transport timeouts; a requirement emerges to cancel on interruption; or two repair attempts fail for the same underlying reason.

**Permanent records:** V1 spec and tests — record terminal-state normalization, local wait timeout behavior, and completed Execution Receipt evidence. ADR-0008 and ADR-0009 remain unchanged.

**Status:** done

- [x] Default `generate` polls the accepted item until completion with no total timeout unless the caller supplies `--timeout`.
- [x] `--timeout` returns `wait_timeout` with queue identifiers and states that the remote item was not cancelled.
- [x] Local interruption returns exit status 130 and preserves the accepted remote item.
- [x] Failed and cancelled items return `invokeai_operation_failed` with a concise normalized error and no raw execution graph or server traceback.
- [x] A completed item produces one output containing the expected `item_id`, resolved `seed`, and normalized Image Reference.
- [x] Read-only polling may retry transient reads, but enqueue remains a single mutation and is never replayed.
- [x] The narrow behavior tests and `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass; live completion evidence is recorded.

## Comments

`generate` now waits for the accepted queue item by default. `generation.Wait`
polls the item endpoint through the existing read-only `queue.Get` path — first
immediately, then backing off from 100 ms to one second — until the item reaches
a terminal state, and returns the Execution Receipt with one output record that
carries the polled `item_id`, the resolved seed at the same position in
`resolved_settings.seeds`, and the normalized Image Reference with absolute
InvokeAI image and thumbnail URLs. `--no-wait` keeps the accepted receipt and
never polls.

Terminal-state normalization follows the InvokeAI 6.14 queue-item enum:
`pending`, `in_progress`, and `waiting` keep polling (`waiting` is a suspended
workflow-call parent), `completed` hydrates the single image output, `failed`
and `canceled` return `invokeai_operation_failed` with the concise InvokeAI
error type and message, and any other status returns `invalid_invokeai_response`
instead of being treated as terminal or polled indefinitely. A completed item
that exposes zero or several image outputs is likewise rejected rather than
guessed.

`--timeout` bounds only the local wait, since ADR-0008 separates the optional
caller deadline from bounded per-request transport timeouts; each HTTP request
still uses the client's bounded timeout. An elapsed deadline returns
`wait_timeout` (exit status 6), and a canceled context returns `interrupted`
(exit status 130), both with the accepted `queue_id`, `batch_id`, and ordered
`item_ids`. Neither path calls a cancellation endpoint, and the enqueue mutation
is sent exactly once. New public vocabulary: the `wait_timeout` error code, and
`operation.QueuePosition`, `WaitTimeoutError`, `InterruptedError`,
`ItemFailureError`, and `InvalidQueueResultError` as the classified wait-path
failures.

Repository verification passed:

```text
go test ./...        ok
go test -race ./...  ok
go vet ./...         clean
go mod verify        all modules verified
```

Live verification against InvokeAI 6.14.1 completed a one-output Anima
generation through the public CLI: batch
`639442a0-5455-4177-89d3-02436a9202a1`, item `7`, resolved seed `123456789`, and
image `514ce3d7-8916-49d1-aab1-14c9dfe3b993.png` returned in 8.5 s. The receipt
URLs were accessible (`/full` returned HTTP 200 `image/png`, 176349 bytes;
`/thumbnail` returned HTTP 200 `image/webp`), and the InvokeAI image metadata
reported the same seed, dimensions, steps, scheduler, and guidance, so the
resolved seed and the Image Reference agree.

Two further live runs recorded the local-only stop behavior. With
`--timeout 1ms`, Bediz returned `wait_timeout` for item `8` with exit status 6,
and that item subsequently completed in InvokeAI
(`5046395e-8d77-4f9b-80e5-27980e62ab5e.png`). Sending SIGINT during the wait for
item `9` returned `interrupted` with exit status 130, and item `9` subsequently
completed with image metadata seed `4242`, matching the resolved seed.

Re-running those two cases with the final binary reproduced both outcomes with
the published wording — `"...queue item 10 reached a terminal state; the
InvokeAI item was not canceled"` and `"waiting for queue item 11 was interrupted
locally; the InvokeAI item was not canceled"` — and items `10` and `11` both
completed afterwards (`f01e5797-f70b-4a9e-b77a-57f670ebe157.png` and
`39c50d01-655c-4b2b-ac7f-d7c9cf1630a4.png`). The interrupted run's image
metadata reported seed `4242`, matching its resolved seed, and its full-image
URL answered HTTP 200 `image/png`, 366513 bytes.

The two-axis review returned no hard standard violations and no specification
failures. Its findings were addressed: the wait-path preconditions and a
negative wait timeout now return typed `invalid_request` errors from the domain
module, the receipt envelope type is shared by the accepted and completed
receipt tests instead of declared twice, test names and new prose use InvokeAI's
`canceled` spelling, the fake wait server tracks one poll counter, and a
transient
service-unavailable poll failure now proves polling retryability at the CLI
seam. The single-output seed association remains positional: the tracer polls
one item for one resolved seed, the live runs confirmed the image metadata seed
matches it, and verified multi-output association belongs to ticket 04.
