---
name: bediz
description: Drive a local InvokeAI through the Bediz CLI. Use when the user wants text-to-image or image-to-image (img2img) generation, including with LoRAs, or upscale with InvokeAI or Bediz, models found or installed for InvokeAI, or its queue, gallery, boards, or generation profiles inspected or managed. Not for LoRA training, inpainting or other image editing, or for developing Bediz itself.
metadata:
  bediz-version: v1.1.0
---

# Bediz

Bediz is a deterministic controller for a local InvokeAI installation. You are the **Creative Agent**: you turn the user's intent into prompts, a model, and settings. Bediz validates those explicit choices, runs them on InvokeAI, and returns a **Result Envelope**. The human watches the same queue and gallery in the InvokeAI web interface and can take the work over there (**Handoff**).

Bediz never builds prompts, picks creative settings, or guesses between candidates. You never build execution graphs or call InvokeAI's API yourself: every action goes through a `bediz` command.

Bediz covers text-to-image and image-to-image generation for Anima, SDXL, and FLUX.1, including LoRAs in both **Generation Modes**, generative upscale, and the model, gallery, queue, board, and profile commands below. For inpainting, outpainting, Canvas layers, reference images, ControlNet-guided generation, or LoRAs in upscale, tell the user Bediz cannot do it and that they can continue in the InvokeAI web interface. Bediz cannot train LoRAs.

## Every call

- Add `--json`. Standard output then holds exactly one Result Envelope; standard error holds diagnostics only.
- Branch on the envelope, not on prose: `ok`, then `data` on success or `error.code`, `error.message`, and `error.details` on failure. Also read `warnings` on success.
- Send operation inputs as a **Request Document** on standard input with `--request -`. It avoids shell quoting, and it compiles to the same operation as the flags. Operation flags and `--request` never mix; `--json`, `--url`, `--no-wait`, `--timeout`, and `--yes` may accompany it.
- Every Request Document carries `"schema_version": 1`. Unknown, misspelled, `null`, or inapplicable fields are rejected, so send only fields you mean. The command's `--help` lists its flags.
- Bediz connects to `http://127.0.0.1:9090` unless `--url`, `BEDIZ_URL`, or `bediz config set --url` says otherwise.
- For InvokeAI authentication, use an existing `BEDIZ_TOKEN` or stored configuration. When setup is needed, ask the user to run `bediz config set --token-stdin` in their terminal with the token on standard input. Keep tokens out of process arguments, commands you show, files you write, and replies.

| Exit | Meaning | Your move |
| --- | --- | --- |
| 0 | Success | Read `data` and `warnings`. |
| 2 | Invalid request | Use `error.details.field` when present; otherwise inspect the other details and `error.message`. Ask the user before changing a field they fixed. |
| 3 | Selection required | See [Selection Required](#selection-required). |
| 4 | Unsupported capability or missing component | Pick a supported family or model, or install the component (see [Models](#models)). |
| 5 | Connection or authentication failure | Check the target with `bediz config get --json`; ask the user to start InvokeAI or supply access. |
| 6 | InvokeAI operation failure | On `outcome_unknown`, see [Unknown Outcome](#unknown-outcome); otherwise report the error. |
| 130 | Interrupted locally | InvokeAI work keeps running; inspect the queue. |

## Workflow

1. **Readiness.** When you do not yet know what this installation supports (first use in a session, after an exit status 4 or 5, or after the user changes InvokeAI), run `bediz doctor --json`. On success the report is in `data`; when `ok` is `false` (for example, when a model is missing), the same report is in `error.details.report`. Its `capabilities` field is the **Capability Matrix** for this installation: each entry has `operation`, an optional `family`, `compatible`, and `failures`. Generation Mode rows have `mode`: `txt2img` or `img2img`. Each generation family also has a `setting: "loras"` row, with no `mode` or `ui_sync`; it checks LoRA support without requiring an installed LoRA. A LoRA request needs both its family's LoRA row and its Generation Mode row to be compatible. Request only compatible operation, family, and Generation Mode combinations. For `models install`, check the generic row plus the applicable `family` row (`starter`, `huggingface`, or `path`), and also `source_token` when supplying a Source Token. A failure naming `missing_component:<name>` means a model is absent; the report's `issues` carries the details. `ui_sync` tells you how much of each operation the InvokeAI web interface can restore. Compare the report's `bediz.version` with this skill's `metadata.bediz-version`; when they differ, tell the user this skill may not match their Bediz.
2. **Model.** Run `bediz models list --json`, narrowing with `--base` or `--type` when the list is long. Select models by their exact `key` (the **Model Key**). Prefer an installed model that fits the request. When none fits, go to [Models](#models).
3. **Request.** Write the Request Document under [Creative Discretion](#creative-discretion), using the [family rules](#families).
4. **Run.** `generate` and `upscale` wait for completion by default. For long jobs, add `--no-wait`, then `bediz queue wait ITEM_ID --json` with the returned `data.queue.item_ids`.
5. **Report.** The success `data` is the **Execution Receipt**: `resolved_settings` (with `model_key`, `component_keys`, and the ordered `seeds`), `queue`, and `outputs`, each with `item_id`, `seed`, and an `image` whose `image_name`, `image_url`, and `thumbnail_url` identify the result. Give the user the image and the settings that matter to them, and report every warning (see [UI Synchronization](#ui-synchronization)). Images stay in InvokeAI; save a local copy only when asked, with `bediz images download IMAGE_NAME --output PATH --json`.

## Creative Discretion

You may improve prompts and choose every model and setting the user left open without asking about each minor choice. Every prompt, model, and setting the user fixed explicitly is sent exactly as given.

- A prompt the user wrote as the prompt is fixed: send it verbatim. A description of what they want is intent: write the prompt yourself.
- A named model or LoRA, LoRA Weight, size, seed, step count, scheduler, guidance, Denoising Strength, output count, or board is fixed.
- For image-to-image, choose `strength` when the user left it open. Lower values retain more of the Source Image; the valid range is `0 < strength ≤ 1`, and omission defaults to 0.75. Preserve a strength the user stated exactly.
- Omitting a technical field is a valid choice: the Generation Profile, then the model family's default, fills it, except for the image-to-image dimensions and strength described here.
- When Bediz rejects a fixed value, tell the user why and ask; keep the rest of the request unchanged. Substituting a different value yourself overrides the user.

## Families

`generate` supports the bases `anima`, `sdxl`, and `flux` (FLUX.1). `upscale` supports main models with base `sdxl` or `sd-1` and variant `normal`. Other bases return `unsupported_capability`.

| Family | Negative prompt | `guidance` | Dimension multiple | `components` |
| --- | --- | --- | --- | --- |
| Anima | yes | at least 1 | 8 | `vae`, `qwen3_encoder` |
| SDXL | yes | CFG scale, at least 1 | 8 | `vae` |
| FLUX.1 dev | no | at least 1 | 16 | `vae`, `t5_encoder`, `clip_embed` |
| FLUX.1 schnell | no | not accepted | 16 | `vae`, `t5_encoder`, `clip_embed` |

Width and height are sent together or not at all. A scheduler outside the family's list is rejected; omit it unless the user named one.

### Generate

```sh
bediz generate --request - --json <<'EOF'
{"schema_version": 1, "model": "MODEL_KEY", "positive_prompt": "...", "negative_prompt": "...", "width": 1216, "height": 832, "steps": 30, "guidance": 5, "seed": 7, "output_count": 2, "board_id": "BOARD_ID"}
EOF
```

The other generation fields are `profile`, `scheduler`, and `components` (for example `{"vae": "MODEL_KEY"}`). Without a seed, each output gets a random one; with a seed, later outputs count up from it.

#### Image-to-image

Use the same `generate` command with a **Source Image**. An InvokeAI image name reuses an image already in its gallery; an absolute local path uploads the file once. A source selects image-to-image; omitting it selects text-to-image, where `strength` is invalid.

```sh
bediz generate --model MODEL_KEY --prompt 'watercolor landscape' --image IMAGE_NAME --strength 0.35 --json
bediz generate --model MODEL_KEY --prompt 'watercolor landscape' --image-path /absolute/path/source.png --strength 0.35 --json
```

Use exactly one of `--image` and `--image-path`. The equivalent Request Document adds `source` and optional `strength`:

```sh
bediz generate --request - --json <<'EOF'
{"schema_version": 1, "model": "MODEL_KEY", "positive_prompt": "watercolor landscape", "source": {"type": "image", "reference": "IMAGE_NAME"}, "strength": 0.35}
EOF
```

For a local file, use `{"type": "path", "reference": "/absolute/path/source.png"}` as `source`. Direct path sources support PNG, JPEG, and GIF. For WebP, BMP, or ICO, first run `bediz images upload /absolute/path/source.webp --json`, then pass the returned image's `image_name` as an `image` source.

Without an explicit width and height pair, output dimensions follow the source, each rounded down to the family alignment in the table above. An explicit pair stretches the source to that exact size. Generation Profile dimensions do not apply to image-to-image. The Execution Receipt records `source_image`, `source_uploaded`, the resolved dimensions, and `resolved_settings.strength`.

#### LoRAs

Use `generate` for LoRAs with Anima, SDXL, or FLUX.1 in either Generation Mode. Find compatible installed LoRAs with `bediz models list --type lora --base FAMILY_BASE --json`, replacing `FAMILY_BASE` with the main model's exact `base`: `anima`, `sdxl`, or `flux`. A LoRA must have `type: "lora"` and the same base as the main model. Use the inventory's `trigger_phrases` and `default_weight` when present; otherwise consult the model description for usage guidance.

Choose LoRAs and weights under Creative Discretion when the user left them open; preserve every LoRA and weight they stated. When you write or improve a prompt, include the trigger phrases of each applied LoRA. When the user fixed the prompt verbatim, send it unchanged: tell them which trigger phrases are missing and that the LoRA's effect may be weaker without them.

The repeatable flag is `--lora <selector>[=<weight>]`. Each selector is a Model Key or unique name; flags apply in the order given. Prefer Model Keys when a name is ambiguous or contains `=`, because the flag splits at the last `=`.

```sh
bediz generate --model MODEL_KEY --prompt 'alienstyle portrait, fine detail' --lora STYLE_LORA_KEY --lora DETAIL_LORA_KEY=0.6 --json
```

The equivalent Request Document uses an ordered `loras` array with `model` and optional `weight`:

```sh
bediz generate --request - --json <<'EOF'
{"schema_version": 1, "model": "MODEL_KEY", "positive_prompt": "alienstyle portrait, fine detail", "loras": [{"model": "STYLE_LORA_KEY"}, {"model": "DETAIL_LORA_KEY", "weight": 0.6}]}
EOF
```

For image-to-image, add the same `source` and optional `strength` described above. Omit `loras` when using none; an empty array or a repeated resolved Model Key is invalid. An omitted **LoRA Weight** uses the LoRA's recorded default, otherwise 0.75. Valid weights are finite numbers from −10 to 10 inclusive; zero removes the effect and negative weights invert it.

A missing LoRA follows [Installation Consent](installing-models.md#installation-consent): report its source, approximate size, and known license (or that it is unknown), and install only after consent unless the user asked to install or use that LoRA. Confirm the installed model's type and base before generation.

Correct failures from the structured error:

- `selection_required` with `error.details.kind: "lora"`: use [Selection Required](#selection-required) to choose an exact candidate Model Key and resubmit.
- `unsupported_capability` for a wrong LoRA base or type: select a `lora` with the main model's base; ask before replacing a LoRA or main model the user fixed.
- `invalid_request` with `error.details.field` naming `loras`, `loras.0.model`, or `loras.0.weight` (indexes are zero-based): correct the named list, selector, or weight. An invalid recorded default needs an explicit valid weight; ask before changing a fixed value.

### Upscale

```sh
bediz upscale --request - --json <<'EOF'
{"schema_version": 1, "source": {"type": "image", "reference": "IMAGE_NAME"}, "model": "MODEL_KEY", "positive_prompt": "...", "scale": 2, "components": {"tile_controlnet": "MODEL_KEY"}}
EOF
```

- `source` is an InvokeAI image (`"type": "image"`) or an absolute file on this machine (`"type": "path"`), which Bediz uploads once.
- The other upscale fields are `profile`, `negative_prompt`, `creativity` and `structure` (−10 to 10), `steps`, `scheduler`, `guidance`, `seed`, `tile_size`, `tile_overlap`, `board_id`, and `components.upscale_model` and `components.vae`.
- Bediz never infers which ControlNet is a Tile ControlNet. Without `components.tile_controlnet`, it returns `selection_required` with kind `tile_controlnet`, even for a single candidate. Pick the one candidate whose name marks it as a Tile model; when none or several do, ask the user.

## Selection Required

Exit status 3 with `error.code` `selection_required` means one name or reference matched several things. `error.details` holds `kind`, `selector`, and `candidates`. Bediz submitted nothing.

- Choose a candidate yourself when the user's constraints single one out, or when it is a technical choice within your discretion, such as one of several compatible encoders.
- Ask the user when the candidates differ in a way they would care about, such as two different main models that share a name, or several versions or files of a model to download.
- Resubmit a new request that names the chosen candidate exactly: its key, board identifier, or version or file identifier.

## Unknown Outcome

`outcome_unknown` means Bediz sent a state-changing request once and cannot tell whether InvokeAI applied it. Bediz never retries it. Inspect the state before any resubmission:

| After | Inspect |
| --- | --- |
| `generate` or `upscale` enqueue, `queue cancel`, `queue clear` | `bediz queue list --json`, then `bediz queue get ITEM_ID --json` |
| `models install` | `bediz models list --json`, and `bediz models status --job-id JOB_ID --json` for each job a starter install lists in `error.details.jobs` |
| `models delete` | `bediz models list --json` |
| `images upload`, image-to-image source upload, `images delete` | `bediz images list --json` |
| `boards create` | `bediz boards list --json` |
| `auth huggingface login`, `auth huggingface logout` | `bediz auth huggingface status --json` |

When the state shows the change happened, continue from it. Submit again only when it clearly did not, and the user still wants it.

For image-to-image failures, decide from `error.code`, never from `error.details.source_uploaded` alone. `source_uploaded: true` only says the source image now exists; every failure after upload carries it, including an enqueue with an Unknown Outcome.

- For a local `path` source, `outcome_unknown` with `source_uploaded: true` means enqueue may have happened. Inspect `bediz queue list --json` and `bediz queue get ITEM_ID --json` before any resubmission.
- For a local `path` source, `outcome_unknown` without `source_uploaded` concerns the upload itself; nothing was enqueued. Inspect `bediz images list --json` before uploading again.
- For an existing `image` source, `outcome_unknown` concerns enqueue. Inspect the queue as in the table, even when `source_uploaded` is absent.
- Only after a conclusive failure following a successful upload, and when the user still wants the result, resubmit using `error.details.source_image.image_name` as an `image` source so the file is not uploaded twice. Examples are an `invokeai_operation_failed` enqueue rejection or a failed or canceled item. Inspect accepted batch items before replacing a failed or canceled item, since other items may still be running.

`wait_timeout` and `interrupted` are not failures of the job: the queue items keep running. Resume with `bediz queue wait ITEM_ID --json` for each ID in `error.details.pending_item_ids`; never resubmit. Cancel only when the user asks, with `bediz queue cancel ITEM_ID --json`.

## UI Synchronization

After `generate` and `upscale`, Bediz sends the resolved settings to the InvokeAI web interface (**Parameter Recall**). Report its warnings honestly:

- Automatic Recall replaces the web interface's LoRA list, including the human's selections, with the execution's exact ordered LoRAs and weights. A generation without LoRAs or an upscale clears that list.
- Quick consecutive runs whose Recalls overlap, including `--no-wait` runs, can leave an earlier execution's LoRA in the web interface. The Execution Receipt is the authoritative record of what ran.
- `ui_sync_partial`: InvokeAI accepted the job and the settings Bediz could send. An open web interface restores only part of them: tell the user it does not restore the fields in `details.not_restored`. The Execution Receipt keeps every value. Bediz cannot tell whether a browser was open to receive the settings, so do not claim the user can see them.
- Image-to-image adds `source_image` and `strength` to `ui_sync_partial`'s `details.not_restored` for every supported family. Tell the user automatic Recall does not restore the source or Denoising Strength in the web interface.
- `ui_sync_failed`: InvokeAI accepted the job, but the interface did not receive its settings. A LoRA display-name collision, even with a LoRA of another base, prevents the entire Recall patch and leaves the interface unsynchronized; selecting by key still lets generation run. Say so, rather than telling the user the interface shows them. After a generation, you can load the supported settings on request with `bediz recall --request - --json`, sending `model`, the prompts, `width`, `height`, `steps`, and the first `seed` from the receipt. Standalone `recall` does not accept or restore LoRAs; for those, the human can use the receipt to continue in the web interface.

## Models

When no installed model fits, or Bediz reports `missing_component`, read [installing-models.md](installing-models.md) before installing anything. It covers the **Installation Consent** rule, finding models with your browser, and the install commands.

## Execution Approval

Bediz needs an explicit `--yes` for irreversible actions: `images delete`, `models delete`, `queue clear`, `profiles delete`, `profiles create --replace`, and `models install --move`. Supply `--yes` only for the action the user approved in this conversation, naming exactly what it destroys. Generation, upscale, upload, download, waiting, and `queue cancel` need no approval.

## Other commands

| Need | Command |
| --- | --- |
| Gallery | `bediz images list --json` (`--board`, `--limit`, `--offset`), `bediz images get IMAGE_NAME --json`, `bediz images upload PATH --json` |
| Queue | `bediz queue list --json`, `bediz queue get ITEM_ID --json` |
| Output Boards | `bediz boards list --json`, `bediz boards get SELECTOR --json`, `bediz boards create NAME --json`; use the returned `board_id` in requests |
| Generation Profiles | `bediz profiles list --json`, `bediz profiles get NAME --json`; name one with `"profile"` in a request |
| Load settings into the web interface without running | `bediz recall --request - --json` |

A Generation Profile stores technical defaults only: no prompts, seeds, sources, boards, or LoRAs. Create one with `bediz profiles create --request - --json` when the user asks to save settings for reuse.
