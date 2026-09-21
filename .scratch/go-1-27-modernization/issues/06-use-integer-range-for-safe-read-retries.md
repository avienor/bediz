# 06: Use integer range without changing safe-read retry semantics

**What to build:** Apply the Modern Go Guidelines `range_over_int` rule to the HTTP client's retry-attempt loop. Preserve the exact number and numbering of attempts, linear retry wait inputs, context cancellation behavior, retryable HTTP statuses, safe-read eligibility, and the rule that mutations are sent once and return Unknown Outcome when their transport result is inconclusive.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — the syntax change is narrow, but mutation-once and Unknown Outcome behavior have high failure cost and require an independent review.

**Verification gate:** Tests prove the exact call count for successful retries, exhaustion, read-only POST retries, context cancellation during retry wait, mutation HTTP failure, and mutation connection loss. The Retry and Unknown Outcome specification and unsafe-retry ADR remain satisfied, and all project-defined Go checks pass.

**Escalate when:** Any change is needed to retry count, delay calculation, retryable status selection, safe-read classification, context error propagation, mutation submission count, or Unknown Outcome mapping.

**Status:** done

- [x] The zero-based fixed-count retry loop uses integer range with identical attempt values.
- [x] Safe reads retain their configured retry count and mutations remain single-send.
- [x] Context cancellation and Unknown Outcome behavior remain unchanged.
- [x] Focused transport tests and all repository verification commands pass.

## Comments

Implementation (uncommitted diff against `63b0ea4`):

- `internal/httpclient/client.go` (`doJSON`): `for attempt := range attempts` replaces `for attempt := 0; attempt < attempts; attempt++`. `attempts` is still computed before the loop (1 for mutations, `1 + c.retries` for safe reads), so `attempt` takes exactly `0..attempts-1` as before. Both 0-based dependencies are untouched: the retry guards `attempt+1 < attempts` and the linear wait inputs `defaultRetryWait*time.Duration(attempt+1)`. No other loop of this shape exists in the repository.

Tests (module seam — `New`, `Options`, `GetJSON`, `QueryJSON`, `DoJSON`; server hits and emitted RoundTrips counted):

- `TestGetRetriesExhaustConfiguredAttempts` — GET, `Retries: 2`, always-503 server: exactly 3 server calls and a conclusive `*HTTPError` 503. Pins the `1 + retries` budget on the exhaustion path.
- `TestContextCancellationDuringRetryWaitStopsFurtherAttempts` — the retry wait's cancellation branch. The client's `retryWait` hook cancels the caller context at the instant the first retry wait begins and then delegates to the production `wait`, so the wait's `ctx.Done()` branch and the loop's `return err` path both run. Asserts `errors.Is(err, context.Canceled)`, exactly 1 server call and 1 transport attempt, and that the first retry wait input is 100 ms.
- `TestContextCancellationStopsRetryAttempts` — cancellation while the second attempt is in flight: `errors.Is(err, context.Canceled)`, exactly 2 server calls and 2 transport attempts. The transport counter is load-bearing: an escaped third attempt fails inside the transport on the canceled context and never reaches the server, so server calls alone would still read 2.
- `TestMutationIsNeverRetried` now asserts the conclusive `*HTTPError` classification instead of only `err != nil`; the single-send call count is unchanged.

Both cancellation tests are deterministic — no sleeps, no timing windows. `retryWait` is overridden only to place the cancellation instant inside the wait window; the production `wait` still executes. Cancellation inside that window is otherwise indistinguishable at the public seam from cancellation during an in-flight retry (same error, same attempt count), and no public option controls the wait duration.

Mutation evidence (in-place mutations, reverted):

- Loop bound changed to `range c.retries` (drops the initial attempt): `TestGetRetriesTransientStatus` fails with `request attempts exhausted`; `TestGetRetriesExhaustConfiguredAttempts` fails with 2 calls.
- Loop renumbered to `for attempt := 1; attempt <= attempts; attempt++`: `TestContextCancellationDuringRetryWaitStopsFurtherAttempts` fails with `first retry wait = 200ms, want 100ms`, and `TestGetRetriesTransientStatus` fails because `attempt+1 < attempts` then stops one attempt early.
- Retry-wait error ignored (`_ = c.retryWait(...); continue`): `TestContextCancellationStopsRetryAttempts` fails with `server calls = 2, transport attempts = 3`.
- Retry guard reduced to `attempt+1 < attempts` (dropping `ctx.Err() == nil`): not detectable — `wait` returns the context error immediately on a canceled context, so the observable outcome is identical. Left unchanged and deliberately unpinned.

Live verification against the supported local InvokeAI baseline (6.14.1 at `127.0.0.1:9090`): old and new binaries built from the same tree (only `internal/httpclient/client.go` swapped) produced byte-identical exit status, stdout, and stderr in every case:

- Healthy path: `version --json`, `doctor --json`, `models list --json`, `queue list --json`, `images list --json`.
- A local proxy in front of the baseline returning 503 for the first 1, 2, and 5 requests: identical attempt counts (2, 3, 3 per endpoint for `models list`; `queue list` retried both its ID-index GET and its read-only summaries POST), identical envelopes, and identical exit 6 / `invokeai_operation_failed` on exhaustion.
- Connection refused: identical exit 5 / `connection_failed` for `models list`, `queue list`, and `doctor`.

Verification (Go 1.27.1, module `go 1.27.0`, fresh `-count=1` runs):

```text
go test -count=1 ./...        ok (all packages)
go test -race -count=1 ./...  ok (all packages)
go vet ./...                  clean
go mod verify                 all modules verified
```

Independent review (two axes, uncommitted diff):

- Spec axis: no missing requirements and no scope creep. Five of the six gate scenarios were already covered or are covered by the added tests; the one finding was that the cancellation test canceled during the in-flight second attempt rather than inside the retry wait, leaving `wait`'s `ctx.Done()` branch unproven. Addressed by adding `TestContextCancellationDuringRetryWaitStopsFurtherAttempts`. The "linear retry wait inputs" requirement was undefended by any test (informational, no change required for this diff); now pinned by that test's 100 ms assertion. No escalation trigger fired: retry count, delay calculation, retryable statuses, safe-read classification, context error propagation, mutation submission count, and Unknown Outcome mapping are all unchanged.
- Standards axis: no hard violations. `range_over_int` is applied faithfully (zero-based, step 1, bound fixed before the loop), and `errors.AsType`, `t.Context()`, `atomic.Int32`, and the existing `roundTripperFunc` follow the file's conventions; AGENTS.md's "internal call counts outside the test surface" rule is scoped to the CLI seam, and the tests observe behavior at the module boundary rather than inside the loop; the in-flight cancellation test drives only `New`/`Options`/`GetJSON`, while the during-wait test additionally injects the cancellation instant through the unexported `retryWait` hook and still runs the production `wait`. Two P3 judgement calls: a duplicated conclusive-503 assertion block across two tests (kept inline — two sites, matching the file's established inline-assertion style) and `calls`/`attempts` naming in the cancellation test (applied: renamed to `serverCalls`/`roundTrips`, since they count different boundaries).
