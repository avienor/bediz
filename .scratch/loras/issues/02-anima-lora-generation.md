# 02: Apply LoRAs to Anima generation

**What to build:** `generate` applies LoRAs to Anima generation, in both text-to-image and image-to-image, under the contract ticket 01 established. The feature spec (`.scratch/loras/spec.md`) and the V1 spec LoRA subsection hold the shared rules. Anima-specific behavior:

- **Resolution:** A LoRA must have type `lora` and base `anima`. Any other base is `unsupported_capability`. An Anima main model with `loras` is no longer `unsupported_capability`. A FLUX.1 main model with `loras` stays unsupported unless ticket 03 has landed.
- **Graph:** The tested Anima graph for the Generation Mode, including its negative conditioning branch, plus:
  - one intermediate `anima_lora_loader` per LoRA, chained in request order, each with the complete Model Identifier as `lora` and the resolved `weight`;
  - the first loader taking `anima_model_loader`'s `transformer` and `qwen3_encoder`, and each later loader taking the previous loader's outputs;
  - the last loader's `transformer` feeding `anima_denoise`, and its `qwen3_encoder` feeding both the positive and negative `anima_text_encoder`;
  - `core_metadata.loras` set to `[{model, weight}]` in request order.

  The VAE path and the image-to-image encoder path are unchanged. Graphs without LoRAs, and their fixtures, are byte-for-byte unchanged.
- **UI Synchronization:** The interim rule from ticket 01 applies: a generation with LoRAs appends `loras` to the Anima `not_restored` list.
- **Doctor:** A new row `generate`/`anima` with `setting: "loras"` requires:
  - the Anima text-to-image requirements;
  - `AnimaLoRALoaderInvocation` (`anima_lora_loader`) with `lora`, `weight`, `transformer`, and `qwen3_encoder`;
  - `CoreMetadataInvocation` with its text-to-image properties plus `loras`.

  Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. The row has no LoRA model requirement. `generate` checks the same requirements before any upload or enqueue for a request with LoRAs. A missing `anima_lora_loader` schema or property makes only the Anima LoRA row incompatible. A missing `core_metadata.loras` affects every LoRA row as ticket 01 records.

**Blocked by:** 01: Apply LoRAs to SDXL generation.

**Execution route:** `worker + independent review`. Ticket 01 fixes the public contract. This slice adds one family's LoRA branch, and fixtures, public tests, and a live run can detect an incorrect one.

**Verification gate:**
- Versioned InvokeAI 6.14.1 Anima LoRA enqueue fixtures are checked against the live OpenAPI invocation vocabulary. They cover one LoRA with text-to-image, two LoRAs whose chain order equals request order, and one LoRA with image-to-image.
- Public-seam tests prove:
  - that a non-Anima LoRA is `unsupported_capability` before any upload or enqueue;
  - that a missing `anima_lora_loader` schema or property fails an Anima LoRA `generate` before any upload or enqueue, and marks only the Anima LoRA row incompatible while the SDXL LoRA row and every mode row stay compatible;
  - that the positive and negative encoders both receive the last loader's Qwen3 encoder;
  - that component resolution is unchanged;
  - the Anima LoRA receipt and interim `not_restored` list;
  - that Anima fixtures and receipts without LoRAs are unchanged.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline with `Anima Base 1.0`, following the live-verification guide:
  - Install the consented `LyliaEngine/Anima_Detail_Tweaker` LoRA through `bediz models install`, or a substitute within the recorded consent. Confirm `models list --type lora` reports base `anima`.
  - A waited 768 × 768 text-to-image with the LoRA at 1.0, and the same request without it at the same seed. The outputs differ visibly, and the LoRA output's metadata shows `loras`.
  - An image-to-image request with the LoRA.
  - `doctor --json` reports the Anima LoRA row as compatible.
  - The live E2E gate passes unchanged.
- Report unavailable live steps and list ad hoc images.

**Review gate:**
- A separate reviewer reviews a fixed base/head diff, starting from this ticket, the feature spec, the V1 spec LoRA subsection, and the worker's evidence. The reviewer must be able to read the InvokeAI 6.14.1 invocation schemas and the built frontend bundle.
- Before any fix, the reviewer:
  - compares the fixtures with the spec's chain and the backend `anima_lora_loader`, and confirms that the rerouted consumers match the stock frontend's Anima LoRA branch (search the bundle for `anima_lora_collection_loader`);
  - independently reruns one live Anima LoRA generation and checks its seed, metadata, and visible effect against a LoRA-less run;
  - checks the public tests against the spec.
- Blocking findings: a graph difference the spec does not record, a contract drift from ticket 01, a changed LoRA-less fixture or receipt, a missing rejection test, or live evidence that cannot be reproduced.

**Escalate when:**
- InvokeAI does not record the consented or substitute LoRA as `type: lora` with base `anima`.
- `anima_lora_loader` rejects the chain, or the stock branch wires the negative encoder differently from the spec.
- A LoRA at weight 1.0 has no visible effect at a fixed seed.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec: the Anima row of the LoRA subsection, the §10 Anima LoRA row, and the §12 Anima interim `not_restored` addition.
- `CHANGELOG.md` under Unreleased. Tests.
- The live-verification guide's installed LoRAs.
- No ADR or glossary change: ADR-0024 and the terms from ticket 01 cover this slice.

**Status:** ready-for-agent

- [x] `generate` with an Anima main model and `loras` applies them through one tested enqueue in both Generation Modes, as recorded.
- [x] `doctor`, the V1 spec, the live-verification guide, and `CHANGELOG.md` reflect the behavior verified live.
- [ ] The independent review found no acceptance-blocking issue.

## Comments

- 2026-09-29: Confirmed local InvokeAI `6.14.1` at `http://127.0.0.1:9090`; the live `AnimaLoRALoaderInvocation` exposes `lora`, `weight`, `transformer`, `qwen3_encoder`, and the common node properties. The installed frontend bundle's `anima_lora_collection_loader` branch reroutes the denoiser and both positive and negative text encoders, matching the documented single-loader chain's consumers.
- `bediz models list --type lora --json` initially showed only SDXL LoRAs. `bediz models install --source-type url --source https://huggingface.co/LyliaEngine/Anima_Detail_Tweaker/resolve/main/Anima_Detail_Tweaker.safetensors --json` created job 2; `bediz models status --job-id 2 --json` completed with model key `5e9b17b5-e986-4cb8-83ef-a982326ab283`. `models list --type lora --json` confirmed `type: lora`, `base: anima`.
- `bediz doctor --json` reported `ready: true` and compatible `generate/anima/loras`; its loader schema and `core_metadata.loras` properties were available.
- Waited 768 × 768 Anima text-to-image with seed `41827`, 24 steps, and the same prompt produced `cab5cb50-165d-4769-959e-5bdd0005bc66.png` without LoRA (queue item 158) and `c85cb7ed-71c0-4bff-ba3c-9b84eee4ce8c.png` with `Anima_Detail_Tweaker` at `1.0` (item 159). The outputs differ visibly in cloak and facial detail. The LoRA image's InvokeAI metadata has seed `41827` and `loras: [{model: <complete Anima LoRA identifier>, weight: 1.0}]`; the LoRA-less receipt omits `loras`.
- Waited 768 × 768 image-to-image with the LoRA at `1.0`, source `cab5cb50-165d-4769-959e-5bdd0005bc66.png`, seed `41828`, and strength `0.6` produced `03625502-2dfa-451c-ac44-9930b46f2d90.png` (item 160). Its InvokeAI metadata records `anima_img2img`, source, seed, strength, and the LoRA. These three named images remain in the gallery as ad hoc verification outputs.
- `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passed, including the new Anima LoRA generation and metadata case; the gate cleaned up its own images.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
- The three primary waited generations above each ran `/tmp/bediz-anima-lora generate --request - --timeout 15m --json` with these exact standard-input Request Documents, respectively:
  - No LoRA: `{"schema_version":1,"model":"06409299-d28f-4c00-8416-4d23cb1b8358","positive_prompt":"Close-up portrait of an elven woman in an embroidered cloak, intricate facial details, soft cinematic lighting","negative_prompt":"blurry, text","width":768,"height":768,"steps":24,"seed":41827}` → `cab5cb50-165d-4769-959e-5bdd0005bc66.png`.
  - Text-to-image LoRA: `{"schema_version":1,"model":"06409299-d28f-4c00-8416-4d23cb1b8358","positive_prompt":"Close-up portrait of an elven woman in an embroidered cloak, intricate facial details, soft cinematic lighting","negative_prompt":"blurry, text","width":768,"height":768,"steps":24,"seed":41827,"loras":[{"model":"5e9b17b5-e986-4cb8-83ef-a982326ab283","weight":1.0}]}` → `c85cb7ed-71c0-4bff-ba3c-9b84eee4ce8c.png`.
  - Image-to-image LoRA: `{"schema_version":1,"model":"06409299-d28f-4c00-8416-4d23cb1b8358","positive_prompt":"Close-up portrait of an elven woman in an embroidered cloak, intricate facial details, soft cinematic lighting","negative_prompt":"blurry, text","width":768,"height":768,"steps":24,"seed":41828,"source":{"type":"image","reference":"cab5cb50-165d-4769-959e-5bdd0005bc66.png"},"strength":0.6,"loras":[{"model":"5e9b17b5-e986-4cb8-83ef-a982326ab283","weight":1.0}]}` → `03625502-2dfa-451c-ac44-9930b46f2d90.png`.
- Independent reviewer reran `go run ./cmd/bediz generate --model 06409299-d28f-4c00-8416-4d23cb1b8358 --prompt 'portrait of an elven alchemist wearing embroidered green cloak, cinematic fantasy painting, intricate facial detail' --negative-prompt 'blurry, text, watermark' --width 768 --height 768 --steps 24 --seed 771934 --output-count 1 --url http://127.0.0.1:9090 --json` → `8b048a08-4a01-449c-b516-64e6df094770.png`; the same command with `--lora 5e9b17b5-e986-4cb8-83ef-a982326ab283=1` before `--url` → `c46d1ebf-9ed2-415d-aedf-b2dcd92b8de1.png`. Both waited runs succeeded with seed `771934`; only the latter metadata contained the LoRA at weight `1`, and the images visibly differed. Both remain in the gallery.
