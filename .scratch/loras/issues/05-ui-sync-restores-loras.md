# 05: Restore the exact LoRA list through UI Synchronization

**What to build:** After Handoff, the InvokeAI web interface shows exactly the LoRAs, and their weights, that Bediz's latest direct execution used, provided no Recall overlapped another. The LoRAs of an earlier execution and the person's own selection are replaced. When Recalls overlap, the stock frontend can keep an earlier execution's LoRA, and this limitation is documented rather than prevented. The feature spec (`.scratch/loras/spec.md`) records every decision.

- **Generation:** After every successful generation enqueue (including `--no-wait`), automatic Recall adds `loras` to the family's existing patch:
  - For a generation with LoRAs, the value is the resolved list in request order as `{"model_name": <display name>, "weight": <resolved weight>}`.
  - For a generation without LoRAs, the value is `[]`.

  This applies to every registered family and to both Generation Modes.
- **Upscale:** After every successful upscale enqueue (including `--no-wait`), the upscale Recall patch adds `loras: []`. The stock Upscale tab applies the web interface's LoRA list, but Bediz upscale uses none. The upscale `not_restored` list is unchanged.
- **Display names:**
  - Bediz sends a LoRA's display name only when that name identifies the single installed `lora` model, across all bases.
  - Otherwise Bediz sends no Recall patch and adds `ui_sync_failed`. This matches the existing main-model collision rule.
  - The generation or upscale outcome, its single enqueue, and its Execution Receipt are unchanged.
  - A failed or inconclusive enqueue sends no Recall.
- **Compatibility:**
  - The `recall` doctor row, generation synchronization, and upscale synchronization additionally require `RecallParameter.loras`. It must be a nullable array whose items reference `LoRARecallParameter` with `model_name` (string) and `weight` (number).
  - When the field is missing or different, the `recall` row fails with `incompatible_recall_schema:loras`. Automatic synchronization then adds `ui_sync_failed`, and `ui_sync.generate` and `ui_sync.upscale` are omitted.
  - Direct Execution compatibility is unchanged.
  - Standalone `recall` neither requires nor accepts `loras`.
- **Overlapping Recalls:**
  - **Why it happens:** The stock 6.14.1 handler clears its LoRA list synchronously but adds each recalled LoRA only after fetching that LoRA's model config. If a later Recall arrives while an earlier Recall's fetch is pending, for example after two quick `--no-wait` runs, the earlier LoRA can be added after the later clear.
  - **Consequence:** The web interface can then keep an earlier execution's LoRA.
  - **Main model:** The main model has the same pre-existing limitation.
  - **No workaround:** Bediz neither delays nor serializes Recalls to work around the browser, and it does not manipulate browser state.
  - **Documentation:** The V1 spec records the limitation next to the guarantee, and the Execution Receipt remains authoritative.
- **Warnings:** The interim `loras` entry that tickets 01–03 add to `ui_sync_partial.not_restored` is removed. Every other `not_restored` list is unchanged.

**Blocked by:**
- 01: Apply LoRAs to SDXL generation.
- 02: Apply LoRAs to Anima generation.
- 03: Apply LoRAs to FLUX.1 generation.

**Execution route:** `frontier-owned`. This slice changes the Recall patch of every existing generation and upscale, clears a person's web-interface LoRA selection, and can be accepted only by live observation of the stock frontend.

**Verification gate:**
- Public-seam tests prove:
  - the exact generation patch with LoRAs and with `[]`, for each family and Generation Mode;
  - the upscale patch with `loras: []`;
  - that a LoRA display-name collision, including one with a LoRA of another base, sends no patch and adds `ui_sync_failed` while the outcome and receipt stay unchanged;
  - that no Recall follows a failed or inconclusive enqueue;
  - the `incompatible_recall_schema:loras` failure for a missing field, for a non-array field, and for items without `model_name` or `weight`, and its effect on `ui_sync`;
  - that standalone `recall` is unchanged;
  - that `not_restored` no longer names `loras`.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline, following the live-verification guide. Use the `browser-harness-local` skill with the Generate and Upscale tabs open before each request, and record the observed LoRA list and weights after model loading:
  - An SDXL generation with two LoRAs at distinct weights. The web interface lists exactly those LoRAs with those weights.
  - A following SDXL generation without LoRAs. The LoRA list is empty.
  - An Anima LoRA generation after an SDXL one, and a FLUX.1 LoRA generation after that, so the base changes each time. Only the new family's LoRAs remain.
  - An upscale after a LoRA generation. The LoRA list is empty.
  - An overlap check:
    - Using the browser harness, hold the page's `GET /api/v2/models/i/{key}` response for a LoRA whose config the page has not fetched yet, for example through CDP request interception.
    - Meanwhile, send a LoRA generation's Recall followed by a LoRA-less generation's Recall, both with `--no-wait`.
    - Release the held response and record the final LoRA list.

    The result is evidence for the documented limitation, not a pass condition. If the stock handler behaves differently from the spec's description, escalate.
  - `doctor --json` reports the `recall` row compatible and `ui_sync` unchanged.
  - The live E2E gate passes.
- Report unavailable live steps and list ad hoc images.

**Review gate:** Not required by this route.

**Escalate when:**
- Without overlapping Recalls, the stock frontend appends recalled LoRAs to its list instead of replacing it, keeps a stale LoRA after `[]`, or loses recalled LoRAs to its own model-change handling.
- The overlap check contradicts the spec's description of the stock handler.
- InvokeAI rejects a Recall `loras` value that Bediz can produce.
- Clearing the person's LoRA selection causes a Handoff problem that the spec does not describe.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec:
  - §10: the `recall` row's `loras` requirement and the `ui_sync` rule.
  - §12: the generation and upscale `loras` patch, the display-name rule, the overlapping-Recall limitation, and the removal of the interim `not_restored` entries.
- `CHANGELOG.md` under Unreleased, recording that Recall now replaces the web interface's LoRA list after every generation and upscale. Tests.
- No ADR or glossary change: ADR-0024 from ticket 01 records the decision.

**Status:** ready-for-agent

- [ ] After every generation and upscale that no other Recall overlaps, the web interface's LoRA list matches the execution exactly, as observed live for all three families.
- [ ] The overlapping-Recall limitation is recorded in the V1 spec, backed by the recorded overlap check.
- [ ] Name collisions and an incompatible Recall schema produce `ui_sync_failed` without changing outcomes or receipts.
- [ ] `doctor`, the V1 spec, and `CHANGELOG.md` reflect the behavior verified live, and every unavailable live step is reported.
