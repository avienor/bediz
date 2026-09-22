# 01: Stabilize remote CLI execution

**What to build:** Refactor the implemented remote commands behind a shared CLI execution seam without changing their public behavior. Connection resolution, timeout validation, request-document loading, remote failure mapping, and final JSON or human output should follow one consistent path. `doctor` should reuse the same connection setup while retaining its distinct total diagnostic deadline. Stable public operation names and structured error codes should have authoritative definitions instead of being repeated ad hoc. Cobra must remain only the CLI adapter; domain validation and InvokeAI behavior stay in parser-independent modules.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — this is a bounded, behavior-preserving refactor whose correctness is strongly observable through the existing public CLI tests.

**Verification gate:** Existing public argv, stdout, stderr, exit-status, JSON-envelope, configuration-precedence, and token-redaction behavior remains unchanged. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all pass, and the independent review finds no domain behavior moved into the Cobra adapter.

**Escalate when:** The refactor would change public JSON, exit statuses, connection precedence, timeout semantics, token handling, or any domain request type; or when a shared abstraction cannot represent an implemented command without operation-specific branching that is harder to understand than the current code.

**Status:** done

- [x] All implemented remote commands use one coherent execution/result seam while preserving their external behavior.
- [x] `doctor` shares connection setup but retains a single total diagnostic deadline.
- [x] Public operation names and structured error codes have authoritative definitions with regression coverage.
- [x] All project-defined Go verification commands pass.

## Comments

Implemented as a behavior-preserving refactor.

**Shared execution seam.** `internal/cli/remote.go` now owns the remote command surface: `remoteOptions` and flag capture, `newRemoteClient` connection resolution, `failRemote` failure classification, and `remoteExecution[Request, Result].run`, which is the single sequence every implemented remote command takes (timeout validation, request-document conflict, connection resolution, request-document loading, domain invocation, envelope or human output). `queue list|get`, `images list|get|upload`, and `models list` are declared as `remoteExecution` values, so the duplicated per-command skeletons are gone. `run` is a method on the generic type because Go does not allow a method to declare its own type parameters.

**Doctor.** `doctor` reuses `remoteOptions`, `captureRemoteFlags`, and `newRemoteClient`, and keeps its distinct single deadline through `context.WithTimeout` over the whole diagnostic run. `doctor.Failure` now returns `*result.Error` instead of `(int, string, string)`; the process exit status follows from the code, so the domain module no longer decides exit statuses.

**Authoritative definitions.** `internal/result/contract.go` defines the public operation names, the structured error codes, and the code-to-exit-status mapping, and `fail` derives the exit status from the code instead of call sites passing both. `internal/capability/matrix.go` references the operation-name constants instead of repeating literals. `internal/result/contract_test.go` pins every operation name and error code to its published wire value, pins the exit-status constants, and fails when a mapped code is not pinned.

**Verification evidence.**

- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go mod verify` all pass.
- Differential check against the pre-refactor binary (`git worktree` at `32a9637`, built `cmd/bediz` from both trees): 46 invocations compared byte-for-byte on stdout, stderr, and exit status, covering every remote command in human and JSON mode, request documents from a file and standard input, mixed `--request` plus operation flags, out-of-range limits, unknown and trailing JSON fields, missing arguments, a zero timeout, connection and authentication failures, doctor's not-ready path, config get and set (including conflicting flags), version, unknown commands, and JSON-mode help. All 46 matched.
- Help output for all 15 command and subcommand paths is byte-identical, including flag order.
- A stub InvokeAI server exercised the built binary's human output, `--json` envelopes, `--url` over `BEDIZ_URL` precedence, and token redaction (no secret appeared on stdout or stderr).

**Live baseline check:** no local InvokeAI instance was reachable on `127.0.0.1:9090`, so live verification against the supported 6.14.x baseline was unavailable; the stub-server checks above stand in and are recorded here as the narrowest available substitute.

**Review.** Two-axis review (Standards and Spec) ran as independent sub-agents. Spec axis reported the change correct with no missing requirement and no scope creep. Standards axis raised five judgement calls, all of which were addressed: literal pinning of operation names and error codes (including doctor's codes), an independent exit-status expectation in the new CLI contract test, an accurate test name and comment that no longer claims doctor runs through the seam's `run`, a corrected `failRemote` comment and exit-status map comment that no longer claim an exclusivity or categorisation the code does not have, and an exhaustiveness guard that fails when a mapped code is not pinned by the contract test (mutation-checked).
