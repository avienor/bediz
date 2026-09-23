# 01: Match Request Document members exactly and reject duplicates

**What to build:** Every Request Document (`generate`, `recall`, `models install`, and `upscale`) matches member names exactly and rejects repeated members. A member whose name differs from a documented field only in case (for example `"MODEL"`) is an unknown field. A repeated member (for example `"model"` twice) is `invalid_request`. Both apply at every nesting depth, such as within `components` or `source`, and fail with `invalid_request` (exit status 2) before any network request. A document that is not exactly one JSON value remains `invalid_request`. Valid documents keep their current meaning. The accepted behavior is recorded in `.scratch/strict-request-documents/spec.md`.

**Blocked by:** The `generative-upscale` pull request is merged and this branch is rebased onto `master`, so the `upscale` Request Document is covered.

**Execution route:** `worker + independent review` — the rule is fully specified, the change is bounded and reversible, and CLI-seam tests that count network requests reliably detect an incorrect implementation.

**Verification gate:**
- Public-seam CLI tests for each of `generate`, `recall`, `models install`, and `upscale` prove:
  - a top-level member that differs only in case → exit 2, `invalid_request`, zero network requests;
  - a nested member that differs only in case (where the document has a nested object) → the same;
  - a repeated top-level member → the same;
  - a repeated nested member → the same.
- Every existing Request Document test still passes, including the trailing-value and unknown-field cases.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required: every new rejection happens before network access. State this in the pull request.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer builds the binary and independently runs a case-folded document and a duplicate-member document through all four commands against an unreachable InvokeAI URL, confirming `invalid_request` rather than `connection_failed`.
- The reviewer checks that valid documents for all four commands decode to the same typed operation as before, including numbers, strings with non-ASCII text, empty optional objects, and fields filled by flags.
- Blocking findings: any command still accepting a case-folded or duplicate member; any change in the meaning of a valid document; a rejection that happens after a network request; a changed error code or exit status.

**Escalate when:** The stricter decoding changes how a previously valid document behaves (for example number handling or invalid UTF-8). A command reads its Request Document outside the shared loader. Repeated repair loops fail.

**Permanent records:** V1 spec and tests. §8.1 states that member names match exactly and that duplicate members are validation errors. No new terminology (Request Document already covers this) and no ADR (this enforces the existing §8.1 contract).

**Status:** done

- [x] Case-folded members are rejected before network access for all four commands, at every depth
- [x] Duplicate members are rejected before network access for all four commands, at every depth
- [x] Valid documents and existing tests are unchanged
- [x] V1 §8.1 records the rule
