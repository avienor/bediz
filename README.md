# Bediz

Let your AI agent make images with your local [InvokeAI](https://github.com/invoke-ai/InvokeAI), and follow every step in the InvokeAI web interface.

You tell your agent what you want. The agent writes the prompt and chooses a model and settings. Bediz checks those choices, runs them on InvokeAI, and returns an exact record of what ran. The job shows up in InvokeAI's queue and gallery like any other, so you can watch it and continue the work in the web interface at any point.

```
you ──▶ agent ──▶ bediz ──▶ InvokeAI ◀── web interface ◀── you
```

## Why

Agents are good at turning a request into a prompt. Clicking through a web interface or writing InvokeAI graphs by hand is where they go wrong. Bediz gives them a small, strict command line instead:

- **No guessing.** Bediz never writes prompts or picks between candidates. An ambiguous model name returns the choices; an unsupported setting is an error, not a silent substitute.
- **Exact receipts.** Every generation returns the resolved model keys, settings, and the seed of each image, so any result can be reproduced.
- **Made for agents.** With `--json`, every command writes exactly one result envelope with stable error codes and exit statuses.
- **Careful with your work.** Destructive commands need `--yes`, an uncertain mutation is reported instead of retried, and tokens never appear in output.
- **InvokeAI stays in charge.** Jobs, images, and history live in InvokeAI. Bediz keeps no database of its own.

## What it can do

- Text-to-image generation with Anima, SDXL, and FLUX.1
- Generative upscale with SD1.5 and SDXL
- Model installation from InvokeAI starter models, Hugging Face, Civitai, direct URLs, and server paths
- Queue, gallery, board, and generation profile management
- Loading a generation's settings into the InvokeAI web interface

Bediz supports InvokeAI 6.14.1 and later 6.14 releases. `bediz doctor` reports what your installation can do. Inpainting, img2img, Canvas, LoRAs, and ControlNet-guided generation are not part of V1; continue in the web interface for those.

## Install

On Linux amd64 or macOS arm64 (Apple silicon):

```sh
curl -fsSL https://raw.githubusercontent.com/avienor/bediz/master/install.sh | sh
```

On Windows amd64, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/avienor/bediz/master/install.ps1 | iex
```

The script installs the latest release: it verifies the archive against the release's `SHA256SUMS`, puts `bediz` in `~/.local/bin` (`%LOCALAPPDATA%\Programs\bediz` on Windows), and installs the agent skill from the same release for your user with `npx`. Without Node.js it prints the skill command to run later. It never uses `sudo` or edits your shell profile; when the directory is not on `PATH`, it tells you what to add. Set `BEDIZ_VERSION` to install another release, such as `v1.0.0-rc.1`, and `BEDIZ_INSTALL_DIR` to use another directory.

You can also ask your agent to install Bediz:

> Read https://github.com/avienor/bediz/blob/master/INSTALLATION.md and install Bediz.

With Go installed, you can build the latest release from source instead, then install the agent skill from the version `bediz version` reports:

```sh
go install github.com/avienor/bediz/cmd/bediz@latest
npx skills add https://github.com/avienor/bediz/tree/$(bediz version)/skills/bediz -g
```

## Usage

With InvokeAI running and the skill installed, start a new agent session and ask for an image:

> Make an image of a lighthouse on a cliff during a storm.

The agent checks what your InvokeAI supports, picks an installed model, runs the generation, and reports the image and its settings. When no installed model fits, it suggests one and installs it only after you agree.

You can run the same commands yourself:

```sh
bediz doctor
bediz models list
bediz generate --model "Anima Base 1.0" --prompt "a lighthouse on a cliff during a storm"
```

Add `--json` for machine-readable output. Every operation also accepts its input as a JSON request document from a file or standard input:

```sh
echo '{"schema_version":1,"model":"Anima Base 1.0","positive_prompt":"a lighthouse on a cliff during a storm","seed":42}' |
  bediz generate --request - --json
```

Run `bediz help` to see every command, and `bediz <command> --help` for its flags.

## Connection settings

Bediz connects to `http://127.0.0.1:9090` by default. Connection settings resolve in this order:

1. `--url` and `--token` on the command
2. `BEDIZ_URL` and `BEDIZ_TOKEN`
3. the per-user Bediz configuration file
4. `http://127.0.0.1:9090`

Manage the saved settings with:

```sh
bediz config get
bediz config set --url http://127.0.0.1:9090
bediz config set --token TOKEN
bediz config set --unset-token
```

A single-user InvokeAI needs no token. Tokens are never included in command output. The configuration file is written atomically with user-only permissions.

## Documentation

- [INSTALLATION.md](INSTALLATION.md): installation steps for agents
- [skills/bediz](skills/bediz/SKILL.md): how an agent uses Bediz
- [docs/spec/v1.md](docs/spec/v1.md): the complete V1 behavior
- [CONTEXT.md](CONTEXT.md): the project's vocabulary
- [docs/adr](docs/adr): design decisions
- [CONTRIBUTING.md](CONTRIBUTING.md): building, testing, and releasing

## License

Bediz is licensed under the [Apache License 2.0](LICENSE).
