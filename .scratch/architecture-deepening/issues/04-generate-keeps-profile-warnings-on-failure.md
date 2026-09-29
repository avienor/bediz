# 04: Generate keeps profile preference warnings when a later step fails

**What to build:** A `generate` request can use a Generation Profile whose component preference is skipped with `profile_preference_skipped`. When a later step fails before the enqueue is accepted, the failure Result Envelope now still carries those skip warnings, exactly as `upscale` already does. Examples of such a later step are fallback component resolution, the compatibility check, Source Image confirmation or upload, and compilation. Successful behavior is unchanged: the warnings still appear in both the Result Envelope and the Execution Receipt.

This is decision A of `.scratch/architecture-deepening/spec.md`. It is additive under V1 spec §25: a warning appears where there was none. Mirror upscale's tested failure points. Upscale's existing public tests are the pattern. Source of truth: V1 spec §11.4 and §13.2, and ADR-0022.

**Blocked by:** 02 (Classify Structured Errors outside the CLI adapter), so that carrying the warnings on failure lives in the Structured Error module with a table test.

**Execution route:** `worker + independent review`. The behavior is fully specified by upscale's existing contract, and public tests can detect an incorrect implementation.

**Verification gate:**
- New public CLI-seam tests: a generate request with a skipped profile preference, followed by a failing fallback resolution, carries the `profile_preference_skipped` warning with `profile`, `component`, and `reason` in the failure Result Envelope. The same holds for a failure at a later pre-enqueue step.
- Existing generate and upscale tests pass with unmodified assertions.
- V1 spec §11.4 states the behavior in the same terms as §13.2. If upscale's tested failure points are broader than §13.2's wording, clarify both sentences together.
- `CHANGELOG.md` records the change under `Unreleased`.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required, because no request InvokeAI receives changes. Record that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, V1 spec §11.4 and §13.2, and a fixed base/head diff.
- The reviewer independently confirms each of the following:
  - generate's failure points that carry warnings match upscale's;
  - successful receipts are unchanged;
  - the spec and CHANGELOG describe exactly the implemented behavior.
- Blocking findings:
  - warnings on a failure point that upscale does not cover, or missing on one it does;
  - any changed error code or exit status;
  - spec text that disagrees with the tests.

**Escalate when:**
- Upscale's actual failure-point coverage disagrees with §13.2 in a way that is not a clarification.
- Matching upscale would change an existing generate assertion.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec §11.4: the accepted behavior, and §13.2 if its wording needs clarifying.
- `CHANGELOG.md` under `Unreleased`.
- Public tests.

**Status:** completed

- [x] A generate failure after a skipped profile preference and before an accepted enqueue carries the skip warnings, matching upscale and its clarified upload URL exception.
- [x] V1 spec §11.4 and `CHANGELOG.md` record the behavior.
- [x] All verification commands pass, and the absence of a live check is recorded.

## Comments

### 2026-09-29 — Contract clarification

- Inspection found one existing upscale exception: an upload response with an image name but an invalid image or thumbnail URL produces an Uploaded Source Image failure whose outer profile preference warnings are omitted. `TestFailureContext/uploaded_source_extraction_preserves_the_existing_outer_warning_behavior` explicitly preserves this behavior.
- The user asked to follow the recommended option: keep this exception for both operations and document it in V1 spec §11.4 and §13.2. Existing upscale assertions remain unchanged. Public CLI tests now cover the exception for both operations; normal pre-enqueue failures retain warnings, and post-acceptance wait failures continue to omit them.
- No live InvokeAI check is required or planned: this change affects local failure classification and sends no different request to InvokeAI.

### 2026-09-29 — Completed

- Commit: `03269d22562ffdf47369a908804ab130e6e9974a` (`Preserve generation profile warnings on submission failures`) on the existing `master` branch. Its tree matches the independently reviewed snapshot.
- Generate now carries skipped Generation Profile component preferences through fallback resolution, compatibility checks, Source Image validation and upload, compilation, and failed or inconclusive enqueue results. The Structured Error module retains warning order, error classification, and uploaded Source Image details.
- New public CLI tests cover the failure stages, the upload URL exception for both operations, and the absence of profile warnings on post-acceptance wait failures. New Structured Error table tests cover warnings with classified and unclassified causes, wrapped errors, uploaded source details, and empty warning slices.
- Every existing generate and upscale assertion remains unchanged; successful Execution Receipt construction and all requests sent to InvokeAI are unchanged.
- V1 spec §11.4 and §13.2 describe the same failure handling and exceptions. `CHANGELOG.md` records the change under `Unreleased`; the architecture plan and tickets 05–06 carry the clarification forward.
- Passed: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`.
- Independent Standards and Spec reviews used fixed base `0e696d19b2bbad6175c6640dc36b65aa19962034` and immutable head snapshot `ee4103aa383b60a516cb9c9d95009df4733e697b`. Both axes reported zero findings. The reviewed tree is `d3fe00b89eb15eeccc85e93928d4b5d9e12921b2`.
- No live InvokeAI check was required or run because no request sent to InvokeAI changes.
