# 03: Reject null list elements in every Request Document

**What to build:** A `null` element inside a list in any Request Document fails with `invalid_request` (exit status 2) before any network request, like an explicit null member value. Today `models list` accepts `{"base_models": [null]}`: the null becomes an empty base filter and the query silently changes. Agents commonly emit `null` for an unknown value, so this reaches InvokeAI unnoticed. The error names the list field. The accepted behavior is recorded in `.scratch/strict-request-documents/spec.md`.

**Blocked by:** 02: Reject explicit null in every Request Document. Both change the shared null check.

**Execution route:** `worker + independent review`: a bounded extension of the null rule, detected by CLI-seam tests that count network requests.

**Verification gate:**
- A public-seam CLI test for `models list` (the only Request Document with a list field) proves that a null as the only element and as a later element → exit 2, `invalid_request`, zero network requests, and that the same list without the null reaches InvokeAI.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required: the rejection happens before network access.

**Permanent records:** V1 §8.1 states that an explicit null member value or list element is a validation error.

**Status:** done

- [x] Null list elements are rejected before network access
- [x] V1 §8.1 records the rule
