# 05: Advertise and live-verify Anima Direct Execution

**What to build:** Register the completed `generate` operation for the Anima Model Family in the Capability Matrix with its supported InvokeAI range, enqueue and inspection endpoints, required invocation types and fields, and required installed main/component models. Make `doctor` report Anima Direct Execution only when the entire compatibility check passes, while leaving UI Synchronization unadvertised until its separate delivery slice. Update user-facing capability documentation so generation is no longer described as deferred, and add the live end-to-end check that proves a canonical Generation Request produces a completed Execution Receipt on the supported baseline.

**Blocked by:** 02: Poll one Anima execution to a completed Execution Receipt; 03: Resolve Anima settings and components deterministically; 04: Generate a Resolved Seed Set to an explicit Output Board.

**Execution route:** `worker + independent review` — the complete behavior is fixed, compatibility fixtures and the live baseline can detect false-positive or false-negative capability advertising, and documentation is a derived description of tested behavior.

**Verification gate:** Capability and doctor tests prove the operation is compatible only inside the supported version range and only when every required endpoint, invocation field, Anima main model, Anima VAE, and Qwen3 encoder is present. Negative fixtures each remove one requirement and must suppress readiness with a precise issue. The live InvokeAI 6.14.1 test must run a small meaningful generation through the public CLI, wait for completion, validate the exact receipt and accessible Image Reference, and keep JSON stdout parseable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` must all pass.

**Escalate when:** Capability Matrix and live readiness disagree; InvokeAI 6.14 patch releases require different graph topology or fields; live success depends on an unregistered model/component combination; advertising generation would imply Recall/UI Synchronization is already implemented; documentation would overstate support; or two repair attempts fail for the same underlying reason.

**Permanent records:** Tests — the V1 spec, accepted ADRs, and glossary already define the behavior and terminology. Capability Matrix and user-facing capability documentation are updated as derived records; no new ADR is expected unless live evidence invalidates an accepted decision.

**Status:** completed

- [x] The Capability Matrix registers `generate` for Anima with the supported version range, exact endpoints, invocation fields, and model requirements exercised by the implementation.
- [x] `doctor` reports Anima generation ready only when all registered requirements pass and otherwise reports precise failures.
- [x] `doctor` does not claim Generation UI Synchronization before delivery step 4.
- [x] User-facing capability documentation advertises the verified Anima path and no longer says all generation is deferred.
- [x] The live end-to-end test runs a canonical request, observes queue completion, validates the Execution Receipt and Image Reference, and does not depend on transient browser state.
- [x] `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass, with the live baseline result reported separately when the environment is unavailable.
