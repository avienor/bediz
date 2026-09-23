# 02: Generate SDXL images with UI Synchronization and Recall

**What to build:** Register the SDXL family adapter so that `generate` accepts an installed main model with base `sdxl` and type `main`, in any format InvokeAI's SDXL loader accepts.

- **Defaults:** 1024 × 1024, 30 steps, scheduler `dpmpp_3m_k`, guidance 7.0, one output, empty negative prompt.
- **Validation:** Width and height are positive multiples of 8. Steps are positive. `guidance` is SDXL CFG scale: finite and at least 1. The scheduler must be one of the 31 InvokeAI 6.14.1 `SCHEDULER_NAME_VALUES` listed in the feature spec.
- **Prompts:** The negative prompt is supported, and the SDXL style prompts equal the positive and negative prompts.
- **VAE:** The only component is an optional `components.vae` / `--vae` override (base `sdxl`, type `vae`), resolved by exact key or unique name. Without it, the main model's bundled VAE is used, no VAE is selected automatically, and `component_keys` is empty. With it, `component_keys` is `{"vae": "<key>"}`. `qwen3_encoder`, `t5_encoder`, and `clip_embed` selectors are `invalid_request`.
- **Graph:** The stock InvokeAI 6.14.1 SDXL text-to-image topology: `sdxl_model_loader`, positive and negative `sdxl_compel_prompt`, `noise`, `denoise_latents`, `core_metadata`, and a non-intermediate `l2i` output node carrying the optional board. A `vae_loader` is added only for an override. Complete Model Identifiers are used, and the metadata records the resolved settings and models. The batch seed data keeps `item_ids` and `resolved_settings.seeds` aligned by position, exactly as the Anima adapter does.
- **Execution:** Submission, waiting, `--no-wait`, and receipts follow V1 §11.5–11.6 unchanged.
- **Automatic UI Synchronization:** Recall patches prompts, model, dimensions, steps, and the first seed, and also `cfg_scale` from the resolved guidance, because the stock 6.14.1 frontend applies it. It emits `ui_sync_partial` with `not_restored` set to `scheduler`, `vae`, `output_count`, and `board_id`, plus `guidance` if live verification shows CFG is not restored. If the Recall capability now depends on `cfg_scale`, its schema check requires it.
- **Standalone `recall`:** It accepts SDXL main models for its existing fields, with SDXL alignment and the 64-pixel minimum.
- **Doctor:** `doctor` registers `generate/sdxl` with its endpoints, invocation schemas, and installed-model requirement. It carries `ui_sync: partial` only when Recall is compatible.

**Blocked by:** 01: Dispatch generation, Recall, and capability checks through Model Family adapters.

**Execution route:** `worker + independent review` — every product decision is recorded, and a graph fixture, public-seam tests, and a live generation plus browser check can reliably detect a wrong implementation.

**Verification gate:**
- A versioned SDXL enqueue fixture for InvokeAI 6.14.1 covers the bundled VAE and a VAE override, with and without a board. It is checked against the live OpenAPI invocation vocabulary.
- Public-seam tests prove:
  - defaults and explicit overrides;
  - every approved scheduler accepted and an Anima-only or unknown scheduler rejected;
  - misaligned or single-sided dimensions, non-finite or low guidance, and inapplicable components rejected before enqueue;
  - VAE override ambiguity → `selection_required`, and an incompatible override → `unsupported_capability`;
  - no automatic VAE selection;
  - seed ordering for multiple outputs, a single enqueue, no retry, and `outcome_unknown` on an inconclusive enqueue;
  - Recall failure → `ui_sync_failed` with a successful receipt;
  - the SDXL `not_restored` list and the `cfg_scale` patch;
  - `recall` accepting an SDXL model and rejecting 8-aligned dimensions below 64;
  - `doctor` matching.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide. `Juggernaut XL v9` and `sdxl-vae-fp16-fix` were installed through Bediz's starter installation on 2026-09-23:
  - one waited two-output SDXL generation with an explicit seed produces a receipt whose images carry the matching seeds;
  - one generation with the VAE override completes;
  - `doctor --json` reports `generate/sdxl` as compatible with `ui_sync: partial`;
  - using the `browser-harness-local` skill, a browser records which InvokeAI UI controls show the recalled prompt, model, dimensions, steps, seed, and CFG after model loading has settled;
  - the live E2E gate (`BEDIZ_E2E_URL` set) passes with `generate/sdxl` registered and gains a self-cleaning SDXL generation case alongside the Anima one.
- Report any live step that was unavailable.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer independently compares the SDXL fixture's node types, fields, and edges with InvokeAI 6.14.1's own SDXL text-to-image graph. This includes the style prompts, the noise and seed wiring, the metadata fields, and the fp32 or VAE decode settings.
- The reviewer reruns or inspects the live receipt, and verifies the seed-to-image association against the queue items' metadata.
- The reviewer confirms that the `not_restored` list matches the recorded browser observation.
- Blocking findings: graph divergence from stock behavior without a recorded reason; any silently ignored inapplicable field; automatic VAE selection; seed misassociation; a success receipt for a failed or partial batch; a sync claim not supported by browser evidence; `doctor` advertising a capability that failed its checks.

**Escalate when:** The stock SDXL graph requires inputs that the feature spec does not record. The default SDXL scheduler or VAE produces broken output on the baseline model. The frontend resets recalled values after model loading. InvokeAI's `cfg_scale` Recall behavior differs from the bundled handler. Starter installation needs a source form or credential the spec does not allow. Repeated repair loops fail.

**Permanent records:** V1 spec and tests. §2 and §11.1–11.6 record the SDXL fields, defaults, bounds, scheduler set, optional VAE override semantics, graph contract, and receipt `component_keys`. §12 records the SDXL Recall fields, the `cfg_scale` restoration evidence, and the `not_restored` list. §10 records the `generate/sdxl` doctor entry. No new terminology. No new ADR, because ADR 0017 already allows reporting verified per-field restoration.

**Status:** implemented

- [x] `generate` produces SDXL images through one tested enqueue, with defaults, validation, and an optional VAE override exactly as recorded.
- [x] The Execution Receipt, `ui_sync_partial` warning, and standalone `recall` reflect SDXL behavior verified in a live browser.
- [x] `doctor` advertises `generate/sdxl` only when its endpoints, invocations, and installed models are present on a supported version.

## Comments

- 2026-09-23: `curl -fsS http://127.0.0.1:9090/api/v1/app/version` reported InvokeAI 6.14.1. `bediz models list --json` reported main model `ba02e32a-bc3b-41e7-bb18-ed7678ee5765` (`Juggernaut-XL-v9`) and VAE `21e21480-70b1-42dc-b96a-55a829591a07` (`sdxl-vae-fp16-fix`). `bediz doctor --json` reported `generate/sdxl` compatible with `ui_sync: partial`, and overall readiness true.
- 2026-09-23: Waited `bediz generate --model ba02e32a-bc3b-41e7-bb18-ed7678ee5765 --prompt 'a small turquoise ceramic vase on a wooden table, studio photograph' --negative-prompt 'text, watermark' --width 768 --height 768 --steps 3 --seed 424242 --output-count 2 --json` completed. Receipt queue items `[35,34]` and ordered seeds `[424242,424243]` matched each item's `field_values` seed. Gallery images are `c642acf3-b177-48f4-8a11-386e0cc514f8.png` and `26f847ab-cfe6-4593-8b34-418c2c2a666b.png`.
- 2026-09-23: Waited `bediz generate --model ba02e32a-bc3b-41e7-bb18-ed7678ee5765 --vae 21e21480-70b1-42dc-b96a-55a829591a07 --prompt 'a small amber glass bottle on a white table, studio photograph' --negative-prompt 'text, watermark' --width 768 --height 768 --steps 2 --seed 424250 --json` completed with `component_keys.vae` set to the override and image `1f55be9e-a5f4-4731-a87b-0505c390a447.png`.
- 2026-09-23: With the InvokeAI browser tab open before Recall, `browser-harness-local` observed the Generate controls after model loading: positive prompt `a small turquoise ceramic vase on a wooden table, studio photograph`, negative prompt `text, watermark`, main model `Juggernaut-XL-v9`, width and height `768`, steps `3`, seed `424242`, and CFG Scale `7`. The stock frontend handler applies `cfg_scale`; it has no handlers for scheduler, VAE, output count, or board. The browser observation supports omitting `guidance` from SDXL `not_restored`.
- 2026-09-23: `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test ./e2e -run TestLiveGate -count=1 -v` passed, including `generate/sdxl` doctor readiness and a self-cleaning SDXL image case. The E2E image `0408f471-4ad7-4cf9-bf46-5934b74e0ba5.png` was deleted by test cleanup.
- 2026-09-23: A final comparison with the stock frontend's generation-state defaults found `vaePrecision: fp32`, which makes SDXL `l2i.fp32` true. The first fixture had false; the fixture, compiler, and V1 spec were corrected. The earlier live images were generated before this correction; a fresh live check follows below.
- 2026-09-23: After the fp32 correction, waited SDXL generations at 768 × 768 and 2 steps completed both with the bundled VAE (seed `424260`, queue item 39, image `1267dd1f-5700-4f6b-a3c4-1b1218d34e3f.png`) and with the explicit `sdxl-vae-fp16-fix` override (seed `424261`, queue item 40, image `208427ce-768b-4dc9-9fb4-f5474b15745a.png`). Both receipts carried the expected `component_keys`.
- 2026-09-23: Both versioned SDXL enqueue fixtures were checked against the live `/openapi.json` invocation schemas: all 12 bundled-VAE nodes and 13 override nodes use known fields and all 21 edge destinations name accepted invocation fields.
- 2026-09-23: Re-ran `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test ./e2e -run TestLiveGate -count=1 -v` after the fp32 correction; all subtests passed. The final E2E image `9e0965b7-9293-4496-9f5e-11fa70c75fd0.png` was deleted by test cleanup.
- 2026-09-23: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed after the final SDXL component-presence fix.
- 2026-09-23: Inspected two 30-step, 768 × 768 live outputs after the fp32 correction. The bundled-VAE run (seed `424270`, queue item 43) produced a coherent red apple studio photograph in `f7cf948d-f332-4105-bfca-07327d501f2d.png`; the explicit `sdxl-vae-fp16-fix` run (seed `424271`, queue item 44) produced a coherent blue mug studio photograph in `1617f1b1-afd8-4f19-bad2-0c28eebae175.png`. The heavy banding seen in the two-step checks was due to their abbreviated sampling, not the default scheduler or VAE.
- 2026-09-23: Independent review found that an explicitly empty SDXL VAE override had been treated as omission. Public CLI tests reproduced an unwanted enqueue for both JSON and `--vae ""`. The request now preserves VAE selector presence and rejects empty overrides before enqueue or Recall; the V1 spec records this distinction.
- 2026-09-23: After the review fix, `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed. The live `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test ./e2e -run TestLiveGate -count=1 -v` also passed for Anima and SDXL; its SDXL image `bd5673c7-46ef-4e2b-8182-ffc119f87f7b.png` was deleted by cleanup.
