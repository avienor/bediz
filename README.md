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

The easiest way is to ask your agent:

> Read https://github.com/avienor/bediz/blob/master/INSTALLATION.md and install Bediz.

It downloads and verifies a release, asks before changing your `PATH`, installs the agent skill, and checks your InvokeAI. The steps below are the same installation, done by hand.

Choose a release tag from [GitHub Releases](https://github.com/avienor/bediz/releases). Releases exist for Linux amd64, macOS arm64, and Windows amd64. On Linux, download the archive and `SHA256SUMS`, verify the archive, and put `bediz` in `~/.local/bin`:

```sh
TAG=v1.0.0
ARCHIVE=bediz_${TAG}_linux_amd64.tar.gz
curl -fsSLO "https://github.com/avienor/bediz/releases/download/$TAG/$ARCHIVE"
curl -fsSLO "https://github.com/avienor/bediz/releases/download/$TAG/SHA256SUMS"
grep "  $ARCHIVE\$" SHA256SUMS | sha256sum -c -
tar -xzf "$ARCHIVE" bediz
mkdir -p ~/.local/bin
install -m 755 bediz ~/.local/bin/bediz
bediz version
```

Add `~/.local/bin` to `PATH` if `bediz version` is not found. On macOS, use `ARCHIVE=bediz_${TAG}_darwin_arm64.tar.gz` and `shasum -a 256 -c -` in place of `sha256sum -c -`. On Windows, download `bediz_<tag>_windows_amd64.zip` and `SHA256SUMS`, compare `Get-FileHash -Algorithm SHA256` of the zip with its line in `SHA256SUMS`, and put `bediz.exe` in a directory on `PATH`.

With Go installed, you can instead build the tagged version from source:

```sh
go install "github.com/avienor/bediz/cmd/bediz@$TAG"
```

Then install the agent skill from the same tag, for the current project or, with `-g`, for your user. It needs Node.js:

```sh
npx skills add "https://github.com/avienor/bediz/tree/$TAG/skills/bediz"
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
