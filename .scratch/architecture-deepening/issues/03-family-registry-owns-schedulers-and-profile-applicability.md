# 03: Model Family registry owns scheduler sets and profile applicability

**What to build:** Each generation family and each upscale family records its tested scheduler set once, beside its capability entry. Every scheduler decision reads that one set:

- family resolution of an explicit or profile scheduler;
- Generation Profile applicability for a resolved family;
- profile save-time validation.

At save time a profile has no resolved family, so save-time validation accepts exactly the union of all registered sets. Today that union equals the shared SDXL list, so no profile that was accepted before is rejected, and none that was rejected is accepted.

Generation Profile applicability comes from the family registry that ticket 01 established, instead of rules restated beside it. The rules it must cover are:

- applicable component kinds;
- width and height alignment;
- FLUX.1 schnell guidance;
- the scheduler set.

The shared graph-operation helpers stop owning family scheduler knowledge.

This is a prefactor. No accepted or rejected request or profile, error code, message, or `field` detail changes. Source of truth: `.scratch/architecture-deepening/spec.md` (Modules 1), V1 spec §11.2, §13.1, and §16, and ADR-0011.

**Blocked by:** 01 (Model Family registry owns Generation Mode requirements and alignment).

**Execution route:** `worker + independent review`. The existing public profile, generate, and upscale tests describe the behavior, and a set-equality test proves save-time validation is unchanged.

**Verification gate:**
- Every existing public generate, upscale, and profile test passes with unmodified assertions.
- A new test asserts that the save-time accepted scheduler set equals the union of registered generation and upscale sets, and equals the previous list.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required. Record that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, V1 spec §11.2 and §16, and a fixed base/head diff.
- The reviewer independently confirms each of the following:
  - every family's scheduler set is identical to before;
  - the save-time union is identical to the previous list;
  - profile applicability produces the same `invalid_request` details, with the profile source, name, and field, for each inapplicable field.
- Blocking findings:
  - any changed accept/reject outcome;
  - a scheduler set recorded in more than one place;
  - profile applicability rules restated outside the registry.

**Escalate when:**
- The union of registered sets differs from the previous save-time list.
- A family's scheduler set would need to change to fit the new owner.
- An import cycle prevents profile validation from reading the recorded sets.
- Repeated repair loops fail.

**Permanent records:** None. Terminology, accepted design, and behavior are unchanged, and the new set-equality test joins the existing public tests as evidence.

**Status:** complete

- [x] Each family's tested scheduler set is recorded once, and resolution, profile applicability, and save-time validation read it.
- [x] Save-time validation accepts exactly the union of registered sets, and a test proves it equals the previous list.
- [x] All public tests pass unchanged, and all verification commands pass.

## Comments

### 2026-09-29 — Implementation and verification

- Capability entries record the tested scheduler sets: Anima's 6, FLUX.1's 3, and the shared 31 for SDXL generation and both upscale families. Image-to-image entries inherit their family's set. Resolution, family-specific profile applicability, and local upscale validation read these entries; shared graph-operation helpers no longer contain scheduler knowledge.
- Profile save-time validation reads the union of registered generation and upscale entries through the capability module, avoiding an import cycle with generation and upscale. The new public profile test proves that union equals the previous SDXL list and checks acceptance and rejection in both profile sections. Its initial run failed because capability entries lacked the scheduler field, then passed after adding the recorded sets.
- The existing generation family registry supplies applicable profile component kinds, its existing dimension alignment, FLUX.1 schnell guidance applicability, and the family's scheduler set. Profile validation retains the original error order, messages, and `invalid_request` details with `source`, `profile`, and `field`.
- Added 38 CLI checks covering every inapplicable profile field and error priority across both Generation Modes, including explicit request overrides. These checks pass both on the implementation and on an isolated archive of the starting commit, confirming the unchanged error contract. Existing public test assertions and enqueue fixtures are unchanged.
- Passed `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Narrow profile, generation, upscale, and CLI checks also passed.
- No live InvokeAI check was required or run: this prefactor changes no request InvokeAI receives, as the verification gate specifies. Live E2E was not requested in the environment.
- Independent Standards and Spec reviews of the fixed base/head diff (`a49a821c7ac77a15bf1bb3d04c698d6aeb2cdd28` to `d5f6fcc5876d3428885f63fc3747699c43989195`) both passed with zero findings. The Spec reviewer independently confirmed the unchanged family sets, save-time union, and complete profile error contract.
- Committed on the current `master` branch as `0e696d1` (`Centralize family scheduler sets and profile applicability`). The committed tree exactly matches the reviewed tree. This local ticket remains untracked with the pre-existing `.scratch/` files.
