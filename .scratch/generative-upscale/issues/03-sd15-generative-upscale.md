# 03: Upscale with an SD1.5 model

**What to build:** Register SD1.5 as an upscale family so that `bediz upscale` accepts an installed main model with base `sd-1`, type `main`, and variant `normal`, in any format InvokeAI's SD1.5 loader accepts. The request, bounds, defaults, receipt, and execution rules are unchanged from ticket 02. The differences, recorded in the `generative-upscale` feature spec, are:

- **Graph:** The stock InvokeAI 6.14.1 Upscale graph with the SD1.5 branch: `main_model_loader`, a `clip_skip` node with 0 skipped layers feeding positive and negative `compel` nodes, and the loader's UNet and VAE. There are no style prompts. Clip skip is not a public setting.
- **Components:** The Tile ControlNet candidates are ControlNets with base `sd-1`. The missing-component guidance names the `lllyasviel/control_v11f1e_sd15_tile` starter source. The VAE override requires base `sd-1`. An SDXL ControlNet or VAE selected for an SD1.5 main model, or the reverse, is `unsupported_capability`.
- **Doctor:** `doctor` registers `upscale/sd-1` with its invocation vocabulary and installed-model requirements (an SD1.5 `normal` main model, a Spandrel model, and an SD1.5 ControlNet). The live E2E gate gains a self-cleaning SD1.5 upscale case.
- **Unchanged:** SD1.5 does not become a `generate` family, and standalone `recall` still rejects SD1.5 main models.

**Blocked by:** 02: Upscale an existing InvokeAI image with an SDXL model.

**Execution route:** `worker + independent review` — a second family branch of a graph already reviewed in ticket 02, fully specified, and detectable by its fixture and a live upscale.

**Verification gate:**
- A versioned SD1.5 upscale enqueue fixture for InvokeAI 6.14.1 covers the bundled VAE and a VAE override. It is checked against the live OpenAPI invocation vocabulary.
- Public-seam tests prove:
  - SD1.5 main-model acceptance;
  - non-`normal` SD1.5 variants → `unsupported_capability`;
  - SD1.5 Tile ControlNet candidates and the missing-component guidance;
  - cross-base component rejection in both directions;
  - `generate` and `recall` still rejecting SD1.5 models;
  - `doctor` matching.
- The SDXL upscale fixture and all generation fixtures are byte-identical.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide. First install the consented `Dreamshaper 8` (`Lykon/dreamshaper-8`) and SD1.5 `Tile` (`lllyasviel/control_v11f1e_sd15_tile`) starters through Bediz.
  - One waited SD1.5 upscale of a small existing gallery image completes with the expected dimensions and seed.
  - `doctor --json` reports `upscale/sd-1` compatible.
  - The live E2E gate passes with the new case.
- Report any live step that was unavailable, and list the gallery images it created.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer independently compares the SD1.5 fixture with the stock builder's SD1.5 branch: loader, clip skip, compel wiring, metadata, and VAE source.
- The reviewer confirms that family-specific knowledge stays in the upscale family registration and that the SDXL behavior is unchanged.
- Blocking findings: graph divergence from stock behavior without a recorded reason; mixing SD1.5 and SDXL components; SD1.5 leaking into `generate` or `recall`; `doctor` advertising a capability whose checks failed.

**Escalate when:** The installed SD1.5 starter records a variant or format the feature spec does not cover. The stock SD1.5 branch needs inputs the spec does not record. The starter install needs a source form or credential the V1 spec does not allow. Repeated repair loops fail.

**Permanent records:** V1 spec and tests. §13 records SD1.5 upscale acceptance, the SD1.5 graph branch, and SD1.5 component bases. §10 records `upscale/sd-1`. No new terminology or ADR.

**Status:** completed

- [x] `bediz upscale` upscales with an SD1.5 `normal` main model and SD1.5 components through the stock SD1.5 graph branch.
- [x] `doctor` advertises `upscale/sd-1` only when its endpoints, invocations, and installed models are present on a supported version.

## Comments

- 2026-09-23: `go run ./cmd/bediz models install --source-type starter --source Lykon/dreamshaper-8 --json` submitted job 18; `models status --job-id 18 --json` reported completed. `dreamshaper-8` is key `5743f3c4-4418-4556-af5e-75eac3ba4f2d`, base `sd-1`, variant `normal`, format `diffusers`, which the feature spec covers.
- 2026-09-23: `go run ./cmd/bediz models install --source-type starter --source lllyasviel/control_v11f1e_sd15_tile --json` submitted job 19; it completed with model key `c4adad50-9ce1-41fa-9caf-6bfc3c92fde4` (`control_v11f1e_sd15_tile`, base `sd-1`, format `diffusers`).
- 2026-09-23: `go run ./cmd/bediz doctor --json` reported `ready: true` and both `upscale/sdxl` and `upscale/sd-1` compatible with no failures, confirming the SD1.5 invocation vocabulary (`main_model_loader`, `clip_skip`, `compel`) against the live OpenAPI document.
- 2026-09-23: A waited `upscale` of existing `40acb2fc-44fc-447c-ae72-40ce89e9906f.png` (512 × 512) with Dreamshaper 8, the SD1.5 Tile ControlNet, scale 2, steps 10, tile size 512, and seed 12345 returned a successful receipt for queue item 71 (batch `c4752943-4140-4963-92e5-f60c00aa4a90`) with final gallery image `d347ff33-9d6e-4ffb-bafc-8091424d6e07.png` at 1024 × 1024. Independent read-only inspection of item 71 showed status `completed` and batch seed 12345. The Spandrel and unsharp-mask intermediate results reused the cached images from ticket 02's item 62. The ad-hoc output image remains in InvokeAI.
- 2026-09-23: `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passed `TestLiveGate`, including the new self-cleaning SD1.5 upscale case (seed 46) alongside the SDXL case; its uploaded source and output were deleted. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
- 2026-09-23: The stock SD1.5 builder omits `skipped_layers` on `clip_skip` and relies on its default of 0; Bediz sends 0 explicitly, which is the same graph behavior and matches the recorded "0 skipped layers".
