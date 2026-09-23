# 05: Synchronize upscale parameters to the InvokeAI UI

**What to build:** After a successful upscale enqueue, attempt partial UI Synchronization through InvokeAI's Recall API, as recorded in the `generative-upscale` feature spec:

- **Recall patch:** One patch, never retried, containing the resolved positive and negative prompts, the main model, steps, and the seed. The stock 6.14.1 Upscale tab shares these values with the Generate tab. The patch never sends width, height, or `cfg_scale`: dimensions do not apply to the Upscale panel, and stock Recall writes `cfg_scale` to the Generate tab's CFG, not the upscale CFG.
- **Main model:** The existing display-name uniqueness rule applies. A display-name collision, a missing Recall endpoint, an incompatible patch schema, or an inconclusive or rejected Recall adds a `ui_sync_failed` warning. The upscale outcome, the single enqueue, and the Execution Receipt are unchanged.
- **Success warning:** A successful patch adds a `ui_sync_partial` warning to the envelope and receipt. Its `not_restored` list is `source_image`, `upscale_model`, `scale`, `creativity`, `structure`, `tile_controlnet`, `tile_size`, `tile_overlap`, `scheduler`, `guidance`, `vae`, and `board_id`. The list changes only if live browser evidence shows a field is restored or a patched field is not.
- **Doctor:** `upscale/sdxl` and `upscale/sd-1` carry `ui_sync: partial`, and `ui_sync.upscale` is `partial`, only when the supported version, Direct Execution, and tested Recall requirements are all compatible.
- **Unchanged:** Standalone `recall` still rejects SD1.5 main models. Upscale Synchronization uses its own tested patch, not the generation family registry.

**Blocked by:**
- 02: Upscale an existing InvokeAI image with an SDXL model.
- 03: Upscale with an SD1.5 model.

**Execution route:** `worker + independent review` — the patch fields and warning contract are recorded, and public-seam tests plus a live browser observation of the Upscale tab reliably detect an incorrect claim.

**Verification gate:**
- Public-seam tests prove:
  - the exact Recall patch body for SDXL and SD1.5 upscales, including the absence of width, height, and `cfg_scale`;
  - the `ui_sync_partial` warning and its `not_restored` list;
  - `ui_sync_failed` with a successful receipt for a Recall failure, an inconclusive Recall, and a display-name collision;
  - no Recall after a failed or inconclusive enqueue;
  - Recall is attempted for `--no-wait`, matching generation;
  - `doctor` reporting `ui_sync.upscale: partial` only with compatible Recall, and omitting it otherwise;
  - standalone `recall` still rejecting SD1.5.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide:
  - using the `browser-harness-local` skill, with InvokeAI open before the upscale, the browser records the Upscale tab's prompt, negative prompt, main model, steps, and seed after model loading settles, for one SDXL and one SD1.5 upscale;
  - the browser also records whether any `not_restored` control changed;
  - the Generate tab's CFG is observed as unchanged;
  - `doctor --json` reports `ui_sync.upscale: partial`;
  - the live E2E gate passes.
- Report any live step that was unavailable, and list the gallery images it created.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer confirms that the `not_restored` list and the patched fields match the recorded browser observation and the stock frontend's `recall_parameters_updated` handler.
- The reviewer checks that Recall runs only after a conclusive enqueue and is never retried.
- Blocking findings: a synchronization claim not supported by browser evidence; sending `cfg_scale` or dimensions; a Recall failure that changes the upscale outcome; `doctor` advertising `ui_sync.upscale` when Recall is incompatible.

**Escalate when:** The stock frontend applies recalled values differently in the Upscale tab than the bundled handler suggests. Recall of the main model resets Upscale panel controls. Synchronization would need browser storage or DOM manipulation, which V1 §12 forbids. Repeated repair loops fail.

**Permanent records:** V1 spec and tests. §12 records the upscale Recall patch fields, the omitted `cfg_scale` and dimensions with their reason, the `not_restored` list backed by browser evidence, and that standalone `recall` excludes SD1.5. §10 records `ui_sync.upscale: partial` for the registered upscale entries. No new ADR: ADR 0012 and ADR 0017 already define partial upscale synchronization.

**Status:** completed

- [x] A successful upscale sends one tested Recall patch and reports `ui_sync_partial` with a browser-verified `not_restored` list.
- [x] A Recall failure produces `ui_sync_failed` without changing the upscale outcome.
- [x] `doctor` reports `ui_sync.upscale: partial` only when Recall is compatible.

## Comments

- 2026-09-24: The stock 6.14.1 `recall_parameters_updated` handler (`frontend/web/dist/assets/App-CKkzUo1u.js`) writes `positive_prompt`, `negative_prompt`, `seed`, `steps`, `width`, `height`, and `cfg_scale` to the shared `params` slice and loads `model` as the main model. The Upscale tab reads prompts, model, steps, and seed from that slice, but its CFG and scheduler come from `upscaleCfgScale` and `upscaleScheduler`. This agrees with the recorded patch fields and with omitting `cfg_scale` and dimensions.
- 2026-09-24: Live browser check against InvokeAI 6.14.1 at `http://127.0.0.1:9090`, with the UI already open on the Upscaling tab before each upscale. Before: prompt `a tiny red teacup on white background`, no negative prompt (FLUX model), main model `flux_schnell_flux1-schnell-bnb_nf4`, steps 4, seed 44, upscale CFG 2, upscale scheduler KDPM 2, creativity 0, structure 0, scale 4x, tile size 1024, tile overlap 128, upscale model `RealESRGAN_x4plus.pth`, no source image, no Tile ControlNet, VAE `FLUX.1-schnell_ae`. Generate tab CFG (persisted `params.cfgScale`) was 7.
  - SDXL: `bediz upscale --image-path <512×512 PNG> --model Juggernaut-XL-v9 --tile-controlnet controlnet-tile-sdxl-1.0 --prompt "bediz sync check sdxl lighthouse" --negative-prompt "blurry sdxl" --steps 7 --seed 424242 --scale 2 --creativity 3 --structure -2 --tile-size 512 --tile-overlap 64 --guidance 3.5 --scheduler euler --timeout 10m --json` succeeded (queue item 79, batch `a8dd9eb7-f349-4549-a63d-6d9e10c6879b`) with one `ui_sync_partial` warning. After model loading settled, the Upscale tab showed prompt `bediz sync check sdxl lighthouse`, negative prompt `blurry sdxl`, model `Juggernaut-XL-v9`, steps 7, and seed 424242. Scale stayed 4x, creativity 0, structure 0, scheduler KDPM 2, CFG 2, tile size 1024, tile overlap 128, upscale model `RealESRGAN_x4plus.pth`, and no source image. VAE showed the default (the FLUX VAE is incompatible with SDXL).
  - SD1.5: the same shape with `--model dreamshaper-8 --tile-controlnet control_v11f1e_sd15_tile --prompt "bediz sync check sd15 meadow" --negative-prompt "noisy sd15" --steps 9 --seed 515151 --creativity -4 --structure 5 --tile-size 576 --tile-overlap 48 --guidance 5.5 --scheduler dpmpp_2m` succeeded (queue item 80, batch `ca219f61-3bb4-40e0-986b-b46acb518e1c`) with `ui_sync_partial`. The Upscale tab showed prompt `bediz sync check sd15 meadow`, negative prompt `noisy sd15`, model `dreamshaper-8`, steps 9, and seed 515151. All other controls were unchanged as above.
  - The Tile Control selector changed from empty to `controlnet-tile-sdxl-1.0`, then to `control_v11f1e_sd15_tile`. The patch carries no ControlNet: the stock frontend's model-change listener selects the first installed ControlNet of the new base whose name contains `tile` when the current selection is unavailable. `tile_controlnet` stays in `not_restored`, because that auto-selection does not guarantee the ControlNet Bediz used.
  - The Generate tab's CFG Scale still showed 7 after both upscales, and persisted `params.cfgScale` remained 7.
  - `bediz doctor --json` reported `ready: true`, `ui_sync: {"generate": "partial", "upscale": "partial"}`, and `upscale/sdxl` and `upscale/sd-1` compatible with `ui_sync: partial`.
  - `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passed (`live E2E: VERIFIED`), including both upscale subtests, which now require `ui_sync_partial` with the recorded `not_restored` list.
- Gallery images created by the browser checks, left in InvokeAI for the user to remove: uploaded sources `90b16f31-a87a-4402-895d-1d5f086d703a.png` (SDXL) and `4222e92d-6b8c-43a9-8a61-94e170fbd8ec.png` (SD1.5); outputs `8ae4e107-c305-4c37-ba45-2d342f6f0eea.png` and `089e6f13-bbd4-4f52-b7d0-c21afdb881c5.png`; intermediates `e14f51d8-3c59-4ebb-9530-b48a7ac1b343.png` and `f3806350-c453-4e1d-8572-9dec750c8afa.png` (item 79), and `6279d0be-abbf-4661-a290-c21431060a18.png` and `c8f99f6e-a52a-4fcd-9af7-0559fb94d8e8.png` (item 80). The E2E gate removed its own images.
- 2026-09-24: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
- 2026-09-24: At the user's request, V1 §12 now records the side effect: when the recalled main model has a different base, the stock frontend replaces incompatible Tile ControlNet and VAE selections itself; both stay in `not_restored`.
