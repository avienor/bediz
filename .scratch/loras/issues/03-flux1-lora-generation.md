# 03: Apply LoRAs to FLUX.1 generation

**What to build:** `generate` applies LoRAs to FLUX.1 dev and schnell generation, in both text-to-image and image-to-image, under the contract ticket 01 established. The feature spec (`.scratch/loras/spec.md`) and the V1 spec LoRA subsection hold the shared rules. FLUX.1-specific behavior:

- **Resolution:** A LoRA must have type `lora` and base `flux`. Any other base, including `flux2`, is `unsupported_capability`. Control LoRAs are type `control_lora` and are rejected by type. Main-model variant and format support is unchanged from text-to-image: `dev` or `schnell`, in `checkpoint`, `bnb_quantized_nf4b`, or `gguf_quantized` format. A FLUX.1 main model with `loras` is no longer `unsupported_capability`.
- **Graph:** The tested FLUX.1 graph for the Generation Mode, plus:
  - one intermediate `flux_lora_loader` per LoRA, chained in request order, each with the complete Model Identifier as `lora` and the resolved `weight`;
  - the first loader taking `flux_model_loader`'s `transformer`, `clip`, and `t5_encoder`, and each later loader taking the previous loader's outputs;
  - the last loader's `transformer` feeding `flux_denoise`, and its `clip` and `t5_encoder` feeding `flux_text_encoder`;
  - `core_metadata.loras` set to `[{model, weight}]` in request order.

  The `max_seq_len`, VAE, and image-to-image encoder paths are unchanged. Graphs without LoRAs, and their fixtures, are byte-for-byte unchanged.
- **UI Synchronization:** The interim rule from ticket 01 applies: a generation with LoRAs appends `loras` to the variant-specific FLUX.1 `not_restored` list.
- **Doctor:** A new row `generate`/`flux` with `setting: "loras"` requires:
  - the FLUX.1 text-to-image requirements;
  - `FluxLoRALoaderInvocation` (`flux_lora_loader`) with `lora`, `weight`, `transformer`, `clip`, and `t5_encoder`;
  - `CoreMetadataInvocation` with its text-to-image properties plus `loras`.

  Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. The row has no LoRA model requirement. `generate` checks the same requirements before any upload or enqueue for a request with LoRAs. A missing `flux_lora_loader` schema or property makes only the FLUX.1 LoRA row incompatible. A missing `core_metadata.loras` affects every LoRA row as ticket 01 records.

**Blocked by:** 01: Apply LoRAs to SDXL generation.

**Execution route:** `worker + independent review`. Ticket 01 fixes the public contract. This slice adds one family's LoRA branch, and fixtures, public tests, and live runs on both variants can detect an incorrect one.

**Verification gate:**
- Versioned InvokeAI 6.14.1 FLUX.1 LoRA enqueue fixtures are checked against the live OpenAPI invocation vocabulary. They cover dev and schnell text-to-image with one LoRA, two LoRAs whose chain order equals request order, and one LoRA with image-to-image.
- Public-seam tests prove:
  - that `flux2`, other-base, and `control_lora` selectors are `unsupported_capability` before any upload or enqueue;
  - that a missing `flux_lora_loader` schema or property fails a FLUX.1 LoRA `generate` before any upload or enqueue, and marks only the FLUX.1 LoRA row incompatible while the other LoRA rows and every mode row stay compatible;
  - that both text encoder inputs receive the last loader's `clip` and `t5_encoder`;
  - that existing negative-prompt and schnell-guidance rejections still apply with LoRAs;
  - the FLUX.1 LoRA receipt and interim `not_restored` list;
  - that FLUX.1 fixtures and receipts without LoRAs are unchanged.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline with the BnB NF4 dev and schnell models, following the live-verification guide:
  - Install the consented `XLabs-AI/flux-RealismLora` LoRA through `bediz models install`, or a substitute within the recorded consent. Confirm `models list --type lora` reports base `flux`.
  - A waited 768 × 768 dev text-to-image with the LoRA at 1.0, and the same request without it at the same seed. The outputs differ visibly, and the LoRA output's metadata shows `loras`.
  - A schnell text-to-image with the LoRA, and one image-to-image request with the LoRA.
  - `doctor --json` reports the FLUX.1 LoRA row as compatible.
  - The live E2E gate passes unchanged.
- Report unavailable live steps, including any VRAM limit, and list ad hoc images.

**Review gate:**
- A separate reviewer reviews a fixed base/head diff, starting from this ticket, the feature spec, the V1 spec LoRA subsection, and the worker's evidence. The reviewer must be able to read the InvokeAI 6.14.1 invocation schemas and the built frontend bundle.
- Before any fix, the reviewer:
  - compares the fixtures with the spec's chain and the backend `flux_lora_loader`, and confirms that the rerouted consumers match the stock frontend's FLUX LoRA branch (search the bundle for `flux_lora_collection_loader`);
  - independently reruns one live FLUX.1 LoRA generation and checks its seed, metadata, and visible effect against a LoRA-less run;
  - checks the public tests against the spec.
- Blocking findings: a graph difference the spec does not record, a contract drift from ticket 01, a changed LoRA-less fixture or receipt, a missing rejection test, or live evidence that cannot be reproduced.

**Escalate when:**
- A LoRA fails on the BnB NF4 transformer or the int8 T5 encoder for a reason other than available memory.
- InvokeAI does not record the consented or substitute LoRA as `type: lora` with base `flux`.
- A LoRA at weight 1.0 has no visible effect at a fixed seed.
- A LoRA behaves differently on schnell in a way that would need a variant rule.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec: the FLUX.1 row of the LoRA subsection, the §10 FLUX.1 LoRA row, and the §12 FLUX.1 interim `not_restored` addition.
- `CHANGELOG.md` under Unreleased. Tests.
- The live-verification guide's installed LoRAs.
- No ADR or glossary change: ADR-0024 and the terms from ticket 01 cover this slice.

**Status:** ready-for-agent

- [ ] `generate` with a FLUX.1 dev or schnell main model and `loras` applies them through one tested enqueue in both Generation Modes, as recorded.
- [ ] `doctor`, the V1 spec, the live-verification guide, and `CHANGELOG.md` reflect the behavior verified live.
- [ ] The independent review found no acceptance-blocking issue.
