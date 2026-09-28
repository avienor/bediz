# 05: Teach the agent skill and README image-to-image

**What to build:** A Creative Agent using the canonical Bediz skill handles image-to-image requests through `generate` and still declines what Bediz cannot do. People reading the README learn that image-to-image exists.

- **Description:** The skill's trigger `description` no longer excludes img2img. It still excludes inpainting, other image editing, and developing Bediz.
- **Scope:** The skill's scope statement lists image-to-image for Anima, SDXL, and FLUX.1. For inpainting, outpainting, Canvas layers, LoRAs, reference images, and ControlNet-guided generation, it still tells the user to continue in the InvokeAI web interface.
- **Usage:** The skill teaches `generate` with `--image` or `--image-path` and `--strength`, and the Request Document members `source` and `strength`. It explains the choice between an InvokeAI image name and a local file.
- **Creative Discretion:** The agent chooses `strength` when the user did not fix it. Lower values keep more of the source; the default is 0.75. A value the user states is preserved.
- **Dimensions:** Output dimensions follow the source, rounded down to the family alignment. An explicit width and height pair stretches the source to that size. Profile dimensions do not apply to image-to-image.
- **Failures:** The skill decides whether to resubmit from the error code, never from `source_uploaded` alone. `source_uploaded: true` only says a source image now exists. It accompanies every failure after the upload, including an enqueue whose outcome is unknown.
  - For a local `path` source, `outcome_unknown` with `source_uploaded: true` means the enqueue may have happened. The existing Unknown Outcome rule applies: inspect `queue list` and `queue get` before any resubmission.
  - For a local `path` source, `outcome_unknown` without `source_uploaded` concerns the upload itself. Inspect `images list` before uploading again. An existing `image` source has no upload; its enqueue Unknown Outcome still requires inspecting the queue even when `source_uploaded` is absent.
  - `wait_timeout` and `interrupted` mean the items keep running. Resume with `queue wait`, and never resubmit.
  - Only after a conclusive failure following a successful upload, when the user still wants the result, does a resubmission pass `error.details.source_image.image_name` as an `image` source so that the file is not uploaded twice. Examples of conclusive failures: an `invokeai_operation_failed` enqueue rejection, or a failed or canceled item.
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

**Status:** implemented

- [x] The skill routes image-to-image requests to `generate` with a Source Image, applies Creative Discretion to `strength`, and handles upload and synchronization outcomes safely.
- [x] The skill still declines inpainting and other unsupported editing, and the trigger check confirms both behaviors.
- [x] The README describes image-to-image as a supported capability.

## Comments

**2026-09-28, implementation:** The canonical skill now routes text-to-image and image-to-image generation for Anima, SDXL, and FLUX.1. It teaches both source flags and Request Document source kinds, strength discretion and bounds, source-aware dimensions, the upload-first route for WebP/BMP/ICO, error-code-based recovery, and honest source/strength Recall warnings. Readiness checks distinguish the two generation modes. README capability coverage and the Unreleased changelog were updated. `metadata.bediz-version` remains `v1.0.0`; no spec, ADR, glossary, or Go implementation change was needed.

**Recovery wording clarification:** V1 §11.7 and `internal/generation/submit.go` attach `source_uploaded: true` only after a successful local-path upload. An existing `image` source has no upload, so an enqueue Unknown Outcome can omit `source_uploaded` and still requires queue inspection. The ticket and skill qualify the upload-unknown rule by source kind. The recovery behavior remains the existing §17 contract. Failed/canceled batch items also require inspecting sibling items before replacing them.

**Verification:** All of the following passed:

```sh
go test ./internal/cli -run 'TestAgentSkill' -count=1
go test ./...
go test -race ./...
go vet ./...
go mod verify
git diff --check
```

The existing alignment test checks every shown command path and long flag, including both source flags, `--strength`, and the upload-first example. A line-by-line contract check found no graph-building or private InvokeAI payload logic. This documentation slice did not require another live InvokeAI generation; tickets 02–04 retain the family integration evidence.

**Code review:** The `implement` workflow requested a review despite this route having no separate review gate. Two independent reviewers inspected the WIP diff from start-of-task commit `177a8f07a4ad3dd9d4b4d8916321d33f22f0e889`:

- **Standards:** No findings. Canonical terminology, skill-writing rules, and public contracts are followed; no baseline smell warranted a finding.
- **Spec:** No findings. The content covers ticket 05, and the local-path versus existing-image Unknown Outcome distinction matches V1 §11.7 and §17.

**Automated trigger check (2026-09-28):** The original automated record was recovered from commit `ac7b69f4eae82e058251fb06d95d05f00b04bf2c`. Its exact 14 prompts were repeated, followed by six new prompts. Tooling was Claude Code 2.1.283 headless, model `claude-opus-5-5`, with one fresh invocation per prompt and the same two-turn bound. The scratch project held a copy of the final canonical skill and its linked reference. The user's other global skills and synchronized plugin skills remained loaded as distractors.

An initial setup run loaded the outdated personal `bediz` skill instead of the project copy; its results were discarded. The final run used an isolated `CLAUDE_CONFIG_DIR` containing links to the user's global skill directories, excluding only the duplicate personal `bediz`. Each trigger's expanded skill content was checked to name the scratch project's `.claude/skills/bediz` directory. The copied skill matches the repository file byte for byte, SHA-256 `1d518d265f9d55945f134bec2ebc5f39063a41d8a812b334f21909a98115d15e`.

The per-prompt argument list was:

```sh
claude -p '<prompt>' --model claude-opus-5-5 --max-turns 2 \
  --output-format stream-json --verbose --allowedTools Skill \
  --disallowedTools Bash,Write,Edit,MultiEdit,NotebookEdit,WebFetch,WebSearch,Agent,Task \
  --tools Skill,Read --permission-mode dontAsk \
  --strict-mcp-config --mcp-config '{"mcpServers":{}}' --no-session-persistence
```

Only `Skill` and `Read` were exposed, with `Skill` allowed; shell, file-write, web, and agent tools were unavailable. A trigger is an assistant `Skill` tool call with `skill: "bediz"`. Every final run produced a result record and empty standard error. Prompts 4, 9, 16, and 18 ended with `error_max_turns` and process status 1 at the specified two-turn bound; their observed routing is recorded below, without claiming operation completion. The other 16 result records were `success` with process status 0.

**Result:** 20 of 20 routing expectations passed. The original 14 have no regression; all four new image-to-image prompts triggered. Inpainting, both background-removal prompts, and outpainting did not trigger.

| # | Prompt | Expected | Observed | Final result |
| --- | --- | --- | --- | --- |
| 1 | Generate a moody cyberpunk street at night on my InvokeAI | trigger | trigger | success |
| 2 | Bediz ile SDXL modeliyle bir portre üret | trigger | trigger | success |
| 3 | Upscale the last image in my InvokeAI gallery 2x | trigger | trigger | success |
| 4 | Find a good anime FLUX model and install it for InvokeAI | trigger | trigger | error_max_turns |
| 5 | What's stuck in my InvokeAI queue? Cancel it | trigger | trigger | success |
| 6 | Make a board called Moodboard in InvokeAI and put the next renders there | trigger | trigger | success |
| 7 | Save these SDXL settings as a generation profile for Bediz: 30 steps, cfg 6, 1216x832 | trigger | trigger | success |
| 8 | Delete the blurry images from my InvokeAI gallery | trigger | trigger | success |
| 9 | Fix the failing Go test in internal/cli | none | none | error_max_turns |
| 10 | Remove the background from this photo: ~/Pictures/me.jpg | none | none | success |
| 11 | Inpaint the sky in this InvokeAI image | none | none | success |
| 12 | Resize these PNGs to 512px with ImageMagick | none | none | success |
| 13 | Write a Python script that calls the Stable Diffusion API | none | none | success |
| 14 | Generate a logo with DALL-E | none | none | success |
| 15 | Use image-to-image in InvokeAI to turn gallery image source.png into a watercolor landscape while preserving its composition. | trigger | trigger | success |
| 16 | Bediz ile /home/nyx/Pictures/landscape.png dosyasından image-to-image üret; SDXL kullan ve strength 0.35 olsun. | trigger | trigger | error_max_turns |
| 17 | Use Anima image-to-image on my InvokeAI gallery image source.png to make an anime landscape. | trigger | trigger | success |
| 18 | Use FLUX.1 image-to-image in InvokeAI with /home/nyx/Pictures/street.png; keep the layout but make it cyberpunk. | trigger | trigger | error_max_turns |
| 19 | Remove the background from this photo | none | none | success |
| 20 | Outpaint this InvokeAI image to extend its borders | none | none | success |

The local harness, prompt list, raw streams, stderr files, and summaries are under `/tmp/bediz-img2img-skill-gwnnrmr7/`; `run.py`, `prompts.json`, and `results.json` describe the final run. These scratch artifacts are not release files.
