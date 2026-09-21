# 07: Stream prerelease identifier validation

**What to build:** Apply the Modern Go Guidelines `strings_split_seq` rule to InvokeAI semantic-version prerelease validation. Supported stable releases, rejected prereleases, build metadata, numeric-leading-zero rejection, and malformed-version errors must remain byte-for-byte equivalent at the public capability boundary.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — the parser change is tiny and deterministic, with a compact exhaustive test table suitable for independent verification.

**Verification gate:** The version table covers the supported range boundaries, stable build metadata, ordinary prereleases, numeric prerelease identifiers with a leading zero, empty or malformed identifiers, and unsupported minor/major versions. All project-defined Go checks pass.

**Escalate when:** Streaming split semantics change acceptance of any existing fixture, the supported InvokeAI range must change, or fixing a discovered version edge case would alter the capability matrix or compatibility policy.

**Status:** done

- [x] Prerelease components are iterated with the sequence API and no temporary split slice is allocated.
- [x] Every existing accepted and rejected version retains its result and error behavior.
- [x] Boundary and malformed-version coverage is explicit.
- [x] Focused capability tests and all repository verification commands pass.

## Comments

Implementation (uncommitted diff against `c6a853c`):

- `internal/capability/matrix.go` (`parseVersion`): `for identifier := range strings.SplitSeq(...)` replaces `for _, identifier := range strings.Split(...)`. The prerelease substring still comes from the same regex capture; identifiers are yielded lazily instead of through an allocated slice, and the early `return` on a numeric identifier with a leading zero stops iteration exactly as the slice loop did. No other `strings.Split` or `bytes.Split` callsite remains in the repository.

Tests (seam: the exported `capability.SupportsInvokeAI`; all 11 pre-existing rows kept verbatim, 16 rows added, grouped by boundary):

- Supported range and stable build metadata: `6.14.9`, `6.14.1+build.1.2`.
- Unsupported minor and major versions, including the exclusive upper bound: `6.13.9`, `6.15.0`, `7.0.0`.
- Ordinary prereleases (well-formed, unsupported): `6.14.1-rc.1`, `6.14.1-0`, `6.14.1-0a`, `6.14.1-alpha-2`. `6.14.1-0` pins the `len > 1` guard and `6.14.1-0a` pins `isNumericIdentifier`.
- Empty and malformed versions: `""`, `v6.14.1`, `6.14`, `6.14.1.2`, `6.014.1`, `6.14.1-`, `6.14.1-alpha.`, `6.14.1-alpha..1`, `6.14.1-rc.01`, `6.14.1+`. `6.14.1-rc.01` is the row that guards iteration past the first identifier.

Equivalence evidence (throwaway in-package probe, deleted after use): the probe printed `supports` plus the exact error text for 53 versions — the table rows plus adversarial extras (leading/trailing separators, `6.14.1-00`, `6.14.1-rc.1.01`, `6.14.1+build..1`, `6.14.1-rc+meta`, `6.14.1+build-1`, whitespace, non-ASCII identifiers, integer overflow in major/minor/patch). Output before and after the change `diff`ed empty, so outcomes are identical byte-for-byte, including the `invalid InvokeAI version %q` message that `doctor` surfaces as `invalid_invokeai_version`.

Streaming-split semantics: a direct comparison of `strings.Split` and `strings.SplitSeq` over 16 inputs (empty input, leading/trailing/doubled separators, 64-part repeats) found zero differences. The sensitive path is the absent prerelease: `SplitSeq("", ".")` still yields one empty identifier and `len("") > 1` is false, so stable versions keep their `true` result.

Mutation evidence (applied to a throwaway copy of the tree, reverted afterwards):

- Numeric guard dropped (`isNumericIdentifier` removed) → `6.14.1-0a` fails.
- Iteration limited to the first identifier → `6.14.1-01` and `6.14.1-rc.01` fail.
- Separator changed from `.` to `-` → `6.14.1-rc.01` fails.
- Leading-zero rejection neutered → `6.14.1-01` and `6.14.1-rc.01` fail.
- Empty identifier treated as malformed → every stable row fails.

Live verification against the supported local InvokeAI baseline (6.14.1 at `127.0.0.1:9090`): pre-change and post-change binaries built from the same tree produced byte-identical exit status, stdout, and stderr for `doctor --json`, `doctor`, `models list --json`, and `images list --json` (all exit 0; `doctor` reports version 6.14.1, supported, ready, zero issues). A stub version endpoint additionally drove the changed error path through the real CLI: `6.14.1-01` → exit 6 with `invalid_invokeai_version` and message `invalid InvokeAI version "6.14.1-01"`; `6.14.1-rc.1` → parsed as a well-formed prerelease with `supported_version: false`.

Verification (Go 1.27.1, module `go 1.27.0`, fresh `-count=1` runs):

```text
go test -count=1 ./...        ok (all packages)
go test -race -count=1 ./...  ok (all packages)
go vet ./...                  clean
go mod verify                 all modules verified
```

Independent review (two axes, uncommitted diff):

- Spec axis: no missing requirements, no scope creep, no wrong expectations. All 11 pre-existing rows survive with identical expectations; all 16 new rows were hand-traced against `versionPattern`, `parseVersion`, and `SupportsInvokeAI`; boundaries match `>= 6.14.1, < 6.15.0` in `matrix.go`, `docs/spec/v1.md`, and ADR-0015.
- Standards axis: no hard violations. `strings_split_seq` is applied completely (pure iteration, no retained slice, Go 1.24 prerequisite satisfied by `go 1.27.0`), tests stay at the public `SupportsInvokeAI` seam with `parseVersion` private, and wording follows `CONTEXT.md`. One P3 judgement call accepted without change: the `""` row makes Go generate the subtest name `#00`, because the table derives subtest names from the version; assertions and failure messages still quote the version via `%q`, and adding a `name` column would deviate from the table's existing shape.

No escalation trigger fired: streaming split semantics changed no fixture, the supported range and capability matrix are untouched, and no version edge case needed a behavior fix.
