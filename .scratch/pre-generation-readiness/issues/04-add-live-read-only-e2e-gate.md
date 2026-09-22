# 04: Add a live read-only E2E gate

**What to build:** Add an opt-in integration harness that builds and invokes the real Bediz binary against a caller-supplied local InvokeAI URL. The gate verifies the supported baseline version and exercises `doctor`, model listing, image listing, and queue listing without mutating remote state or depending on pre-existing models, images, or queue items beyond the readiness conditions each command documents. It validates the actual process boundary: exit status, exactly one JSON result envelope on stdout, diagnostic discipline on stderr, normalized results, bounded list output, and absence of secrets. Deterministic `images get` coverage belongs to the upload-fixture ticket, and deterministic `queue get` coverage is deferred until the generation slice creates a known queue item; the harness must not select arbitrary user resources to make those checks pass.

**Blocked by:** 02: Align advertised capabilities and commands; 03: Define bounded queue inspection.

**Execution route:** `worker + independent review` — the harness is read-only, environment-gated, and its success criteria are machine-verifiable.

**Verification gate:** With `BEDIZ_E2E_URL` targeting the supported local baseline, the harness builds the binary and all named smoke cases pass with validated envelopes and process exit statuses. Without the opt-in setting, ordinary unit verification remains deterministic and the output clearly distinguishes “not requested” from “verified.” A version mismatch or unavailable service cannot be reported as a successful live verification. All project-defined Go verification commands pass independently of the live gate.

**Escalate when:** The live target requires authentication that cannot be supplied without exposing a token; a read-only command changes remote state; the baseline version or OpenAPI contract differs from the accepted support range; or a test would need to depend on arbitrary existing user data.

**Status:** done

- [x] The opt-in harness builds and tests the real binary at the process boundary.
- [x] Supported-version, JSON-envelope, exit-status, stderr, normalization, bounds, and secret-redaction assertions are observable.
- [x] The read-only gate is deterministic and never claims verification when it was skipped or could not reach the baseline.
- [x] Ordinary and live verification instructions are documented and all project-defined Go checks pass.

## Comments

Added an opt-in Go E2E package gated by `BEDIZ_E2E_URL`. With the variable
unset, `go test -count=1 -v ./e2e` makes no network request and reports
`live read-only E2E: NOT REQUESTED`. With a target supplied, the harness builds
`./cmd/bediz` into a temporary directory and invokes the resulting binary for
`doctor`, `models list`, `images list --limit 1`, and
`queue list --limit 1`.

Every smoke case requires exit status zero, exactly one successful V1 result
envelope on stdout, and empty stderr. Independently declared public result
shapes reject backend-only or otherwise unknown fields. The doctor case
requires the supported 6.14.1 baseline, the accepted support range, and the
exact compatible implemented-capability surface. Image and queue pages are
limited to one and checked against the returned page metadata. A non-secret
token sentinel exercises the configured-token path and is rejected if it
appears in either output stream.

The cases accept empty collections and never select an arbitrary model, image,
or queue item. The gate is fail-fast after doctor, so an unavailable or
unsupported target cannot reach the `VERIFIED` status. A direct run against an
unavailable loopback port exited nonzero and did not print `VERIFIED`. A ready
synthetic target reporting supported-range version 6.14.2 was also rejected
because it was not the pinned 6.14.1 integration baseline.

The README documents both ordinary and live commands, including `-count=1` to
prevent a live result from coming from the Go test cache. The live gate passed
against the local InvokeAI 6.14.1 baseline at `http://127.0.0.1:9090`, with all
four smoke cases and the final `VERIFIED` status. `go test ./...`,
`go test -race ./...`, `go vet ./...`, and `go mod verify` all pass.
