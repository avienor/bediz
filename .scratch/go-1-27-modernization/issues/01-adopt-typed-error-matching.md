# 01: Adopt typed error matching without changing V1 failure classification

**What to build:** Apply the Modern Go Guidelines `errors_as_type` rule to Bediz's typed error checks across remote-operation failure handling, readiness diagnostics, queue image hydration, and their tests. Preserve the accepted V1 mapping from error trees to structured error codes and process exit statuses, including the existing authentication, missing-resource, connection, unsupported-capability, invalid-response, interrupted, and unknown-outcome precedence.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — the refactor is bounded and reversible, while an independent reviewer should confirm that the V1 failure precedence remains exact.

**Verification gate:** Focused tests demonstrate direct and wrapped matches for every affected Bediz error type, and the public CLI cases still emit the same result envelopes and exit statuses. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all pass with the Go 1.27 toolchain.

**Escalate when:** Any implementation requires reordering failure precedence, renaming a stable structured error code, changing an exit status, changing the treatment of a missing historical queue image, or supporting a Go toolchain older than the accepted module version. Treat contradictions with the V1 Result and Error Contract or the single-result-envelope ADR as a public-contract decision.

**Status:** done

- [x] Every current type-oriented `errors.As` check in the affected production and test paths uses `errors.AsType` with equivalent error-tree matching.
- [x] Remote-operation failure handling preserves authentication before missing-resource before general HTTP failure classification.
- [x] Readiness diagnostics, queue image hydration, and test assertions preserve their existing observable results for direct and wrapped errors.
- [x] The required focused and repository-wide verification evidence is recorded.

## Comments

Implementation:

- `failRemote` now resolves each typed target with `errors.AsType[T]`; the three `HTTPError` outcomes became one match plus an ordered `switch` (authentication, then not-found, then general HTTP failure) so the accepted precedence is visible in one place.
- `doctor.appendRequestIssue` matches `HTTPError` and `NetworkError` once each, keeping the original case order: authentication rejection, connection failure, general HTTP error, invalid response.
- `queue.Get` output-image hydration keeps skipping a missing historical image via `errors.AsType[*httpclient.HTTPError]` plus the `404` check.
- Test assertions in `httpclient` and `models` use `errors.AsType` directly instead of a temporary target variable.
- No failure precedence, structured error code, exit status, or missing-historical-image behavior changed. No wrapping sites were added or removed, so the wrapped shapes reaching classification are unchanged.

Focused evidence:

- New `TestQueueGetClassifiesWrappedOutputImageFailures` drives `queue get` where the output-image request fails with `401`, a dropped connection, or a non-JSON body. Those errors reach classification wrapped by `queue.Get` (`get output image %q: %w`), so the cases observe `authentication_failed` (exit 5), `connection_failed` (exit 5), and `invalid_invokeai_response` (exit 6) through the public CLI seam.
- Direct matches for every affected type remain covered by the existing CLI and doctor cases: `invalid_request`, `unsupported_capability`, `outcome_unknown`, `connection_failed`, `invalid_invokeai_response`, `not_found`, `invokeai_operation_failed`, `interrupted`, plus the doctor `invokeai_http_error`, `authentication_failed`, and `connection_failed` issues. `InvalidRequestError`, `UnsupportedCapabilityError`, and `OutcomeUnknownError` have no production wrapping site, so no wrapped form of those types can reach classification.
- The new test was checked against a non-chain rewrite: replacing the `errors.AsType` matches in `failRemote` with plain type assertions made all three subtests fail (wrapped errors fell through to `invokeai_operation_failed`); restoring `errors.AsType` made them pass.

Repository-wide evidence (Go 1.27.1, module `go 1.27.0`):

```text
go test ./...        ok (all packages)
go test -race ./...  ok (all packages)
go vet ./...         clean
go mod verify        all modules verified
```

Independent review (two axes, uncommitted diff):

- Spec axis: no missing or partial requirements, no scope creep, no incorrect behavior; precedence, codes, and exit statuses match spec v1 section 9 and ADR-0007; repository-wide search confirms no `errors.As` type check remains. One test-hygiene finding: the new connection-failure handler called `t.Fatalf` from the httptest server goroutine; changed to the file's existing `t.Errorf` plus return pattern.
- Standards axis: no documented-standard violations and no baseline smell requiring action. The duplicated queue-item fixture matches the file's established inline-fixture pattern, so no helper was extracted.
