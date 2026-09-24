# Live verification against local InvokeAI

Read this before exercising Bediz against the local InvokeAI baseline. The opt-in E2E commands and what they cover are in `CONTRIBUTING.md`; this file records the machine, its limits, and the recurring traps.

## Baseline

- InvokeAI 6.14.1 at `http://127.0.0.1:9090`, root `/home/nyx/Workspace/image-studio/invokeai`, Python environment managed by `uv` in its `.venv`.
- Check `curl -fsS http://127.0.0.1:9090/api/v1/app/version` first; an `invokeai-web` process is usually already running. Start one only when port 9090 is free, with `/home/nyx/Workspace/image-studio/start-invokeai.sh`, and wait for that endpoint to answer.
- GPU: RTX 4060 with 8 GB VRAM. Use 768 × 768 and one output for ad-hoc generations; use larger sizes only when the behaviour under test needs them. An out-of-memory failure is an environment limit to report, not a graph defect.
- Installed families: Anima (main, VAE, Qwen3 encoder, two LLLite ControlNets) and SDXL (main `Juggernaut-XL-v9` and VAE `sdxl-vae-fp16-fix`, both in diffusers format, installed from the `Juggernaut XL v9` starter). `bediz models list --json` is the current truth.

## InvokeAI source of truth

The installed package at `/home/nyx/Workspace/image-studio/invokeai/.venv/lib/python3.12/site-packages/invokeai` is the exact 6.14.1 code. Read it instead of guessing:

- Invocation fields and defaults: `app/invocations/`, or the live `/openapi.json`.
- Install source parsing, subfolder handling, and token use: `app/services/model_install/model_install_default.py`.
- Model bases, types, and variants: `backend/model_manager/taxonomy.py`.
- Frontend behaviour, including the Recall event handler: the built bundle under `frontend/web/dist/assets/`. Search it for `recall_parameters_updated`.

## Models and credentials

- Install only models that a ticket or feature spec records as consented. Anything else needs new Installation Consent from the user. Prefer installing through Bediz, which also exercises Bediz.
- Hugging Face credentials belong to the user. When a gated download is needed, ask the user to log in to InvokeAI's Hugging Face integration and accept the repository terms, then confirm with `bediz auth huggingface status` (`valid`). Every token stays in the user's terminal and out of chat, files, and logs.
- Install job IDs belong to the current InvokeAI process. After a restart, reinspect `models list` and the job list before trusting an old ID or resubmitting.
- Starter entries whose `source` uses `org/repo::subfolder` are supported after anonymous tree and access checks. A protected repository needs a valid InvokeAI Hugging Face login before any install job is submitted.

## Live E2E gate

`TestLiveGate` requires a `doctor` report with no issues and every model requirement satisfied. When a slice registers a new family capability, its required models must be installed on the baseline, and the gate must be extended to cover that family, or the gate goes red for everyone.

## UI checks

Use the `browser-harness-local` skill to observe the InvokeAI web UI. Open `http://127.0.0.1:9090` before sending the Recall or generation under test, because Recall reaches only the tabs that are already open. When a patch changes the main model, wait for the model to finish loading before reading the controls. Report the control values you observed, not the values you sent.

## Reporting

- Record each live command and its outcome in the ticket's `## Comments`.
- Name any live step that could not run and why: a missing model, missing consent, VRAM, or network.
- Ad-hoc generations stay in the user's gallery. List their image names so the user can remove them.
