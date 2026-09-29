# 01: Model Family registry owns Generation Mode requirements and alignment

**What to build:** The generation Model Family registry becomes the single owner of what varies with the family and the Generation Mode. For each registered family (Anima, SDXL, FLUX.1) and each mode (text-to-image, image-to-image), the registry supplies:

- the capability entry that generate evaluates before upload or enqueue:
  - text-to-image checks the entry's invocations, as today;
  - image-to-image checks the entry's endpoints and invocations, as today;
- the dimension alignment: 8 for Anima and SDXL, 16 for FLUX.1. The same value is used for:
  - explicit-dimension validation;
  - source-derived dimension rounding;
  - the too-small Source Image check;
  - standalone Recall validation.

Generate submission no longer branches on a family's base name. It asks the registry. The default Denoising Strength of 0.75 belongs to the image-to-image mode, not to any family.

Tests that exercise family resolution or compilation cross the registry's interface. Test-only exported per-family entry points that bypass the registry are removed. Their tests move to the registry's interface with unchanged assertions.

A new contract test ties compilation to the Capability Matrix. For every registered family and mode, every invocation type in the compiled enqueue graph appears in that family and mode's capability entry.

This is a prefactor. No command, flag, Request Document member, result field, error code, message, exit status, or request InvokeAI receives changes. Source of truth: `.scratch/architecture-deepening/spec.md` (Modules 1), V1 spec §10, §11.1, §11.2, and §11.7, ADR-0011, and ADR-0023.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The change is bounded and reversible, and the existing public generate tests and golden enqueue fixtures fully describe the behavior to preserve.

**Verification gate:**
- Every existing public generate, recall, and doctor test passes with unmodified assertions. This covers text-to-image and image-to-image for all three families.
- Every golden enqueue fixture is byte-for-byte unchanged.
- The new compiled-graph ⊆ capability-entry test passes for all six family and mode combinations.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required, because no request changes. Record that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, the feature spec, and V1 spec §11, and reviews a fixed base/head diff.
- The reviewer independently confirms each of the following:
  - no public test assertion or golden fixture changed;
  - generate submission contains no family-name branching;
  - the alignment for explicit dimensions, source rounding, the too-small check, and Recall comes from one registry value per family;
  - image-to-image still checks endpoints and invocations before any upload, and text-to-image still checks only invocations.
- Blocking findings:
  - any public behavior change;
  - any reordering of network requests;
  - a weakened or deleted assertion;
  - family knowledge left outside the registry in the generate submission path.

**Escalate when:**
- A compiled graph uses an invocation type missing from its capability entry. That is a real gap between the graph and `doctor`, not a test to relax.
- Preserving behavior requires changing a public test assertion.
- The registry cannot own the capability entry choice without an import cycle.
- Repeated repair loops fail.

**Permanent records:** Tests: the new compiled-graph ⊆ capability-entry test is contract evidence. Terminology, accepted design, and behavior are otherwise unchanged.

**Status:** complete

- [x] Generate submission selects the capability entry and alignment through the family registry, with no family-name branching.
- [x] One alignment value per family serves explicit dimensions, source rounding, the too-small check, and Recall validation.
- [x] Every compiled graph's invocation types are covered by its family and mode's capability entry, and a test proves it.
- [x] All verification commands pass, and the absence of a live check is recorded.

## Comments

### 2026-09-29 — Implementation and verification

- The family registry supplies each Generation Mode's capability entry and one alignment per family. Explicit dimensions, Source Image rounding and minimum size, and standalone Recall use that alignment.
- Image-to-image Denoising Strength defaults to 0.75 in a shared mode helper. Generate keeps its existing preflight and network request order: image-to-image checks endpoints and invocations; text-to-image checks only invocations.
- Removed exported per-family resolution and compilation entry points. Existing tests now use `Resolve` and `Compile` through the registry, with every assertion unchanged. All golden fixtures are byte-for-byte unchanged; compiler bodies are unchanged after entry-point and type renames.
- Added a contract test using `Resolve`, `CapabilityEntry`, and `Compile` for all six family and mode combinations, including both FLUX variants, SDXL's VAE override, and image-to-image resize invocations.
- Passed `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Narrow generation and CLI generation, Recall, and doctor checks also passed.
- No live InvokeAI check was required or run: this prefactor changes no request InvokeAI receives, as the verification gate specifies.
- Independent Standards and Spec reviews of the fixed base/head diff (`35d7116ef33f76a9b4aeccff636fae4a5962f0a1` to `5aff79b158aa10db28d49db13a6ff542fba4795b`) both passed with zero findings. The Spec reviewer independently confirmed all four review gates.
- Committed on the current `master` branch as `ba07104` (`Centralize generation mode requirements in the family registry`). The committed tree exactly matches the reviewed tree. This local ticket remains untracked with the pre-existing `.scratch/` files.
