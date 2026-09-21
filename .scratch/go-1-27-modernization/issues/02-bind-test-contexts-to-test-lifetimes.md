# 02: Bind test request contexts to test lifetimes

**What to build:** Apply the Modern Go Guidelines `testing_t_context` rule throughout the Go test suite so HTTP requests, controller runs, uploads, retries, and readiness checks inherit the lifetime of the test that owns them. Keep explicit cancellation tests explicit by deriving their cancelable context from the test context. Production entry-point context creation remains unchanged.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — this is a broad but mechanical test-only migration whose correctness is strongly observable through the test and race suites.

**Verification gate:** No test-owned operation uses an unscoped background context without a documented reason; the explicit local-interruption case still proves exit status 130 and the `interrupted` structured error. Fresh, uncached `go test ./...` and `go test -race ./...` runs pass, followed by `go vet ./...` and `go mod verify`.

**Escalate when:** A test requires work to survive beyond its test lifetime, lifecycle cancellation changes an asserted Bediz outcome, or the race run exposes a goroutine or server-handler lifetime defect that cannot be repaired within this test-lifecycle slice.

**Status:** done

- [x] Test operations use `t.Context()` and explicitly cancelable tests derive from it.
- [x] Production context roots and signal handling are unchanged.
- [x] Local interruption, safe-read retry, mutation-once, upload, and readiness scenarios retain their existing assertions.
- [x] Fresh non-race and race verification evidence is recorded.

## Comments

Implementation:

- 68 test-owned context expressions migrated to the owning test's `t.Context()`: `internal/cli/cli_external_test.go` (47, including `TestModelsListClassifiesLocalInterruption`), `internal/doctor/doctor_test.go` (11), `internal/httpclient/client_test.go` (6), `internal/models/models_test.go` (3), `internal/images/images_test.go` (1). Those five files are the entire test surface that constructs a context; `capability`, `config`, and `result` tests never used one.
- The explicit local-interruption case now reads `ctx, cancel := context.WithCancel(t.Context())` with the immediate `cancel()` retained, so the CLI still receives an already-canceled context and the derivation is anchored to the test lifetime instead of an unscoped root.
- The now-unused `context` import was dropped from the four files that no longer reference it; `cli_external_test.go` keeps it for `WithCancel`.
- Subtests (`cli_external_test.go` lines 180, 874, 1409 and `doctor_test.go` line 384) call `t.Context()` on the subtest's own shadowing `t`, so the binding follows the subtest lifetime.
- No assertion, exit status, structured error code, or production line changed. `git diff HEAD` contains only context expressions and the four import removals, and `cmd/bediz/main.go` still roots the process context at `signal.NotifyContext(context.Background(), os.Interrupt)`.

Behavioral proof that the binding is real (throwaway test, written, run, deleted — not committed):

- A CLI run against an `httptest` handler that blocks on `r.Context().Done()`, with a `t.Cleanup` that waits for that handler context. With `t.Context()` the request is torn down with the test (pass, 0.00s); with `context.Background()` the request outlived its test and `httptest.Server.Close` blocked on the live connection (fail, 30.02s). Cleanup runs after `cancelCtx()` in `testing.common.runCleanup`, so this observes exactly the test-lifetime cancellation under review.

Scenario evidence (`go test -count=1 -race -v -run ...`):

```text
--- PASS: TestModelsListClassifiesLocalInterruption (0.00s)                       # exit 130 + "interrupted" envelope unchanged
--- PASS: TestImagesUploadLostResponseReturnsUnknownOutcomeWithoutRetry (0.00s)   # mutation-once / unknown outcome
--- PASS: TestGetRetriesTransientStatus (0.30s)                                   # safe-read retry
--- PASS: TestMutationIsNeverRetried (0.00s)                                      # mutation-once
```

Repository-wide evidence (Go 1.27.1, module `go 1.27.0`, fresh `-count=1` runs):

```text
go test -count=1 ./...        ok (all packages)
go test -race -count=1 ./...  ok (all packages)
go vet ./...                  clean
go mod verify                 all modules verified
```

Residual-context audit: `context.Background()` now appears exactly once in the repository — `cmd/bediz/main.go:12`, the production signal root — and no test file contains `context.Background()`, `context.TODO()`, or `context.WithoutCancel`. No test spawns a goroutine or timer, so nothing needs to outlive its test; no escalation clause triggered.

Independent review (two axes, uncommitted diff):

- Spec axis: all four acceptance checkboxes verified against the diff; no missing, partial, or unrequested behavior; the only open item was the evidence recording above.
- Standards axis: no documented-standard violations. The repeated buffers-plus-`app.Run(t.Context(), …)` shape in `cli_external_test.go` is a pre-existing baseline smell that this diff does not expand, and AGENTS.md defers unrelated refactors, so it stays as is.
