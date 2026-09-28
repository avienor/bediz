# 07 — Keep `doctor` output small when InvokeAI is unreachable

Status: resolved

## Problem
With no connection, `doctor --json` returns about 10 KB: every required endpoint and invocation is listed as unavailable with its full `missing_properties`. Those properties are not actually missing; they are unknown. This wastes agent context and suggests a schema problem.

## Proposal
When OpenAPI is unavailable, report `required_endpoints` and `required_invocations` as `unknown` without per-property lists, or omit them and leave a single issue. Keep the full detail when OpenAPI loaded but is incompatible.

## Acceptance

When OpenAPI cannot be retrieved or decoded, its report is `{"available":false}` and omits `required_endpoints` and `required_invocations`. Existing request/decode issues, capability failures, structured error codes, and exit statuses remain available. A successfully loaded but incompatible OpenAPI document retains the endpoint and invocation checks and the precise missing-property details.

## Comments

### 2026-09-28 — Confirmed and fixed

Confirmed twice with a locally built binary using `doctor --url http://127.0.0.1:1 --json`. The connection was refused, but the 12,211-byte V1 Result Envelope listed 26 unavailable endpoints, 26 unavailable invocations, and 225 supposedly missing properties. It returned `connection_failed`, exit status 5, and empty stderr.

`doctor.Run` called `inspectOpenAPI` even when the OpenAPI request failed, evaluating an unread document as an incompatible schema. It now inspects only successfully decoded documents. The two check lists are optional JSON fields and are omitted when OpenAPI is unavailable. The V1 spec documents this distinction; the underlying request/decode issues and `openapi_unavailable` capability failures remain intact.

The CLI seam was confirmed with the user. Before the implementation change, `go test -count=1 ./internal/cli -run '^TestDoctorUnreachableOmitsUnknownOpenAPIChecks$'` failed because the unread OpenAPI report contained both requirement lists. It passed after the fix. Further CLI coverage verifies HTTP 401, 404, and 500 responses, malformed JSON, and a partially decoded document. A successfully loaded fixture with a missing upload endpoint and missing Anima `steps` property still reports both checks and the invocation issue's exact `missing_properties`.

The same binary comparison after the fix returned 5,094 bytes (about 58% smaller), zero inferred missing properties, the same `connection_failed` code and exit status 5, and empty stderr. The OpenAPI report contained only `available: false`.

Verification passed:

- `go test -count=1 ./internal/cli -run '^TestDoctorUnreachableOmitsUnknownOpenAPIChecks$'` — red before the fix, green after it.
- `go test -count=1 ./internal/cli ./internal/doctor -run 'TestDoctor|TestRunDoesNotInvent'`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`
- `git diff --check`

Live verification passed:

- `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`.
- `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e -run '^TestLiveGate/doctor_verifies_the_supported_baseline$'` passed. Only the read-only doctor case ran; it verified the ready baseline and retained the complete endpoint and invocation checks.
