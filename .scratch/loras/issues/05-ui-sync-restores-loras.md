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

**Status:** implemented

- [x] After every generation and upscale that no other Recall overlaps, the web interface's LoRA list matches the execution exactly, as observed live for all three families.
- [x] The overlapping-Recall limitation is recorded in the V1 spec, backed by the recorded overlap check.
- [x] Name collisions and an incompatible Recall schema produce `ui_sync_failed` without changing outcomes or receipts.
- [x] `doctor`, the V1 spec, and `CHANGELOG.md` reflect the behavior verified live, and every unavailable live step is reported.

## Comments

### 2026-09-29 implementation and verification

Automatic generation and upscale synchronization now shares a Recall operation that sends the exact resolved LoRA list, including an empty array. LoRA names are checked against every installed `lora`, across bases. The additional schema requirement gates automatic synchronization and the `doctor` Recall row, while standalone Recall and Direct Execution keep their contracts. Tests at the agreed CLI and Compatibility Check seams cover the exact patches, both modes and FLUX variants, waiting and `--no-wait`, cross-base collisions, failed/inconclusive enqueue, schema defects, and standalone Recall. The V1 spec §10/§12 and Unreleased changelog record the delivered behavior.

Verification commands passed:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`

Live baseline: `curl -fsS http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`. Built the current source with `go build -o /tmp/bediz-lora-sync ./cmd/bediz`. `/tmp/bediz-lora-sync models list --json` confirmed all required models installed; no model installation was needed. `/tmp/bediz-lora-sync doctor --json` reported `ready: true`, compatible `recall` with no failures, and `ui_sync: {generate: partial, upscale: partial}`.

The browser-harness-local selected work tab and a second verification tab observed the Generate and Upscale controls. Model and LoRA observations came from rendered controls after main-model loading, including each LoRA card's enabled state and both weight inputs. The following live commands succeeded (the binary prefix is `/tmp/bediz-lora-sync`):

| Command | Queue item | Observation / outcome |
| --- | --- | --- |
| `generate --model Juggernaut-XL-v9 --prompt 'alienzkin noodlez, a small red teacup' --width 768 --height 768 --steps 2 --seed 605 --lora alienzkin-sdxl=1 --lora noodlez-sdxl=1.5 --json` | 193 | Completed. Generate showed exactly the two LoRAs, with weights 1 and 1.5. |
| `generate --model Juggernaut-XL-v9 --prompt 'a small red teacup' --width 768 --height 768 --steps 2 --seed 606 --no-wait --json` | 194 | Accepted, later completed. Both open tabs showed an empty LoRA list. |
| `generate --model 'Anima Base 1.0' --vae 'Anima QwenImage VAE' --qwen3-encoder 'Anima Qwen3 0.6B Text Encoder' --prompt 'a small red teacup' --width 768 --height 768 --steps 2 --seed 607 --lora Anima_Detail_Tweaker=1.25 --json` | 195 | Completed. Main model changed from SDXL to Anima; both tabs showed only `Anima_Detail_Tweaker`, enabled, weight 1.25. |
| `generate --model flux_schnell_flux1-schnell-bnb_nf4 --prompt 'a small red teacup' --width 768 --height 768 --steps 2 --seed 608 --lora lora=1.25 --json` | 196 | Completed. Main model changed from Anima to FLUX.1; both tabs showed only `lora`, enabled, weight 1.25. |
| `generate --model Juggernaut-XL-v9 --prompt 'alienzkin noodlez, a small red teacup' --width 768 --height 768 --steps 2 --seed 609 --lora alienzkin-sdxl=1 --lora noodlez-sdxl=1.5 --no-wait --json` | 197 | Accepted, later completed. Repeated the two-LoRA check with both tabs open: exactly `alienzkin-sdxl` at 1 and `noodlez-sdxl` at 1.5, both enabled. |
| `upscale --image e6c7b44d-1a8f-4230-b1d3-8336e7830c67.png --model Juggernaut-XL-v9 --upscale-model RealESRGAN_x4plus.pth --tile-controlnet controlnet-tile-sdxl-1.0 --scale 2 --tile-size 512 --steps 1 --seed 610 --no-wait --json` | 198 | Accepted, later completed at 1536×1536. The main base remained SDXL; both tabs' LoRA lists became empty. |
| `generate --model Juggernaut-XL-v9 --prompt 'alienzkin, a small red teacup' --width 768 --height 768 --steps 1 --seed 611 --lora alienzkin-sdxl=1.75 --no-wait --json` | 199 | Accepted, later completed. Initial overlap setup encountered a cached model config, so no response was held; used the cold setup below instead. |
| `generate --model Juggernaut-XL-v9 --prompt 'alienzkin, a small red teacup' --width 768 --height 768 --steps 1 --seed 612 --lora alienzkin-sdxl=1.75 --no-wait --json` | 200 | Accepted, later completed. Cold config response held for the overlap check below. |
| `generate --model Juggernaut-XL-v9 --prompt 'a small red teacup' --width 768 --height 768 --steps 1 --seed 613 --no-wait --json` | 201 | Accepted, later completed. Arrived while item 200's Recall model response was still held. |
| `queue wait 194 197 198 199 200 201 --json` | above | Every item completed. The upscale has one final image and two intermediate images. |

Overlap evidence: the stock model-list query hydrates all per-key model-config queries, so merely reloading a warm page did not cause a new GET for the LoRA. Using browser-harness CDP `Fetch.enable` at response stage, the Generate page's initial `GET /api/v2/models/` response was held during reload to keep the per-key cache cold. The Upscale tab stayed open. `GET /api/v2/models/i/5a64f0b6-e279-4d4e-8c02-826bc2470066` was then held with HTTP status 200 while the first Recall (item 200) was pending. Item 201's LoRA-less Recall cleared both lists. Before releasing the held LoRA response, both lists were empty. After `Fetch.continueResponse`, the delayed Generate tab showed `alienzkin-sdxl`, enabled, weight 1.75; the warm Upscale tab stayed empty. Releasing the inventory response and disabling Fetch interception restored ordinary network operation. This confirms the documented limitation; no Bediz delay, serialization, or browser workaround was added.

Ad hoc gallery images retained for the user (including upscale intermediates):

- `e6c7b44d-1a8f-4230-b1d3-8336e7830c67.png` — item 193
- `5a61981b-7155-4f99-90e7-cad17b27271f.png` — item 194
- `a4cca1fe-1a10-43bc-9ee3-9c44b6aad898.png` — item 195
- `36f05dcc-99d0-4278-a668-7b139a87ae99.png` — item 196
- `7d2dbbb4-df34-419d-b5ad-578915bbdd82.png` — item 197
- `24ce9346-9741-47b2-8f8c-accbe874bafd.png` — item 198 final
- `8aae64b4-12b1-4bf1-91de-2aeaff706f5c.png` — item 198 intermediate
- `b9362215-d646-4c26-aa0c-dba1f8cb356c.png` — item 198 intermediate
- `1d302536-5788-4703-9585-582945946d3a.png` — item 199
- `da1e5203-b198-44da-9702-f620875d2257.png` — item 200
- `fe771a9a-5c17-4821-9741-effa30018a38.png` — item 201

`BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passed (`TestLiveGate`, 133.494s). Its generated and uploaded fixtures self-cleaned. Every mandatory live step was available and passed; the overlap check confirmed the documented limitation. The separate model-install opt-in gate was not requested by this slice.

Code review against starting HEAD `8a83d2917ceefab9f1765205e2fe255dd498d13e` ran separate Standards and Spec agents. Spec found no actionable issue. Standards identified a missing `doctor` exit-status assertion and a duplicated default main resolver; both were corrected before commit and rechecked.

Final review recheck: Standards 0 remaining findings; Spec 0 findings. After the review repairs, `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed again. Rebuilding the binary and repeating `doctor --json` still reported `ready: true`, compatible Recall, and both synchronization levels `partial`. The extra verification browser tab was closed and CDP interception was disabled.
