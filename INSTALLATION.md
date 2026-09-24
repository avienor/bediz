# Installing Bediz

You are an agent installing Bediz for the user: a checksum-verified `bediz` binary, and the Bediz agent skill from the same release. The install script does the work; you run it, relay what it reports, and check the user's InvokeAI. Work through the steps in order. Each step ends on a **done when** condition; reach it before you move on. When a step says **stop**, report the reason to the user and end the installation.

Ask the user only where a step says so. Do not use `sudo` or administrator rights. InvokeAI and Hugging Face tokens stay with the user. When InvokeAI requires authentication, the user configures the token in their own terminal; keep every token out of your commands, files, and replies.

## 1. Install

Run the install script for the user's system. On Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/avienor/bediz/master/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/avienor/bediz/master/install.ps1 | iex
```

The script installs the latest release. When the user named a version, such as `v1.0.0-rc.1`, set `BEDIZ_VERSION` to it for the script (`curl ... | BEDIZ_VERSION=v1.0.0-rc.1 sh` on Linux and macOS, `$env:BEDIZ_VERSION = 'v1.0.0-rc.1'` before the command on Windows).

The script verifies the archive against the release's `SHA256SUMS`, installs `bediz` in `~/.local/bin` (`%LOCALAPPDATA%\Programs\bediz` on Windows), checks that it reports the release version, and installs the agent skill for the user with `npx`. It reports progress and notes on stderr.

When the script fails, **stop** and report its `bediz install:` message. It fails without installing anything for an unsupported platform, when no stable release is published and no version was named, and when a download or the checksum fails.

**Done when** the script reports `Installed <path>` for the binary.

## 2. Notes

Act on each note the script printed:

- **Not on PATH:** ask the user whether you should add the directory to their shell profile (or to the user `Path` variable on Windows) or whether they will add it themselves. Change the profile or `Path` only after they agree. Until a new shell picks up the change, run the binary by its full path.
- **Resolves to another file:** tell the user which file shadows the new one and let them decide.
- **Skill not installed:** tell the user the command the script printed. When Node.js is missing, they install Node.js first.

Tell the user that the skill loads in a new agent session.

**Done when** the user knows about every note the script printed.

## 3. Doctor

Run `bediz doctor --json` and summarize the Result Envelope for the user. On success the report is in `data`; when `ok` is `false`, it is in `error.details.report`, with the same fields as far as Bediz could check them:

- `ready`, and from `invokeai` the URL, the version, and whether that version is supported;
- which operations and model families `capabilities` reports as compatible;
- every entry in `issues`, with what the user can do about it.

When the exit status is 5, InvokeAI is unreachable or requires authentication. Bediz connects to `http://127.0.0.1:9090` by default. Tell the user to start InvokeAI, or to set another address with `bediz config set --url URL`. When InvokeAI requires authentication, tell the user to configure their token as the README's connection settings describe; do not ask for the token.

**Done when** you have given the user the summary, together with the installed version, the binary's location, and whether the skill was installed.
