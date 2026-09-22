# 03: Report verified partial generation UI Synchronization

**What to build:** Advertise the tested Anima Parameter Recall and `partial` Generation UI Synchronization capability for stock InvokeAI 6.14.x only after the working Handoff is verified. Make `doctor` distinguish Direct Execution readiness from Recall readiness, report missing Recall requirements precisely, and avoid claiming `full` or `--replace` support. Update user-facing capability descriptions to match. Governing sources: V1 §§ 5, 10, 12, 22–24; ADR-0003, ADR-0015, ADR-0017; and the approved feature spec.

**Blocked by:** 01: Submit an Anima Parameter Recall patch; 02: Synchronize Anima generation after enqueue.

**Execution route:** `worker + independent review` — the supported level and deferred behavior are decided, and capability fixtures plus an independent browser check can detect an incorrect advertisement.

**Verification gate:** Supported-version fixtures show `ui_sync.generate` as `partial` only when Recall requirements are present and tested. Missing endpoint/schema, unsupported versions, and absent UI Synchronization implementation never produce a `full` claim or make Direct Execution falsely incompatible. User documentation and `doctor` agree. A live 6.14.1 browser check compares recalled controls with the complete Execution Receipt and records the controls that remain unchanged. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** A separate reviewer examines the fixed base/head diff for spec fidelity, capability-matrix accuracy, public JSON compatibility, and repository standards; independently validates a negative Recall fixture and the live browser observation. A false `full` claim, missing compatibility failure, incorrectly disabled Direct Execution, or inconsistent docs blocks acceptance. After fixes, rerun affected checks and review the changed diff; keep the issue open awaiting independent review until accepted.

**Escalate when:** `doctor` and live UI disagree, the baseline version's Recall requirements differ from tested fixtures, a new synchronization level or public JSON field is needed, a claimed UI control cannot be observed, documentation overstates the Handoff guarantee, or repeated repairs fail for the same cause.

**Permanent records:** Tests, Capability Matrix, and user-facing capability documentation — V1 §10–12 and ADR-0017 already state the accepted `partial` level; implementation must supply fixture and live evidence. Amend V1 and supersede ADR-0017 if live behavior changes the accepted contract. `CONTEXT.md` terminology is unchanged.

**Status:** completed

- [x] `doctor` and the Capability Matrix advertise only verified `partial` generation synchronization, independently of Direct Execution readiness.
- [x] Negative fixtures report missing Recall requirements precisely and never claim full restoration or a working `--replace` option.
- [x] User-facing documentation, live browser evidence, and the complete Execution Receipt describe the same Handoff guarantee.
- [x] The verification gate passes and independent review has no acceptance-blocking finding.

## Comments

### Implementation and live evidence (2026-09-22)

The versioned doctor fixture now includes the 6.14.1 Recall endpoint, request schema reference, and seven tested patch fields. Negative endpoint, request-body, missing-field, and wrong-type fixtures keep `generate` compatible while `recall` reports the exact failure and `ui_sync.generate` is absent. The 6.15.0 fixture reports neither generation nor Recall readiness. No `full` or `--replace` claim is made.

Against local stock InvokeAI 6.14.1, `bediz doctor --json` reported both `generate/anima` and `recall` compatible with `ui_sync.generate: partial`. Before manual Recall, the browser showed the lighthouse prompt, `watermark`, 512×512, 5 steps, seed 722639, Scheduler Euler, and CFG Scale 7.5. A manual Recall patch changed prompts, dimensions to 640×512, steps to 6, and seed to 314159 in the browser; queue total stayed 26, so no job was enqueued.

A one-output Anima generation then returned a complete Execution Receipt for queue item 27 and image `f8b8da84-6347-4ef3-a5a0-f773d63cf1fd.png`. Its resolved settings were 512×512, 5 steps, seed 271828, Scheduler `heun`, guidance 4.5, exact main/VAE/Qwen3 keys, and output count 1. The `ui_sync_partial` warning listed scheduler, guidance, VAE, Qwen3 encoder, output count, and board ID as not restored. The browser displayed the new prompts, 512×512, 5 steps, and seed 271828; Scheduler remained Euler and CFG Scale remained 7.5. The result image appeared in the browser, and `queue get 27` reported the completed item and matching Image Reference. VAE, encoder, count, and board controls had no differing before/after values in this run, so their lack of restoration is supported by the accepted 6.14.1 frontend behavior and warning, not by a differing-value browser observation.

The test-created image was deleted after observation, and a final Recall patch restored the original browser prompts, dimensions, steps, and seed. The live E2E gate separately passed and self-cleaned its upload and generation images. Verification commands: `go test ./...`, `go test -race ./...`, `go vet ./...`, `go mod verify`, and `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e`.

### Independent review

Review of `a901ca1...9977abc` found no documented-standard breach or spec acceptance blocker. The spec reviewer independently ran the missing-`seed` negative fixture and observed a reversible Recall patch in a separate browser tab: prompts, width, steps, and seed changed; Euler and CFG 7.5 did not. The original controls were restored and verified. With only one Anima main model installed, model selection was visible but could not be differentially tested. The standards reviewer noted a non-blocking risk that the doctor and Recall schema checks could drift despite their shared field requirements.
