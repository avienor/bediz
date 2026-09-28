# Changelog

Each release lists the changes that affect users of the `bediz` binary, its JSON contract, the install scripts, or the agent skill. [docs/spec/v1.md](docs/spec/v1.md) describes the resulting behavior in full; section 25 defines which changes a release may make.

## Unreleased

### Added

- SDXL image-to-image generation through `generate --image` or `--image-path`, with `--strength`, source-aware dimensions, an Execution Receipt, and a separate `doctor` mode.
- Anima image-to-image generation through the same Source Image contract, with its tested encoder graph and a separate `doctor` mode.
- FLUX.1 dev and schnell image-to-image generation through the same Source Image contract, with optimized Denoising Strength, 16-pixel source alignment, and a separate `doctor` mode.
- `invalid_request` errors name the Request Document member in `error.details.field` when the decoder or validator knows it, for flags as well as documents.
- `invokeai_operation_failed` may include a sanitized, size-bounded `error.details.invokeai_detail` for InvokeAI 4xx rejections.
- `bediz config set --token-stdin` stores the InvokeAI token from standard input, keeping it out of process arguments and shell history.

### Changed

- The agent skill teaches image-to-image generation for Anima, SDXL, and FLUX.1, including Source Images, Denoising Strength, dimensions, safe failure recovery, and partial UI Synchronization; the README lists it as supported.
- `images upload` and upscale `path` sources reject files whose content is not an image before any network request.
- A source token is refused for a non-loopback plain-HTTP artifact URL: `invalid_request` for a direct URL, `unsupported_capability` for a starter entry.
- `doctor` omits `openapi.required_endpoints` and `openapi.required_invocations` when OpenAPI cannot be read, instead of listing every check as missing.
- The install scripts open the skill installer's own agent selection instead of installing the skill for a fixed agent. The scripts run from the default branch, so this is already in effect.

## v1.0.0 — 2026-09-28

First stable release, targeting InvokeAI `>= 6.14.1, < 6.15.0`.

- Text-to-image generation for Anima, SDXL, and FLUX.1, with Execution Receipts and partial UI Synchronization through Parameter Recall.
- Tiled generative upscale for SD1.5 and SDXL from an InvokeAI image or a local file.
- Model installation from InvokeAI starter models, Hugging Face, direct URLs, server paths, and Civitai, plus model status, scan, and deletion.
- Image, queue, and board management, and local Generation Profiles.
- `doctor` readiness report with the Capability Matrix.
- The canonical agent skill, install scripts, and reproducible release archives for Linux amd64, macOS arm64, and Windows amd64.
