---
name: bediz
description: Drive a local InvokeAI through the Bediz CLI. Use when the user wants images generated or upscaled with InvokeAI or Bediz, models found or installed for InvokeAI, or its queue, gallery, boards, or generation profiles inspected or managed. Not for inpainting, img2img, or other image editing, or for developing Bediz itself.
metadata:
  bediz-version: v1.0.0
---

# Bediz

Bediz is a deterministic controller for a local InvokeAI installation. You are the **Creative Agent**: you turn the user's intent into prompts, a model, and settings. Bediz validates those explicit choices, runs them on InvokeAI, and returns a **Result Envelope**. The human watches the same queue and gallery in the InvokeAI web interface and can take the work over there (**Handoff**).

Bediz never builds prompts, picks creative settings, or guesses between candidates. You never build execution graphs or call InvokeAI's API yourself: every action goes through a `bediz` command.

Bediz covers text-to-image generation, generative upscale, and the model, gallery, queue, board, and profile commands below. For inpainting, img2img, Canvas, LoRAs, reference images, or ControlNet-guided generation, tell the user Bediz cannot do it and that they can continue in the InvokeAI web interface.

## Every call

- Add `--json`. Standard output then holds exactly one Result Envelope; standard error holds diagnostics only.
- Branch on the envelope, not on prose: `ok`, then `data` on success or `error.code`, `error.message`, and `error.details` on failure. Also read `warnings` on success.
- Send operation inputs as a **Request Document** on standard input with `--request -`. It avoids shell quoting, and it compiles to the same operation as the flags. Operation flags and `--request` never mix; `--json`, `--url`, `--token`, `--no-wait`, `--timeout`, and `--yes` may accompany it.
- Every Request Document carries `"schema_version": 1`. Unknown, misspelled, `null`, or inapplicable fields are rejected, so send only fields you mean. The command's `--help` lists its flags.
- Bediz connects to `http://127.0.0.1:9090` unless `--url`, `BEDIZ_URL`, or `bediz config set --url` says otherwise. Keep tokens out of commands you show, files you write, and replies.

| Exit | Meaning | Your move |
| --- | --- | --- |
| 0 | Success | Read `data` and `warnings`. |
| 2 | Invalid request | Fix the field named in `error.details`, or ask the user when that field is one they fixed. |
| 3 | Selection required | See [Selection Required](#selection-required). |
| 4 | Unsupported capability or missing component | Pick a supported family or model, or install the component (see [Models](#models)). |
| 5 | Connection or authentication failure | Check the target with `bediz config get --json`; ask the user to start InvokeAI or supply access. |
| 6 | InvokeAI operation failure | On `outcome_unknown`, see [Unknown Outcome](#unknown-outcome); otherwise report the error. |
| 130 | Interrupted locally | InvokeAI work keeps running; inspect the queue. |

## Workflow

1. **Readiness.** When you do not yet know what this installation supports (first use in a session, after an exit status 4 or 5, or after the user changes InvokeAI), run `bediz doctor --json`. `data.capabilities` is the **Capability Matrix** for this installation: each entry has `operation`, an optional `family`, `compatible`, and `failures`. Request only compatible operation and family pairs. A failure naming `missing_component:<name>` means a model is absent; `data.issues` carries the details. `data.ui_sync` tells you how much of each operation the InvokeAI web interface can show. Compare `data.bediz.version` with this skill's `metadata.bediz-version`; when they differ, tell the user this skill may not match their Bediz.
2. **Model.** Run `bediz models list --json`, narrowing with `--base` or `--type` when the list is long. Select models by their exact `key` (the **Model Key**). Prefer an installed model that fits the request. When none fits, go to [Models](#models).
3. **Request.** Write the Request Document under [Creative Discretion](#creative-discretion), using the [family rules](#families).
4. **Run.** `generate` and `upscale` wait for completion by default. For long jobs, add `--no-wait`, then `bediz queue wait ITEM_ID --json` with the returned `data.queue.item_ids`.
5. **Report.** The success `data` is the **Execution Receipt**: `resolved_settings` (with `model_key`, `component_keys`, and the ordered `seeds`), `queue`, and `outputs`, each with `item_id`, `seed`, and an `image` whose `image_name`, `image_url`, and `thumbnail_url` identify the result. Give the user the image and the settings that matter to them, and report every warning (see [UI Synchronization](#ui-synchronization)). Images stay in InvokeAI; save a local copy only when asked, with `bediz images download IMAGE_NAME --output PATH --json`.

## Creative Discretion

You may improve prompts and choose every model and setting the user left open without asking about each minor choice. Every prompt, model, and setting the user fixed explicitly is sent exactly as given.

- A prompt the user wrote as the prompt is fixed: send it verbatim. A description of what they want is intent: write the prompt yourself.
- A named model, size, seed, step count, scheduler, guidance, output count, or board is fixed.
- Omitting a field is a valid choice: the Generation Profile, then the model family's default, fills it.
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
| `generate`, `upscale`, `queue cancel`, `queue clear` | `bediz queue list --json`, then `bediz queue get ITEM_ID --json` |
| `models install` | `bediz models list --json`, and `bediz models status --job-id JOB_ID --json` for each job a starter install lists in `error.details.jobs` |
| `models delete` | `bediz models list --json` |
| `images upload`, `images delete` | `bediz images list --json` |
| `boards create` | `bediz boards list --json` |
| `auth huggingface login`, `auth huggingface logout` | `bediz auth huggingface status --json` |

When the state shows the change happened, continue from it. Submit again only when it clearly did not, and the user still wants it.

`wait_timeout` and `interrupted` are not failures of the job: the queue items keep running. Resume with `bediz queue wait ITEM_ID --json` for each ID in `error.details.pending_item_ids`. Cancel only when the user asks, with `bediz queue cancel ITEM_ID --json`.

## UI Synchronization

After `generate` and `upscale`, Bediz loads the resolved settings into the InvokeAI web interface (**Parameter Recall**). Report its warnings honestly:

- `ui_sync_partial`: InvokeAI accepted the job and the interface shows part of its settings. Tell the user the web interface does not restore the fields in `details.not_restored`; the Execution Receipt keeps every value.
- `ui_sync_failed`: InvokeAI accepted the job, but the interface did not receive its settings. Say so, rather than telling the user the interface shows them. After a generation, you can load its settings on request with `bediz recall --request - --json`, sending `model`, the prompts, `width`, `height`, `steps`, and the first `seed` from the receipt.

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

A Generation Profile stores technical defaults only: no prompts, seeds, sources, or boards. Create one with `bediz profiles create --request - --json` when the user asks to save settings for reuse.
