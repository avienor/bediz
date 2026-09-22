# 04: Add a live read-only E2E gate

**What to build:** Add an opt-in integration harness that builds and invokes the real Bediz binary against a caller-supplied local InvokeAI URL. The gate verifies the supported baseline version and exercises `doctor`, model listing, image listing, and queue listing without mutating remote state or depending on pre-existing models, images, or queue items beyond the readiness conditions each command documents. It validates the actual process boundary: exit status, exactly one JSON result envelope on stdout, diagnostic discipline on stderr, normalized results, bounded list output, and absence of secrets. Deterministic `images get` coverage belongs to the upload-fixture ticket, and deterministic `queue get` coverage is deferred until the generation slice creates a known queue item; the harness must not select arbitrary user resources to make those checks pass.

**Blocked by:** 02: Align advertised capabilities and commands; 03: Define bounded queue inspection.

**Execution route:** `worker + independent review` — the harness is read-only, environment-gated, and its success criteria are machine-verifiable.

**Verification gate:** With `BEDIZ_E2E_URL` targeting the supported local baseline, the harness builds the binary and all named smoke cases pass with validated envelopes and process exit statuses. Without the opt-in setting, ordinary unit verification remains deterministic and the output clearly distinguishes “not requested” from “verified.” A version mismatch or unavailable service cannot be reported as a successful live verification. All project-defined Go verification commands pass independently of the live gate.

**Escalate when:** The live target requires authentication that cannot be supplied without exposing a token; a read-only command changes remote state; the baseline version or OpenAPI contract differs from the accepted support range; or a test would need to depend on arbitrary existing user data.

**Status:** ready-for-agent

- [ ] The opt-in harness builds and tests the real binary at the process boundary.
- [ ] Supported-version, JSON-envelope, exit-status, stderr, normalization, bounds, and secret-redaction assertions are observable.
- [ ] The read-only gate is deterministic and never claims verification when it was skipped or could not reach the baseline.
- [ ] Ordinary and live verification instructions are documented and all project-defined Go checks pass.
