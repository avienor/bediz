# 05: Teach the agent skill and README image-to-image

**What to build:** A Creative Agent using the canonical Bediz skill handles image-to-image requests through `generate` and still declines what Bediz cannot do. People reading the README learn that image-to-image exists.

- **Description:** The skill's trigger `description` no longer excludes img2img. It still excludes inpainting, other image editing, and developing Bediz.
- **Scope:** The skill's scope statement lists image-to-image for Anima, SDXL, and FLUX.1. For inpainting, outpainting, Canvas layers, LoRAs, reference images, and ControlNet-guided generation, it still tells the user to continue in the InvokeAI web interface.
- **Usage:** The skill teaches `generate` with `--image` or `--image-path` and `--strength`, and the Request Document members `source` and `strength`. It explains the choice between an InvokeAI image name and a local file.
- **Creative Discretion:** The agent chooses `strength` when the user did not fix it. Lower values keep more of the source; the default is 0.75. A value the user states is preserved.
- **Dimensions:** Output dimensions follow the source, rounded down to the family alignment. An explicit width and height pair stretches the source to that size. Profile dimensions do not apply to image-to-image.
- **Failures:** The skill decides whether to resubmit from the error code, never from `source_uploaded` alone. `source_uploaded: true` only says a source image now exists. It accompanies every failure after the upload, including an enqueue whose outcome is unknown.
  - `outcome_unknown` with `source_uploaded: true` means the enqueue may have happened. The existing Unknown Outcome rule applies: inspect `queue list` and `queue get` before any resubmission.
  - `outcome_unknown` without `source_uploaded` concerns the upload itself. Inspect `images list` before uploading again.
  - `wait_timeout` and `interrupted` mean the items keep running. Resume with `queue wait`, and never resubmit.
  - Only after a conclusive failure, when the user still wants the result, does a resubmission pass `error.details.source_image.image_name` as an `image` source so that the file is not uploaded twice. Examples of conclusive failures: an `invokeai_operation_failed` enqueue rejection, or a failed or canceled item.
  - A WebP, BMP, or ICO file is uploaded first with `images upload`, and its image name is then passed as an `image` source.
- **Warnings:** The skill reports the image-to-image `ui_sync_partial` fields (`source_image`, `strength`) honestly.
- **README:** The scope sentence no longer lists img2img as missing, and the feature list names image-to-image generation.

**Blocked by:**
- 02: Generate SDXL images from a Source Image.
- 03: Generate Anima images from a Source Image.
- 04: Generate FLUX.1 images from a Source Image.

**Execution route:** `frontier-owned`. The description wording decides which user requests reach Bediz, and the acceptance evidence is a sampled trigger check rather than a deterministic test.

**Verification gate:**
- The skill alignment test passes with every new command path and long flag the skill shows.
- The skill contains no graph-building or payload logic.
- The skill's failure guidance matches the V1 spec and the existing Unknown Outcome table. No text suggests resubmitting after `outcome_unknown`, `wait_timeout`, or `interrupted` without inspecting first.
- The automated trigger check recorded on 2026-09-24 for the canonical skill is repeated with the same method:
  - a headless agent run in a scratch project holding a copy of the changed skill, with the user's global skills loaded as distractors;
  - one run per prompt, `Skill` allowed, and shell, file-write, web, and agent tools disallowed;
  - a trigger is a `Skill` call naming `bediz`.

  The check runs the original 14 prompts plus new prompts. At least two image-to-image prompts about an InvokeAI image or a local file are expected to trigger. "Inpaint the sky in this InvokeAI image" and "Remove the background from this photo" are still expected not to trigger. Record the tooling, prompts, and results in the ticket comments.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Not required by this route.

**Escalate when:**
- The trigger check shows a regression on any of the original 14 prompts.
- Inpainting or background-removal prompts start to trigger the skill.
- Teaching image-to-image would require the skill to state behavior the V1 spec does not record.
- The retry guidance cannot be stated without contradicting the Unknown Outcome rule.

**Permanent records:**
- The agent skill.
- `README.md`.
- `CHANGELOG.md` under Unreleased, recording the skill change.
- No V1 spec, ADR, or glossary change: the behavior is recorded by tickets 02–04. The skill's `metadata.bediz-version` changes only at release time, as V1 spec §19 defines.

**Status:** ready-for-agent

- [ ] The skill routes image-to-image requests to `generate` with a Source Image, applies Creative Discretion to `strength`, and handles upload and synchronization outcomes safely.
- [ ] The skill still declines inpainting and other unsupported editing, and the trigger check confirms both behaviors.
- [ ] The README describes image-to-image as a supported capability.
