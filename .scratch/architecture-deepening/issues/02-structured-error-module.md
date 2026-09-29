# 02: Classify Structured Errors outside the CLI adapter

**What to build:** The mapping from a domain failure to the public Structured Error moves into a parser-independent Structured Error module. The mapping produces the error code, message, details, and warnings. The CLI adapter keeps the Result Envelope and human-readable output writing, and nothing more.

The module takes over all of the following unchanged:

- every mapping the remote command classifier performs today;
- the extra details after an uploaded Source Image: `source_image` and `source_uploaded: true`;
- the extra details after an applied queue cancellation;
- the `outcome_unknown` message specific to model installation;
- the upscale profile preference warnings carried on failure;
- the queue position, wait-stopped, and accepted-item detail shapes;
- the `field` details for invalid requests;
- the profile not-found mapping that the `profiles` commands use.

`doctor` keeps classifying its own diagnostic issues and is out of scope.

This is a prefactor. No error code, message, details member, warning, or exit status changes for any operation. Source of truth: `.scratch/architecture-deepening/spec.md` (Modules 3), V1 spec §9 and §17, ADR-0007, and ADR-0020.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. It is a mechanical move of an existing mapping. The existing CLI-seam tests fully describe the public output, and the new table tests add direct evidence.

**Verification gate:**
- Every existing CLI external test passes with unmodified assertions.
- New table tests at the module's interface cover every failure kind the module maps. Each asserts the code, message, details, and warnings without an HTTP server.
- The CLI adapter no longer inspects domain error types to choose a code.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required, because no request changes. Record that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, V1 spec §9 and §17, and a fixed base/head diff.
- The reviewer independently confirms each of the following:
  - every branch of the old classifier has an equivalent mapping and a table test;
  - branch order is preserved where it decides between overlapping error kinds;
  - no CLI assertion changed.
- Blocking findings:
  - any changed code, message, details member, or exit status;
  - a mapping lost or reordered in a way that changes a result;
  - the module depending on the CLI parser.

**Escalate when:**
- A mapping depends on CLI-only state that cannot be passed as a plain value.
- Preserving behavior requires changing a public assertion.
- Repeated repair loops fail.

**Permanent records:** Tests: the new table tests become contract evidence for the error mapping. Terminology, accepted design, and behavior are otherwise unchanged.

**Status:** completed

- [x] A parser-independent module maps every domain failure to its Structured Error, and the CLI only writes it.
- [x] Table tests cover every mapped failure without HTTP.
- [x] All CLI external tests pass unchanged, and all verification commands pass.

## Comments

### 2026-09-29 — Completed

- Commit: `a49a821` (`Move structured error classification out of the CLI`) on the existing `master` branch.
- Added `internal/structurederror` to own remote failure classification, uploaded Source Image and applied cancellation details, profile preference warnings, Request Document field details, and local profile-storage failure mapping. The CLI retains output writing and exit-status handling; `doctor` is unchanged.
- Direct module-interface table tests cover every mapped failure, complete error and warning values, wrapped errors, overlapping error kinds, and detail merging without an HTTP server. The module reached 100% statement coverage.
- Every existing CLI external test passed with its assertions unchanged. The moved classifier branches and their order match the original implementation.
- Passed: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`.
- Independent Standards and Spec reviews used fixed base `ba07104` and an immutable head snapshot whose tree matches the committed tree. The Standards review's two Go-style fallback corrections were applied; neither axis has a remaining finding.
- No live InvokeAI check was required or run: this prefactor changes no request sent to InvokeAI.
