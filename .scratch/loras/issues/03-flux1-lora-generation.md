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

**Status:** implemented


- [x] `generate` with a FLUX.1 dev or schnell main model and `loras` applies them through one tested enqueue in both Generation Modes, as recorded.
- [x] `doctor`, the V1 spec, the live-verification guide, and `CHANGELOG.md` reflect the behavior verified live.
- [x] The independent review found no acceptance-blocking issue.

## Comments

- 2026-09-29: Confirmed InvokeAI `6.14.1` at `http://127.0.0.1:9090`. Its live OpenAPI includes `FluxLoRALoaderInvocation` with `lora`, `weight`, `transformer`, `clip`, `t5_encoder`, and the common node properties. Read the installed backend `app/invocations/flux_lora_loader.py` and the built `App-CKkzUo1u.js` bundle: `flux_lora_collection_loader` reroutes the denoiser's transformer and the text encoder's CLIP and T5, leaving max sequence length and VAE paths unchanged. The six new dev/schnell enqueue fixtures use this documented ordered single-loader chain and their input fields were checked against live OpenAPI. Existing LoRA-less fixture files are unchanged.
- `go run ./cmd/bediz models install --source-type url --source https://huggingface.co/XLabs-AI/flux-RealismLora/resolve/main/lora.safetensors --json` submitted job `3`; `go run ./cmd/bediz models status --job-id 3 --json` completed with `22431400` bytes and key `d3721ab4-30e8-4246-8daf-dcdec06f8f49`. `models list --type lora --json` confirmed name `lora`, base `flux`, type `lora`, format `lycoris`. This is the consented XLabs-AI realism LoRA (22 MB, FLUX.1 [dev] Non-Commercial License).
- `/tmp/bediz-flux-lora doctor --json` reported `ready: true` and compatible `generate/flux/loras`, without mode or UI Synchronization fields on that row.
- `/tmp/bediz-flux-lora generate --model 6b6e6ba3-368f-42ee-b6e5-033bba37aa7b --prompt 'Portrait photograph of a woman in a textured wool coat, freckles, realistic skin, natural window light, shallow depth of field' --width 768 --height 768 --steps 24 --seed 52913 --output-count 1 --timeout 20m --json` produced `b34e273b-d343-4b23-9b79-24a27d338262.png` (queue item `177`). The same command with `--lora d3721ab4-30e8-4246-8daf-dcdec06f8f49=1` produced `754aafa7-d10e-44d9-b07a-959d5a858da8.png` (item `178`). Both waited runs succeeded with seed `52913`; the LoRA result visibly differs in expression, freckles, and coat texture. `GET /api/v1/images/i/<image_name>/metadata` confirms only the latter has `loras`, with the complete LoRA Model Identifier and weight `1.0`.
- `/tmp/bediz-flux-lora generate --model 385ce753-8fb8-46fc-ab74-aac4924a9663 --prompt 'Portrait photograph of a woman in a textured wool coat, freckles, realistic skin, natural window light, shallow depth of field' --width 768 --height 768 --steps 4 --seed 52914 --output-count 1 --lora d3721ab4-30e8-4246-8daf-dcdec06f8f49=1 --timeout 20m --json` completed schnell text-to-image and produced `6f6255d2-71e4-44d6-86ae-5a18edd088cb.png` (item `179`).
- `/tmp/bediz-flux-lora generate --model 6b6e6ba3-368f-42ee-b6e5-033bba37aa7b --prompt 'Portrait photograph of a woman in a textured wool coat, freckles, realistic skin, natural window light, shallow depth of field' --width 768 --height 768 --steps 24 --seed 52915 --output-count 1 --image b34e273b-d343-4b23-9b79-24a27d338262.png --strength 0.6 --lora d3721ab4-30e8-4246-8daf-dcdec06f8f49=1 --timeout 20m --json` completed image-to-image and produced `79f3de2b-e55f-4e89-b5c1-4134443baa84.png` (item `180`). Backend metadata records `flux_img2img`, seed `52915`, source image, strength `0.6`, and the LoRA at `1.0`; schnell metadata likewise records its seed and LoRA. The interim warnings append `loras` after the variant's existing fields and, for image-to-image, after `source_image` and `strength`.
- All four primary ad hoc images named above remain in the gallery. No primary live step was unavailable and no VRAM failure occurred.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed. Public tests cover matching-base resolution and weights, both variants and accepted main-model formats, full enqueue fixtures in both modes, variant-specific receipt warnings, omitted LoRA members without LoRAs, incompatible selectors and settings, every LoRA loader property, and isolated doctor failures.
- `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passed in `198.270s`, retaining every existing live generation, source-upload, LoRA, and upscale case. Only its doctor expectations gained the new FLUX.1 LoRA row; it cleaned up its own images.
- Independent Standards and Spec reviewers inspected the fixed diff `ad36070812232168ebcdc9ccfde722e5854c3487...0e0c731caa9de2af3fffc1d09c182c7ba914eb1b`, an immutable implementation snapshot. Standards: no documented-standard violation or actionable baseline smell. Spec: no missing requirement, scope creep, incorrect behavior, or acceptance-blocking finding. The final commit adds only this ticket's completion and evidence records to the reviewed implementation. The Spec reviewer independently verified all six fixtures against fresh live input and output schemas, the backend loader, and the built frontend bundle; confirmed all eight existing FLUX.1 LoRA-less fixture files are unchanged; and reran `go test ./internal/generation ./internal/cli ./internal/doctor ./internal/capability -run 'LoRA|FLUX' -count=1` successfully.
- Independent reviewer ran `/tmp/bediz-flux-lora generate --model 6b6e6ba3-368f-42ee-b6e5-033bba37aa7b --prompt 'Portrait photograph of a middle-aged man with curly brown hair and a linen shirt, realistic skin texture, soft afternoon window light, shallow depth of field' --width 768 --height 768 --steps 24 --seed 64027 --output-count 1 --timeout 20m --url http://127.0.0.1:9090 --json` → `7b91552f-52ce-4923-9fa6-d86ec105986f.png` (item `191`). The same command with `--lora d3721ab4-30e8-4246-8daf-dcdec06f8f49=1` → `91a9d77c-5df8-40a6-8f20-d6c666428cd2.png` (item `192`). Both waited runs returned seed `64027`; backend metadata confirms that seed on both, with the complete LoRA identifier at weight `1.0` only on the latter. The reviewer viewed both images and observed differences in skin texture, hair detail, and shirt folds. Both remain in the gallery, bringing the ad hoc total to six images. Independent `/tmp/bediz-flux-lora doctor --url http://127.0.0.1:9090 --json` also reported ready and compatible `generate/flux/loras` without `mode` or `ui_sync`. No live step was unavailable.
