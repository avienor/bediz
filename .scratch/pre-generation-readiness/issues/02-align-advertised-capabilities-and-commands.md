# 02: Align advertised capabilities and commands

**What to build:** Make the public command surface, Capability Matrix, documentation, and `doctor` agree about what Bediz can currently perform. The Capability Matrix represents implemented and tested controller operations, so Anima generation is not registered or reported as compatible until the third delivery slice implements it. Keep `version` as a supported foundation command and add it to the accepted V1 command surface and user documentation. `doctor` readiness should describe only the implemented first two delivery slices.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — this changes a sensitive public capability and command contract even though the accepted outcome is now explicit.

**Verification gate:** Against the supported local baseline, `doctor --json` does not advertise `generate` before that command exists and still reports accurate readiness for implemented inspection and upload capabilities. `version` is documented, appears in help, and returns the stable human and JSON contracts. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all pass.

**Escalate when:** Existing sources of truth require the Capability Matrix to represent backend readiness rather than implemented controller capability; removing the premature entry would prevent `doctor` from checking a requirement needed by an implemented operation; or documenting `version` would require a schema or exit-status change.

**Status:** done

- [x] The Capability Matrix and `doctor` advertise only implemented, tested controller operations.
- [x] Anima generation capability is deferred to its implementation delivery slice.
- [x] `version` is part of the accepted V1 command surface with public-seam tests.
- [x] Live baseline evidence and all project-defined Go verification commands pass.

## Comments

Implemented the advertised-surface alignment.

**Capability Matrix and doctor.** Removed the premature Anima `generate` entry and its generation endpoints, invocation schemas, component-model requirements, and UI synchronization claim. The registered matrix now contains exactly `models.list`, `images.list`, `images.get`, `images.upload`, `queue.list`, and `queue.get`. Public CLI coverage exercises `doctor --json` and pins that exact compatible operation list while requiring generation-specific readiness fields to remain empty. Doctor readiness now also requires a diagnostic run with no issues, so a failed implemented probe such as model inspection cannot produce a report whose embedded `ready` value or human status says ready.

**Version command and documentation.** Added `version` to the accepted V1 command surface and documented both human and JSON invocations in the README. Public CLI tests pin its root-help entry, exact human output, and V1 JSON envelope including operation, version metadata, schema version, and empty warnings.

**Verification evidence.** `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all pass. The focused capability, doctor, and CLI suites also pass.

**Live baseline check.** A local InvokeAI 6.14.1 baseline was available at `http://127.0.0.1:9090`. Running the real CLI entry point with `doctor --url http://127.0.0.1:9090 --json` returned exit status 0 and one successful V1 result envelope with `ready: true`. All six implemented capabilities were compatible; `generate` was absent; `required_invocations`, component-model `requirements`, relevant component models, and `ui_sync` were empty.

**Review.** Independent Standards and Spec reviews found no scope creep or code smells. They identified the stale ticket state and a readiness inconsistency after generation requirements were removed. This record now contains the completion evidence, and doctor readiness now becomes false whenever a diagnostic issue is present; a regression test covers model-inspection failure.
