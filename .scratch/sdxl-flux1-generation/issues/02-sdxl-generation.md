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

**Status:** ready-for-agent

- [ ] `generate` produces SDXL images through one tested enqueue, with defaults, validation, and an optional VAE override exactly as recorded.
- [ ] The Execution Receipt, `ui_sync_partial` warning, and standalone `recall` reflect SDXL behavior verified in a live browser.
- [ ] `doctor` advertises `generate/sdxl` only when its endpoints, invocations, and installed models are present on a supported version.
