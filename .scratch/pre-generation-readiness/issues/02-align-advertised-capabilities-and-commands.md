# 02: Align advertised capabilities and commands

**What to build:** Make the public command surface, Capability Matrix, documentation, and `doctor` agree about what Bediz can currently perform. The Capability Matrix represents implemented and tested controller operations, so Anima generation is not registered or reported as compatible until the third delivery slice implements it. Keep `version` as a supported foundation command and add it to the accepted V1 command surface and user documentation. `doctor` readiness should describe only the implemented first two delivery slices.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — this changes a sensitive public capability and command contract even though the accepted outcome is now explicit.

**Verification gate:** Against the supported local baseline, `doctor --json` does not advertise `generate` before that command exists and still reports accurate readiness for implemented inspection and upload capabilities. `version` is documented, appears in help, and returns the stable human and JSON contracts. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all pass.

**Escalate when:** Existing sources of truth require the Capability Matrix to represent backend readiness rather than implemented controller capability; removing the premature entry would prevent `doctor` from checking a requirement needed by an implemented operation; or documenting `version` would require a schema or exit-status change.

**Status:** ready-for-agent

- [ ] The Capability Matrix and `doctor` advertise only implemented, tested controller operations.
- [ ] Anima generation capability is deferred to its implementation delivery slice.
- [ ] `version` is part of the accepted V1 command surface with public-seam tests.
- [ ] Live baseline evidence and all project-defined Go verification commands pass.
