# 01: Stabilize remote CLI execution

**What to build:** Refactor the implemented remote commands behind a shared CLI execution seam without changing their public behavior. Connection resolution, timeout validation, request-document loading, remote failure mapping, and final JSON or human output should follow one consistent path. `doctor` should reuse the same connection setup while retaining its distinct total diagnostic deadline. Stable public operation names and structured error codes should have authoritative definitions instead of being repeated ad hoc. Cobra must remain only the CLI adapter; domain validation and InvokeAI behavior stay in parser-independent modules.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — this is a bounded, behavior-preserving refactor whose correctness is strongly observable through the existing public CLI tests.

**Verification gate:** Existing public argv, stdout, stderr, exit-status, JSON-envelope, configuration-precedence, and token-redaction behavior remains unchanged. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all pass, and the independent review finds no domain behavior moved into the Cobra adapter.

**Escalate when:** The refactor would change public JSON, exit statuses, connection precedence, timeout semantics, token handling, or any domain request type; or when a shared abstraction cannot represent an implemented command without operation-specific branching that is harder to understand than the current code.

**Status:** ready-for-agent

- [ ] All implemented remote commands use one coherent execution/result seam while preserving their external behavior.
- [ ] `doctor` shares connection setup but retains a single total diagnostic deadline.
- [ ] Public operation names and structured error codes have authoritative definitions with regression coverage.
- [ ] All project-defined Go verification commands pass.
