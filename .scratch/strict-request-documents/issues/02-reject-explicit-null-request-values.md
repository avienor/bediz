# 02: Reject explicit null in every Request Document

**What to build:** An explicit `null` member value in any Request Document (`generate`, `recall`, `models install`, and `upscale`), at any nesting depth, fails with `invalid_request` (exit status 2) before any network request. Today `generate`, `recall`, and `models install` treat a null as an absent field, so it silently acquires a default: for example `"seed": null` produces a random seed. `upscale` already rejects nulls (V1 §13); its behavior is unchanged, and its rule becomes the general Request Document rule. An optional field is expressed by omitting it. The accepted behavior is recorded in `.scratch/strict-request-documents/spec.md`.

**Blocked by:** 01: Match Request Document members exactly and reject duplicates. Both change the shared Request Document loader.

**Execution route:** `worker + independent review` — the product decision to reject nulls for every command is resolved, and CLI-seam tests that count network requests reliably detect an incorrect implementation.

**Verification gate:**
- Public-seam CLI tests for `generate`, `recall`, and `models install` prove:
  - a top-level null (for example `seed`, `model`, or `move`) → exit 2, `invalid_request`, zero network requests;
  - a nested null (for example `components.vae` or `source.file_id`) → the same;
  - a document with several nulls names the same field on every run.
- The existing `upscale` null tests still pass unchanged.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required: every new rejection happens before network access. State this in the pull request.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer independently checks every Request Document field of the four commands and confirms that no field gives `null` a documented meaning.
- The reviewer confirms that the V1 spec text, the tests, and the four commands agree, and that the `upscale` rule in §13 now refers to the general rule without changing it.
- Blocking findings: any command accepting a null at any depth; an error that names a different field between runs of the same document; a rejection after a network request; a changed error code or exit status; spec text that disagrees with the tests.

**Escalate when:** A Request Document field needs `null` as a meaningful value. Rejecting nulls changes the behavior of an operation flag. Repeated repair loops fail.

**Permanent records:** V1 spec and tests. §8.1 states that explicit null member values are validation errors in every Request Document, and §13 refers to that rule instead of restating it for `upscale`. No new terminology and no ADR (this extends the existing strict request contract to nulls).

**Status:** ready-for-agent

- [ ] Explicit nulls are rejected before network access for `generate`, `recall`, and `models install`, at every depth
- [ ] A document with several nulls reports the same field on every run
- [ ] `upscale` null behavior is unchanged
- [ ] V1 §8.1 records the general rule and §13 refers to it
