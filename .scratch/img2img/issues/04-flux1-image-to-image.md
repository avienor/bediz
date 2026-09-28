# 04: Generate FLUX.1 images from a Source Image

**What to build:** `generate` performs image-to-image generation for FLUX.1 main models under the contract ticket 02 established. The feature spec (`.scratch/img2img/spec.md`) and the V1 spec image-to-image subsection hold the shared rules. FLUX.1-specific behavior:

- **Accepted models:** Exactly the text-to-image set: variant `dev` or `schnell` in format `checkpoint`, `bnb_quantized_nf4b`, or `gguf_quantized`.
  - Kontext dev and Krea dev report variant `dev` and are accepted by variant. Bediz does not replicate the stock frontend's display-name block on Kontext.
  - `dev_fill`, unknown variants, SDNQ, and ordinary `diffusers` stay `unsupported_capability`.
  - A non-empty negative prompt, or guidance for schnell, stays `invalid_request`.
- **Strength:** It follows InvokeAI 6.14.1's default "optimized denoising": `denoising_start = 1 − strength^0.2` and `denoising_end = 1`. For example, strength 0.75 gives about 0.0559 and strength 0.5 about 0.1294. The receipt reports the public `strength`, not `denoising_start`.
- **Graph:** The tested FLUX.1 text-to-image topology, plus:
  - `flux_vae_encode`, fed by the source image or by an intermediate `img_resize` (`resample_mode: "bicubic"`) when the resolved dimensions differ from the source;
  - the `flux_model_loader` VAE feeding both `flux_vae_encode` and `flux_vae_decode`;
  - the encoded latents into `flux_denoise.latents`, with `add_noise: true`;
  - `flux_denoise` width and height at the resolved dimensions;
  - `core_metadata` with `generation_mode: "flux_img2img"`, `strength`, and `init_image`.
- **Dimensions:** Unless an explicit pair is given, source dimensions are rounded down to a multiple of 16. A source smaller than 16 pixels in either dimension is `invalid_request` before any upload or enqueue.
- **UI Synchronization:** The Recall patch is the FLUX.1 text-to-image patch. `ui_sync_partial.not_restored` is the FLUX.1 list (`scheduler`, `guidance` for dev only, `vae`, `t5_encoder`, `clip_embed`, `output_count`, `board_id`) followed by `source_image` and `strength`.
- **Doctor:** A `generate`/`flux` entry with `mode: "img2img"` requires:
  - the FLUX.1 text-to-image requirements, including its variant- and format-filtered main-model requirement;
  - the image inspection and upload endpoints;
  - `ImageResizeInvocation` (`img_resize`) with `image`, `width`, `height`, and `resample_mode`;
  - `FluxVaeEncodeInvocation` (`flux_vae_encode`) with `image` and `vae`;
  - `FluxDenoiseInvocation` with its text-to-image properties plus `latents`, `denoising_start`, `denoising_end`, and `add_noise`;
  - `CoreMetadataInvocation` with its text-to-image properties plus `strength` and `init_image`.

  Each schema also keeps the common `id`, `is_intermediate`, `use_cache`, and `type` properties. `generate` checks the same requirements before any upload or enqueue. The entry carries `ui_sync: partial` only when Recall is compatible. The text-to-image `generate/flux` entry does not gain the three denoising properties in this ticket; that pre-existing gap is out of scope.

**Blocked by:** 02: Generate SDXL images from a Source Image. It is independent of ticket 03 and may run in parallel with it.

**Execution route:** `worker + independent review`. The strength mapping and the Kontext policy were decided with the user on 2026-09-28. The remaining work is one family adapter whose correctness fixtures, computed values, and live runs can detect.

**Verification gate:**
- Versioned InvokeAI 6.14.1 FLUX.1 image-to-image enqueue fixtures exist for dev and schnell, with and without resize, and are checked against the live OpenAPI invocation vocabulary. They pin `denoising_start` for representative strengths, including 1, 0.75, and a small value.
- Public-seam tests prove:
  - 16-pixel dimension rounding, and a too-small source rejected before any upload or enqueue;
  - that each missing FLUX.1 image-to-image schema or property listed above fails `generate` as `unsupported_capability` before any upload, and marks the `doctor` entry incompatible with the property named, while the FLUX.1 text-to-image entry stays compatible;
  - `dev_fill`, unknown variant, and SDNQ rejection before any upload;
  - that Kontext-named dev models are accepted by variant;
  - that negative prompts and schnell guidance are still rejected;
  - the FLUX.1 `not_restored` list for dev and schnell;
  - the `doctor` FLUX.1 image-to-image entry's matching and failures, including an inventory with only `dev_fill` or SDNQ main models;
  - that FLUX.1 text-to-image fixtures and receipts are unchanged.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live checks against the local InvokeAI 6.14.1 baseline with the installed FLUX.1 dev and schnell NF4 models and their components (no installation needed), following the live-verification guide:
  - One waited 768 × 768 schnell image-to-image and one waited dev image-to-image produce receipts whose images carry the matching seeds and `flux_img2img` metadata.
  - `doctor --json` reports the entry as compatible.
  - The live E2E gate (`BEDIZ_E2E_URL` set) passes with the entry registered and covers it as the guide requires.
- Report unavailable live steps, including 8 GB VRAM limits, and list ad hoc images.

**Review gate:**
- A separate reviewer reviews a fixed base/head diff, starting from this ticket, the feature spec, the V1 spec image-to-image subsection, and the worker's evidence. The reviewer must be able to read the InvokeAI 6.14.1 invocation schemas and the built frontend bundle.
- Before any fix, the reviewer:
  - recomputes `denoising_start` for the pinned strengths from the frontend's formula;
  - compares the fixtures against the stock frontend's FLUX image-to-image branch;
  - independently reruns one live schnell image-to-image;
  - checks the public tests against the spec.
- Blocking findings: a formula or rounding mismatch, any display-name-based model inference, acceptance of `dev_fill` or another unsupported variant or format, a changed text-to-image fixture or receipt, or unreproducible live evidence.

**Escalate when:**
- The Go and frontend computations of `strength^0.2` disagree for a pinned value.
- `flux_vae_encode` or `flux_denoise` needs inputs the spec does not record.
- A Kontext or Krea model fails image-to-image in a way that suggests a product decision.
- The baseline hardware cannot complete a generation and the failure looks like a graph defect rather than a resource limit.
- Repeated repair loops fail.

**Permanent records:**
- V1 spec: the image-to-image subsection's FLUX.1 row with the strength formula and the Kontext rule, the §12 FLUX.1 `not_restored` list, and the §10 FLUX.1 image-to-image entry.
- `CHANGELOG.md` under Unreleased. Tests.
- No ADR or glossary change: ADR-0023 records the strength meaning.

**Status:** implemented

- [x] `generate` with a supported FLUX.1 dev or schnell main model and a Source Image produces image-to-image outputs through one tested enqueue, using the recorded strength mapping.
- [x] Unsupported variants and formats fail before any upload, and no model is classified by display name.
- [x] Doctor, `ui_sync_partial`, the V1 spec, and `CHANGELOG.md` reflect the behavior verified live.
- [x] The independent review found no acceptance-blocking issue.

## Comments

### 2026-09-28 — Implementation and worker evidence

- Public seams: `generation.CompileFLUX`, the CLI's argv/stdout/stderr/exit status and HTTP boundary, and `doctor` output. Compiler, generation acceptance, and synchronization tests were observed failing before their implementations. The existing shared Source Image path remains the upload and waiting boundary.
- Four versioned image-to-image fixtures cover dev and schnell, each with and without resize. The built 6.14.1 frontend (`index-BRHi9LIu.js`) records optimized denoising enabled by default and `1-n**(t?.2:1)`. Frontend results pinned in the fixtures are: strength 1 → 0; 0.75 → 0.05591248870509802; 0.5 → 0.12944943670387588; 0.01 → 0.6018928294465027.
- The Go result at strength 0.01 is 0.6018928294465028, one ULP from the frontend. Escalated to the user, who approved an absolute tolerance of 1e-15 for fixture comparisons of `denoising_start` only. Production retains `1 - math.Pow(strength, 0.2)`; every other fixture member is compared exactly.
- Checked all eight FLUX text/image enqueue fixtures' node fields and edge vocabulary against live `/openapi.json`. The fixture projection adds `FluxVaeEncodeInvocation` and `ImageOutput` from that document. The four text-to-image fixture files are unchanged.
- CLI tests cover all supported variant/format pairs, Kontext and Krea names, unsupported variants/formats, negative prompts, schnell guidance, small sources on either axis and for either source kind, source rounding, explicit dimensions, flags/documents, profile dimension precedence, component selection, no-resize sources, and the dev/schnell warning lists. Missing schemas and every required invocation property stop generation before upload; doctor names the requirement and preserves text-to-image compatibility for image-only requirements. Recall absence does not remove Direct Execution compatibility. Text-to-image receipts omit the Source Image members and strength.
- Local baseline: `curl -fsS http://127.0.0.1:9090/api/v1/app/version` → 6.14.1; RTX 4060, 8188 MiB VRAM; queue initially idle. Installed NF4 dev/schnell, FLUX VAE, T5 int8, and CLIP Embed were used without installation.
- Built with `go build -o /tmp/bediz-flux1-img2img-ijZbe7/bediz ./cmd/bediz`. Test source `/tmp/bediz-flux1-img2img-ijZbe7/source.png` is a 768 × 768 RGB PNG.
- Waited schnell: `/tmp/bediz-flux1-img2img-ijZbe7/bediz generate --model 385ce753-8fb8-46fc-ab74-aac4924a9663 --prompt 'a small red teacup on a softly lit table' --image-path /tmp/bediz-flux1-img2img-ijZbe7/source.png --strength 0.75 --seed 4040 --timeout 15m --json` → success, 768 × 768, 4 steps, queue item 129, batch `054f69cd-24aa-4496-8c95-8ddc087201b5`. Metadata verified through `GET /api/v1/images/i/7c813dbc-2165-4b8b-9617-02d8d5eb4688.png/metadata`: seed 4040, `flux_img2img`, strength 0.75, matching `init_image`.
- Waited dev: `/tmp/bediz-flux1-img2img-ijZbe7/bediz generate --model 6b6e6ba3-368f-42ee-b6e5-033bba37aa7b --prompt 'a small red teacup on a softly lit table' --image-path /tmp/bediz-flux1-img2img-ijZbe7/source.png --strength 0.75 --steps 4 --seed 4041 --timeout 15m --json` → success, 768 × 768, guidance 4, queue item 130, batch `02b494b3-3608-46a5-a800-25523c282099`. Metadata verified through `GET /api/v1/images/i/e9f43dc9-f972-4817-82b8-fbc167bc6f62.png/metadata`: seed 4041, `flux_img2img`, strength 0.75, matching `init_image`.
- `/tmp/bediz-flux1-img2img-ijZbe7/bediz doctor --json` → ready, both FLUX modes compatible with `ui_sync: partial`. Receipts, metadata, OpenAPI, and verification logs are under `/tmp/bediz-flux1-img2img-ijZbe7/` for the independent reviewer.
- Ad hoc gallery images retained: schnell uploaded source `1f994ce9-befd-4237-8c13-0f5678007aa0.png`, schnell output `7c813dbc-2165-4b8b-9617-02d8d5eb4688.png`, dev uploaded source `9d76308f-6c78-4cd0-b1ec-3255b5607d85.png`, dev output `e9f43dc9-f972-4817-82b8-fbc167bc6f62.png`.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass. The first full test pass exposed a stale doctor capability/invocation count expectation; it was updated for the sixth generation mode and the FLUX encoder before the successful full rerun.
- `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e` passes (203.755 seconds), including the registered FLUX image mode, a waited schnell image-to-image, seed/metadata checks, and cleanup of the E2E-created images. All required live steps completed on the 8 GB baseline.

### Independent Standards review

Fixed base `97178289fe902388d04c804a813c37aac7454caa`, review snapshot `c1fbc4b399dc69d4919bae7bb6ffc4451d155e10` (created without moving the branch).

No documented-standard breaches or material baseline smells found. Tests use public compiler, CLI, doctor, and HTTP boundaries; request counts observe external mutations. Fixture expectations come from the frontend and the approved tolerance is confined to `denoising_start`. The CLI adapter seam, shared Source Image path, deterministic model resolution, and family conventions are preserved. Go 1.27 guidance was checked. Standards: **0 findings**.

### Independent Spec review

Same fixed base and review snapshot. **0 Spec findings; no acceptance blocker.**

The reviewer independently checked the installed 6.14.1 frontend's optimized-denoising default and formula, recomputed all four pinned values, and compared the encoder/VAE/latent edges, dimensions, resize behavior, and metadata to the agreed frontend image-to-image path. All eight FLUX fixtures passed against live OpenAPI node and edge vocabulary; the four text-to-image fixture files are unchanged. Public tests cover the requested schema/property failures, rounding, small sources, model acceptance/rejection, receipts, and variant-specific synchronization lists. The approved tolerance affects only fixture `denoising_start`.

Independent public tests passed:

```sh
go test ./internal/generation ./internal/cli -run 'FLUX' -count=1
```

Independent waited generation passed with exit 0 and empty stderr:

```sh
/tmp/bediz-flux1-img2img-ijZbe7/bediz generate --model 385ce753-8fb8-46fc-ab74-aac4924a9663 --prompt 'a small red teacup on a softly lit table' --image 1f994ce9-befd-4237-8c13-0f5678007aa0.png --strength 0.75 --seed 4042 --timeout 15m --url http://127.0.0.1:9090 --json
```

Item 139, batch `e740673c-beee-479d-aa17-ada24f687f3f`, produced ad hoc gallery image `00438c95-4667-46f9-add9-aa77f9bdfde3.png`, retained in addition to the four worker images listed above. Exact receipt assertions passed; direct HTTP metadata confirms seed 4042, `flux_img2img`, strength 0.75, and matching `init_image`. Independent `doctor` reports both FLUX modes compatible with partial UI Synchronization. Evidence files `reviewer-schnell-{receipt,stderr,metadata,queue}` and `reviewer-doctor.json` are in the worker evidence directory.

Review totals: Standards **0**, Spec **0**; neither axis has a blocking finding. After the fixed review snapshot, only this ticket's verification evidence and completion status changed.
