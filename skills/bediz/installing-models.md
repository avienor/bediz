# Installing models with Bediz

## Installation Consent

- The user asked to install or use a specific model: install it without asking again.
- A creative request only revealed that a model or component is missing: first look for an installed compatible model with `bediz models list --json`. When none fits, tell the user the source, the approximate download size, and the license as the source states it, or that the license is unknown. Install only after the user agrees.

Consent covers the model you described. A different model, version, or file that turns up later needs its own consent.

## Finding a model

Bediz resolves exact references; it does not search or recommend. Use your browser to research candidates on Hugging Face, Civitai, or the publisher's page, and confirm before proposing one:

- Its base matches a supported family: `anima`, `sdxl`, or `flux` for generation; `sdxl` or `sd-1` for upscale.
- For FLUX.1, a `dev` or `schnell` variant in `checkpoint`, `bnb_quantized_nf4b`, or `gguf_quantized` format. Ordinary Diffusers-format FLUX.1 models install but do not generate.
- Its download size and license, to report under Installation Consent.

Then hand Bediz the exact reference and leave the download to it.

## Sources

| `source.type` | `source.reference` | Choices Bediz may return |
| --- | --- | --- |
| `starter` | An exact InvokeAI starter identifier, such as the one in a `missing_component` error's `installation_guidance` | None; declared dependencies install first, and already installed entries appear in `skipped` |
| `huggingface` | `org/repo` or `https://huggingface.co/org/repo` | `huggingface_artifact` when the repository holds several checkpoints; resubmit with `--artifact URL` |
| `url` | A direct HTTP(S) file URL without a query, fragment, or credentials | None |
| `civitai` | A model-version ID, or a model page URL with `?modelVersionId=` | `civitai_version` for a page without a version; `civitai_file` for a version with several files; resubmit with the chosen version ID or `--file-id` |
| `path` | An absolute path on the InvokeAI server, not on this machine | None |

A choice among versions or files belongs to the user unless their request already settles it. `bediz models scan --path SERVER_FOLDER --json` lists the model files in a server folder and whether each is installed.

A `path` source registers the file where it is. Moving it into InvokeAI's model storage needs `--move` plus `--yes`, and the user's approval of that move.

```sh
bediz models install --source-type huggingface --source org/repo --json
```

The same install as a Request Document, for example an exact Civitai file:

```sh
bediz models install --request - --json <<'EOF'
{"schema_version": 1, "source": {"type": "civitai", "reference": "VERSION_ID", "file_id": 12345}}
EOF
```

## Protected sources

- A gated Hugging Face repository needs InvokeAI's stored login: check `bediz auth huggingface status --json`. When it is not `valid`, ask the user to run `bediz auth huggingface login --token-stdin` with their token on standard input.
- A protected download also takes a one-time token through `--token-stdin`. Feed it from where the user keeps it, for example `printf '%s' "$HF_TOKEN" | bediz models install --source-type huggingface --source org/repo --token-stdin --json`. The token never appears in a command you show, a file, or a reply.
- `--token-stdin` cannot accompany `--request -`, because both read standard input. Pass the source as flags instead.

## Tracking the install

The result's `data.jobs` lists each accepted job with `job_id` and `status`. Poll `bediz models status --job-id JOB_ID --json` every few seconds until the status is `completed`, `error`, or `cancelled`. A completed job reports its `model_key`; confirm it with `bediz models list --json`, and rerun `bediz doctor --json` when the new model was meant to make an operation compatible. Job IDs do not survive an InvokeAI restart, so after a restart inspect `bediz models list --json` instead.
