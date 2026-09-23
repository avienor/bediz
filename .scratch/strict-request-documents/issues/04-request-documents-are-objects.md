# 04: A Request Document is a JSON object

**What to build:** A Request Document whose single JSON value is not an object fails with `invalid_request` (exit status 2) before any network request. Arrays, strings, and numbers already failed to decode. A bare `null` did not: `models list`, `queue list`, and `images list` decoded it as an empty request and ran an unfiltered query. The accepted behavior is recorded in `.scratch/strict-request-documents/spec.md`.

**Blocked by:** 03: Reject null list elements in every Request Document. All three change the shared loader.

**Execution route:** `worker + independent review`: one bounded check in the shared loader, detected by CLI-seam tests that count network requests.

**Verification gate:**
- A public-seam CLI test for `models list`, `queue list`, and `images list` proves that `null`, `[]`, `"request"`, and `1` → exit 2, `invalid_request`, zero network requests, and that `{"schema_version":1}` reaches InvokeAI.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required: the rejection happens before network access.

**Permanent records:** V1 §8.1 states that a request document contains exactly one JSON value and that it is an object.

**Status:** done

- [x] Non-object documents, including `null`, are rejected before network access
- [x] V1 §8.1 records the rule
