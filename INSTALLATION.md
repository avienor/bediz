# Installing Bediz

You are an agent installing Bediz for the user: a checksum-verified `bediz` binary on PATH, and the Bediz agent skill from the same release tag. Work through the steps in order. Each step ends on a **done when** condition; reach it before you move on. When a step says **stop**, report the reason to the user and end the installation.

Ask the user only where a step says so. Everything you install goes into user-owned directories: when a step would need `sudo` or administrator rights, stop and tell the user instead.

InvokeAI and Hugging Face tokens stay with the user. When InvokeAI requires authentication, the user configures the token in their own terminal; keep every token out of your commands, files, and replies.

## 1. Platform

Detect the operating system and CPU architecture: `uname -s` and `uname -m` on Linux and macOS, or `$env:PROCESSOR_ARCHITECTURE` in PowerShell on Windows. Map them to one of the supported platforms:

| Detected | Platform | Archive |
| --- | --- | --- |
| `Linux` with `x86_64` | `linux_amd64` | `bediz_<tag>_linux_amd64.tar.gz` |
| `Darwin` with `arm64` | `darwin_arm64` | `bediz_<tag>_darwin_arm64.tar.gz` |
| Windows with `AMD64` | `windows_amd64` | `bediz_<tag>_windows_amd64.zip` |

Any other combination is unsupported: **stop** and tell the user that Bediz releases exist only for Linux amd64, macOS arm64 (Apple silicon), and Windows amd64.

**Done when** you have the platform name and the archive name pattern.

## 2. Release tag

Use the tag the user named, such as `v1.0.0` or `v1.0.0-rc.1`. Otherwise find the latest release; the final URL ends in `/releases/tag/<tag>`:

```sh
curl -fsSLI -o /dev/null -w '%{url_effective}\n' https://github.com/avienor/bediz/releases/latest
```

In PowerShell, use `(Invoke-WebRequest https://github.com/avienor/bediz/releases/latest -UseBasicParsing).BaseResponse.ResponseUri` (Windows PowerShell) or `.BaseResponse.RequestMessage.RequestUri` (PowerShell 7).

The latest release excludes prereleases. When the command fails or the URL has no tag, **stop**: tell the user no release is published yet and point them to <https://github.com/avienor/bediz/releases> to choose a tag.

**Done when** you have one exact tag, written `<tag>` below.

## 3. Download and verify

Work in a new temporary directory. Download the archive and `SHA256SUMS` from the release:

```sh
TAG=<tag>
ARCHIVE=bediz_${TAG}_linux_amd64.tar.gz   # the archive for your platform
curl -fsSLO "https://github.com/avienor/bediz/releases/download/$TAG/$ARCHIVE"
curl -fsSLO "https://github.com/avienor/bediz/releases/download/$TAG/SHA256SUMS"
grep "  $ARCHIVE\$" SHA256SUMS | sha256sum -c -
```

On macOS, use `shasum -a 256 -c -` in place of `sha256sum -c -`. On Windows, download the same two URLs with `Invoke-WebRequest -OutFile`, then compare the archive's `(Get-FileHash -Algorithm SHA256 <archive>).Hash` with its line in `SHA256SUMS`, ignoring letter case.

When a download fails, **stop** and report the URL. When the archive has no line in `SHA256SUMS`, or the checksums differ, **stop**: tell the user the checksum did not verify, delete the downloaded files, and install nothing.

**Done when** the checksum check reports `OK` for the archive.

## 4. Binary on PATH

Extract `bediz` (`bediz.exe` on Windows) from the archive and copy it to the user's binary directory:

| Platform | Directory | Commands |
| --- | --- | --- |
| Linux, macOS | `~/.local/bin` | `tar -xzf "$ARCHIVE" bediz`, `mkdir -p ~/.local/bin`, `install -m 755 bediz ~/.local/bin/bediz` |
| Windows | `$env:LOCALAPPDATA\Programs\bediz` | `Expand-Archive <archive> -DestinationPath <temp>`, `New-Item -ItemType Directory -Force <directory>`, `Copy-Item <temp>\bediz.exe <directory>` |

Then check what the shell finds: `command -v bediz` on Linux and macOS, `Get-Command bediz` on Windows.

- When the directory is not on PATH, ask the user whether you should add it to their shell profile (or to the user `Path` variable on Windows) or whether they will add it themselves. Change the profile or `Path` only after they agree. Until a new shell picks up the change, run the binary by its full path.
- When `bediz` resolves to a different file, tell the user which file shadows the new one and let them decide.

Run the installed binary with `bediz version --json`. When its `data.version` differs from `<tag>`, **stop** and report both values.

**Done when** `bediz version --json` reports `<tag>` and you know whether `bediz` resolves on PATH.

## 5. Agent skill

The skill needs Node.js for `npx`. When `npx --version` fails, tell the user to install Node.js and install the skill later with the command below, then continue with step 6.

Ask the user whether to install the skill for the current project or globally for their user. Then install it from the same tag as the binary, adding `-g` for a global installation:

```sh
npx skills add https://github.com/avienor/bediz/tree/<tag>/skills/bediz -y
```

Confirm the version: `npx skills list --json` (add `-g` for a global installation) gives the `bediz` skill's `path`. The `metadata.bediz-version` field in `<path>/SKILL.md` must equal `<tag>`. When it differs, **stop** and report both values.

Tell the user that the skill loads in a new agent session.

**Done when** the installed `SKILL.md` declares `metadata.bediz-version: <tag>`, or `npx` is unavailable and the user knows to install Node.js and then the skill from `<tag>`.

## 6. Doctor

Run `bediz doctor --json` and summarize the Result Envelope for the user. On success the report is in `data`; when `ok` is `false`, it is in `error.details.report`, with the same fields as far as Bediz could check them:

- `ready`, and from `invokeai` the URL, the version, and whether that version is supported;
- which operations and model families `capabilities` reports as compatible;
- every entry in `issues`, with what the user can do about it.

When the exit status is 5, InvokeAI is unreachable or requires authentication. Bediz connects to `http://127.0.0.1:9090` by default. Tell the user to start InvokeAI, or to set another address with `bediz config set --url URL`. When InvokeAI requires authentication, tell the user to configure their token as the README's connection settings describe; do not ask for the token.

**Done when** you have given the user the summary, together with the tag, the binary's location, and the skill's scope and path.
