# 06: Run upscale through the Direct Execution module

**What to build:** Upscale joins the Direct Execution module as its second adapter. The upscale adapter supplies upscale resolution, compilation, the upscale Execution Receipt, and the upscale Recall patch. The module owns the lifecycle, the post-upload failure carrying, profile warnings on failures before an accepted enqueue (not on wait failures, as today), synchronization as a warning, and waiting.

Preserve ticket 04's clarified exception: an upload response with an image name whose image or thumbnail URL cannot be normalized reports the uploaded Source Image without profile preference warnings. The public assertions for this existing behavior remain unchanged.

The following stay with the upscale operation and are unchanged:

- the post-wait scale verification (`scale_not_applied`);
- its receipt shape, including the required `source_image` and `source_uploaded`;
- its source semantics.

The separate synchronization package is removed. Its only purpose was to break the import cycle between Recall and generation, which the module now resolves. Its Anima-only entry point, used only by tests, goes with it, and those tests move to the module's interface with unchanged assertions.

Both operations share one Generation Profile loading and component-preference implementation. Profile load failures keep their current Structured Errors. Existing human-readable warning texts are kept exactly as they are, even where generate and upscale currently word them differently.

This is a prefactor. No command, flag, Request Document member, receipt field, error code, message, exit status, warning, or request InvokeAI receives changes. Source of truth: `.scratch/architecture-deepening/spec.md` (Modules 2), V1 spec §12 and §13, ADR-0009, ADR-0012, and ADR-0017.

**Blocked by:** 05 (Run generate through a Direct Execution module).

**Execution route:** `worker + independent review`. The seam exists after ticket 05. Adding the second adapter is bounded, and the existing upscale public tests fully describe the behavior.

**Verification gate:**
- Every existing public upscale and generate test passes with unmodified assertions, and every golden enqueue fixture is unchanged.
- Module-interface tests cover the upscale adapter: a scale verification failure after an upload carries the uploaded Image Reference, and a Recall failure is a warning.
- No synchronization package remains.
- The CLI upscale command contains no lifecycle ordering.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required. Record that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, V1 spec §13, and a fixed base/head diff.
- The reviewer independently confirms each of the following:
  - the upload still happens after every validation, resolution, and compatibility check, immediately before the single enqueue;
  - `scale_not_applied` details are unchanged;
  - profile warning texts are unchanged;
  - profile warnings still appear on pre-enqueue failures and still do not appear on wait failures;
  - no test assertion changed.
- Blocking findings:
  - any public behavior change;
  - reordered network requests;
  - upscale-specific knowledge in the module;
  - a weakened or deleted assertion.

**Escalate when:**
- Upscale needs a lifecycle step the seam cannot express without operation-specific branching in the module.
- Preserving behavior requires changing a public assertion or a receipt field.
- Repeated repair loops fail.

**Permanent records:** None. Terminology, accepted design, and behavior are unchanged, and the existing public tests and the module-interface tests are the evidence.

**Status:** complete

- [x] Upscale runs through the Direct Execution module as its second adapter.
- [x] The synchronization package is removed, and its tests are migrated with unchanged assertions.
- [x] Profile loading and component preferences share one implementation, with warning texts unchanged.
- [x] All public tests and fixtures are unchanged, and all verification commands pass.

## Comments

- Upscale resolution, compilation, Execution Receipt, Recall patch, and scale verification remain operation-specific. Direct Execution now owns upload, enqueue, warning carrying, synchronization, and waiting for both graph operations. The CLI makes one module call. The separate synchronization package is removed; its assertions remain covered at the module and CLI interfaces.
- Existing public test assertions and golden enqueue fixtures were not changed. A former `upscale.Submit` test now calls `directexecution.Upscale` with `NoWait: true` and keeps its assertions. New module-interface tests cover upload and enqueue ordering, a post-upload `scale_not_applied` failure, pre-enqueue profile warnings, the malformed named upload exception, and Recall failure as a warning.
- Passed `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. No live InvokeAI check was required for this behavior-preserving refactor.
- Independent standards and spec reviews of `bd0efa4...df72665` found no blocking findings. The spec reviewer checked upload order, error details, warning behavior and text, Recall behavior, generic module boundaries, and unchanged existing assertions.
- Committed on the existing `master` branch as `df726653ee19d745aac134099f5d474b9faec974` (`Run upscale through direct execution`). This ticket remains in the pre-existing untracked `.scratch/` tree and was not included in the commit.
