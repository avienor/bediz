# 02: Upscale an existing InvokeAI image with an SDXL model

**What to build:** Add `bediz upscale` as the tracer bullet for Generative Upscale. It covers an SDXL main model and a source that is already an InvokeAI image. All decisions are recorded in the `generative-upscale` feature spec. In summary:

- **Request:** The schema-version-1 Upscale Request Document and its matching flags compile to the same typed operation. `source` is the typed object `{"type": "image", "reference": "<image name>"}`, with flag `--image`. The other fields are `model`, `positive_prompt`, `negative_prompt`, `scale`, `creativity`, `structure`, `steps`, `scheduler`, `guidance`, `seed`, `tile_size`, `tile_overlap`, `board_id`, and `components.upscale_model`, `components.tile_controlnet`, and `components.vae`. Their flags, defaults, and bounds are in the feature spec's table. `scale` accepts only 2, 4, and 8. A `path` source type is recognized but returns `unsupported_capability` until ticket 04. Unknown fields, and operation flags mixed with `--request`, are `invalid_request`.
- **Validation order:** Every bound is checked before network. Then come the version check, main-model resolution, component resolution, the OpenAPI vocabulary check, and a read-only source-image check (`not_found` when the image is absent), all before the single enqueue. A main model whose base is not `sdxl`, or whose variant is not `normal`, returns `unsupported_capability`.
- **Upscale Component Set:** The Spandrel model follows the V1 §11.4 rules, including automatic selection of exactly one compatible model. InvokeAI records no Spandrel scale, so this selection does not prove that the model upscales; output verification (below) enforces it. The Tile ControlNet is never selected automatically: without a selector, Bediz returns `selection_required` (kind `tile_controlnet`) listing every SDXL ControlNet, even when only one exists, or `missing_component` naming the `xinsir/controlNet-tile-sdxl-1.0` starter source. The VAE is an optional override with no automatic selection.
- **Graph:** The feature spec's stock InvokeAI 6.14.1 Upscale graph with the SDXL branch: Spandrel autoscale, unsharp mask, tiled `i2l`, `l2i`, and multi-diffusion denoise, two collected Tile ControlNet nodes with the recorded creativity and structure formulas, `fp32: true`, the optional board, `core_metadata` with the stock upscale fields, and an optional `vae_loader`.
- **Execution:** One `enqueue_batch` mutation, never retried, with one queue item and one resolved seed. The command waits by default. `--no-wait`, `--timeout`, `wait_timeout`, `interrupted`, and failed or canceled items behave as for generation.
- **Output dimensions:** The expected output is `floor(source × scale / 8) × 8` in each dimension, from the source Image Reference. After waiting, a completed output with other dimensions is never a success: it returns `invokeai_operation_failed` with `reason: "scale_not_applied"`, the expected and actual dimensions, the queue identifiers, and the output and source Image References. `--no-wait` reports the expected dimensions without verifying them.
- **Receipt:** `operation: "upscale"`, with `submitted_request`, `source_image`, `source_uploaded: false`, `resolved_settings` (including the expected `output_width` and `output_height`, `component_keys`, and a one-element `seeds`), `queue`, `outputs`, and `warnings`, as recorded in the feature spec. No UI Synchronization is attempted yet, so `warnings` is empty.
- **Doctor:** `doctor` registers `upscale/sdxl` with its endpoints, invocation vocabulary, and installed-model requirements (an SDXL `normal` main model, a Spandrel model, and an SDXL ControlNet), without a `ui_sync` level. These requirements establish presence only. They do not claim that the ControlNet is a Tile model or that the Spandrel model enlarges images. The live E2E gate gains a self-cleaning SDXL upscale case.

**Blocked by:** 01: Share graph-operation plumbing between generation and upscale.

**Execution route:** `worker + independent review` — the public contract and graph are fully recorded in the feature spec, and a fixture compared with the stock builder, public-seam tests, and a live upscale reliably detect a wrong implementation.

**Verification gate:**
- A versioned SDXL upscale enqueue fixture for InvokeAI 6.14.1 covers the bundled VAE and a VAE override, with and without a board. It is checked against the live OpenAPI invocation vocabulary.
- Public-seam tests at the CLI seam (argv, stdout, stderr, exit status, and InvokeAI requests) prove:
  - defaults, explicit values, and flag/document equivalence;
  - every bound: `scale` outside {2, 4, 8}; creativity and structure outside −10…10 or non-integer; non-positive steps; unknown scheduler; guidance below 1 or non-finite; tile size and overlap alignment and range; overlap not less than tile size; an empty selector. Each is rejected before any network request;
  - a non-SDXL or non-`normal` main model → `unsupported_capability`;
  - a shared main-model name → `selection_required`;
  - Spandrel automatic selection, ambiguity, and absence;
  - Tile ControlNet `selection_required` with one and with several SDXL ControlNets, `missing_component` with the starter guidance, and an explicit override accepted;
  - a base-mismatched override → `unsupported_capability`;
  - a missing source image → `not_found` with no enqueue;
  - a single enqueue with no retry, and `outcome_unknown` on an inconclusive enqueue;
  - `--no-wait`, `wait_timeout`, and failed-item receipts;
  - seed and output association;
  - expected output dimensions for aligned and unaligned sources (for example 513 × 513 at scale 2 → 1024 × 1024) at scales 2, 4, and 8;
  - a completed output with mismatched dimensions → `invokeai_operation_failed` with `scale_not_applied` and its details, and never a success receipt;
  - `doctor` matching.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide. First install the consented `RealESRGAN_x4plus` and SDXL `Tile` (`xinsir/controlNet-tile-sdxl-1.0`) starters through Bediz.
  - One waited SDXL upscale of a small existing gallery image (for example 512 × 512 at scale 2, with an explicit seed) completes. Its output dimensions equal the recorded rule's expected dimensions, and its seed matches the queue item.
  - `doctor --json` reports `upscale/sdxl` compatible.
  - The live E2E gate passes with the new case.
- Report any live step that was unavailable, and list the gallery images it created.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer independently compares the fixture's node types, fields, and edges with the stock 6.14.1 Upscale graph builder in the installed frontend bundle. This includes the denoising-start and both ControlNet weight and step formulas at several creativity and structure values, the unsharp-mask settings, tiling and `fp32` flags, SDXL style prompts, the metadata fields, and the VAE wiring.
- The reviewer reruns or inspects the live receipt and checks the output image dimensions and seed against InvokeAI.
- Blocking findings:
  - graph divergence from stock behavior without a recorded reason;
  - any automatic Tile ControlNet selection or name-based guess;
  - a silently ignored or clamped field;
  - validation that runs after the enqueue;
  - a retry or second mutation;
  - a success receipt for a failed item or for mismatched output dimensions;
  - `doctor` advertising a capability whose checks failed.

**Escalate when:** The stock graph needs an input the feature spec does not record. Stock InvokeAI rejects a value that the recorded bounds allow. A small SDXL upscale fails for a reason other than VRAM. A consented stock Spandrel starter produces dimensions other than the recorded rule. The starter installs need a source form or credential the V1 spec does not allow. The receipt shape proves insufficient for Handoff. Repeated repair loops fail.

**Permanent records:** V1 spec and tests. §6 and §13 record the Upscale Request Document, its flags, defaults and bounds, the validation order, the Upscale Component Set rules (including Tile ControlNet candidates without automatic selection and Spandrel output verification), the output-dimension rule and `scale_not_applied` failure, the SDXL graph contract, and the upscale Execution Receipt. §10 records `upscale/sdxl`. `CONTEXT.md` is unchanged, because Generative Upscale, the Upscale Component Set, and the Source Image are already defined. There is no new ADR: ADR 0012 already requires unambiguous component resolution.

**Status:** completed

- [x] `bediz upscale` upscales an existing InvokeAI image with an SDXL model through one tested enqueue, with the recorded defaults, bounds, and components.
- [x] A Tile ControlNet is used only when the caller names it; otherwise structured candidates or a missing-component error are returned.
- [x] A success receipt is returned only when the output dimensions match the recorded rule; otherwise `scale_not_applied` is returned.
- [x] The upscale Execution Receipt and `doctor`'s `upscale/sdxl` entry match the feature spec and the live baseline.

## Comments

- 2026-09-23: `go run ./cmd/bediz models install --source-type starter --source https://github.com/xinntao/Real-ESRGAN/releases/download/v0.1.0/RealESRGAN_x4plus.pth --json` submitted job 16; `models status --job-id 16 --json` reported completed, model key `aa9cc15b-418f-44e2-a36b-8808043c1301`.
- 2026-09-23: `go run ./cmd/bediz models install --source-type starter --source xinsir/controlNet-tile-sdxl-1.0 --json` returned `connection_failed` before mutation because Hugging Face redirects that mixed-case repository path and the existing public-repository preflight does not follow redirects. The same consented public repository was installed through Bediz with `go run ./cmd/bediz models install --source-type huggingface --source xinsir/controlnet-tile-sdxl-1.0 --json`; job 17 completed with model key `4d32de9e-0520-4853-9503-eab819d81532`. This starter-path limitation is separate from upscale behavior.
- 2026-09-23: The first waited live `upscale` of existing `40acb2fc-44fc-447c-ae72-40ce89e9906f.png` (512 × 512, scale 2, steps 4, seed 12345) completed in InvokeAI as queue item 62, but Bediz initially returned `invalid_invokeai_response` because the stock graph emits two intermediate image results before the final image. The wait path now selects exactly one non-intermediate output and still rejects zero or several final outputs. Item 62 created intermediate images `0c6c4ef4-965e-412c-b463-1168b5b5b190.png` and `79385b28-de1f-4050-bec2-2d4eb9068ee8.png`, plus final gallery image `a7c05ad7-e0a7-4ba8-8d57-fa11f88031d8.png`; these ad-hoc images remain in InvokeAI.
- 2026-09-23: Repeating the same waited `upscale` command returned a successful receipt for queue item 63 (batch `6ce30ea2-06e5-4c83-9c20-94a74c239236`), seed 12345, and the cached final image `a7c05ad7-e0a7-4ba8-8d57-fa11f88031d8.png` at 1024 × 1024. Independent read-only inspection of the queue item and Image Reference matched the receipt's item, seed, source, and dimensions.
- 2026-09-23: `go run ./cmd/bediz doctor --json` reported `ready: true` and `upscale/sdxl` compatible, with no failures or upscale UI synchronization level.
- 2026-09-23: `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passed `TestLiveGate`, including a 512 × 512 uploaded source upscale to 1024 × 1024 with seed 45; its source and output were self-cleaned. An initial E2E attempt completed the operation but failed in the new strict test decoder; after adding the missing receipt fields to that decoder, the gate passed. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
