# 04: Installation documentation and V1 acceptance run

**What to build:** A person or an agent can install a matching Bediz binary and skill from a published release. An agent that installed Bediz this way can then complete a live generation through Bediz, which supplies the remaining §24 evidence.

- **README installation section, for people:** download the archive for the platform from GitHub Releases and verify it with `SHA256SUMS`, or run `go install github.com/avienor/bediz/cmd/bediz@<version>`. Then install the skill from the same tag with `npx skills add`, and run `bediz doctor`.
- **`INSTALLATION.md`, for agents:** written so that a user can tell an agent "read this file and install Bediz". The agent detects the OS and architecture and refuses unsupported platforms with the supported list. It downloads the matching archive and `SHA256SUMS` from the selected release, verifies the checksum, and stops on a mismatch. It places the binary on PATH, or tells the user the directory to add, and runs `bediz version`. It asks the user whether to install the skill for the project or globally, installs it from the same tag, and runs `bediz doctor --json` and summarizes the result. It never handles InvokeAI or Hugging Face tokens.
- **Acceptance run:** the same fresh-context agent, now with only the installed binary and skill, completes an end-to-end generation for a plain creative request on the local InvokeAI 6.14.1 baseline. It also shows the §18 consent behavior on a request whose model is missing.

**Blocked by:** 02 (Canonical agent skill), 03 (Publish tagged releases).

**Execution route:** `frontier-owned`. The documents are instructions for agents, and only a live walkthrough can verify them. The run observes a live system and agent behavior, and its findings may change the skill or spec.

**Verification gate:** Read `docs/agents/live-verification.md` first. The repository is public. The walkthrough uses a (pre)release whose tag contains the skill and the final README and `INSTALLATION.md`. If no such tag exists, one is published through ticket 03 first, after asking the user before pushing it. The installed skill's `metadata.bediz-version` equals the installed binary's `bediz version`. Every command in the README section runs as written on Linux. The tag-pinned `npx skills add` command recorded in ticket 02 installs the skill from that tag, not from `master`. The ticket comments record one fresh-context agent session covering both parts:

- Following only `INSTALLATION.md` in a clean environment, it ends with a checksum-verified binary on PATH, the skill installed from the same tag, and a `doctor` summary.
- It completes a generation, and the comments record its commands, the Execution Receipt, and the output image names. It used `--json`, exact model identifiers, and no InvokeAI payloads.
- For a missing model, it reported the source, size, and license before any installation, and it installed nothing without consent.

`bediz doctor --json` shows no issues, and `TestLiveGate` passes. A local rebuild of the released tag matches the published `SHA256SUMS`. Report the Windows and macOS steps as unverified live when no such machine is available.

**Escalate when:** The ticket 02 tag-pinned command fails against this repository; the repository is not yet public; the agent needs steps the document lacks, or installation requires elevated privileges; the agent misreads or bypasses the skill (return to ticket 02); a spec contradiction appears; or a VRAM, model, or network limit blocks the run.

**Permanent records:** V1 spec §19 and §21 record the supported installation paths. README and `INSTALLATION.md` are added or updated. Any finding from the acceptance run is recorded by reopening ticket 02 or updating the spec.

**Status:** in-progress (fresh-agent walkthrough and acceptance run left to the user)

## Accepted behavior

- The Bediz binary gains no install or skill command.
- The skill always comes from the same tag as the binary.
- The agent asks before it chooses the skill scope and before it modifies PATH.

- [ ] README installation section is verified on Linux
- [ ] `INSTALLATION.md` passes a fresh-agent walkthrough
- [x] Tag-pinned skill installation is confirmed, and the skill and binary versions match
- [ ] A skill-driven end-to-end generation succeeds live
- [ ] The consent behavior for a missing model is observed
- [ ] `doctor` and the capability matrix agree, and the released archives are reproducible
- [x] Spec §19 and §21 are updated

## Comments

**2026-09-24, documentation and release candidate:**

- **Documents:** the README has an Install section. `INSTALLATION.md` is at the repository root. Spec §21 records the two supported installation paths and what `INSTALLATION.md` makes an agent do, and §19 records the `-g` global form of the tag-pinned skill command. A two-axis review led to these fixes:
  - README uses `$TAG` in the `go install` and skill commands.
  - `INSTALLATION.md` no longer shows a token command.
  - It says where the `doctor` report is when `ok` is `false`: `error.details.report`.
  - A missing Node.js no longer ends the whole installation.
- **Release candidate:** the user approved `v1.0.0-rc.1`. Commit 85af9f3 sets `metadata.bediz-version: v1.0.0-rc.1`, and `go run ./tools/release -check v1.0.0-rc.1` passed. Workflow run 36019536669 published a prerelease that is not a draft, with exactly the three archives and `SHA256SUMS`.
- **Checksums and reproducibility:** in a scratch directory, the README's download and verify lines for Linux ran as written with `TAG=v1.0.0-rc.1`, and `sha256sum -c` reported `OK`. The published `SHA256SUMS` is byte-identical to a local `go run ./tools/release v1.0.0-rc.1` build. The extracted binary reports `v1.0.0-rc.1`, commit `85af9f3b…`, and date `2026-09-24T15:20:58Z`.
- **Latest release:** with only a prerelease published, `releases/latest` redirects to `/releases`. `INSTALLATION.md` step 2 stops there and asks the user to name a tag.
- **Tag-pinned skill:** in a scratch project, `npx skills add https://github.com/avienor/bediz/tree/v1.0.0-rc.1/skills/bediz -y` (skills 1.7.0) ran. It installed `.agents/skills/bediz` and symlinked it for Claude Code. The installed `SKILL.md` has git blob `c4deea8a…`, equal to `v1.0.0-rc.1:skills/bediz/SKILL.md`. `origin/master` has no skill yet, and `skills-lock.json` records `"ref": "v1.0.0-rc.1"`. `npx skills list --json` reports the path, and `metadata.bediz-version` is `v1.0.0-rc.1`, which equals the binary's version.
- **Live gate:** `bediz doctor --json` reports `ready: true` with no issues on InvokeAI 6.14.1. `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e -run '^TestLiveGate$'` passed with `live E2E: VERIFIED`.
- **Verification:** `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- **Not run:**
  - The user will run the fresh-agent walkthrough of `INSTALLATION.md` and the skill-driven acceptance generation, including the missing-model consent case.
  - The README steps from `tar` onward were not run, because they install into the user's `~/.local/bin`.
  - The macOS and Windows steps are unverified live: no such machine is available.
- **Walkthrough prompts:** in a fresh session, in an empty project directory:
  1. "Read https://github.com/avienor/bediz/blob/master/INSTALLATION.md and install Bediz v1.0.0-rc.1." Until the branch is merged, use the tag URL `https://github.com/avienor/bediz/blob/v1.0.0-rc.1/INSTALLATION.md`.
  2. In a new session: "Make an image of a lighthouse on a cliff during a storm."
  3. "Make a photo with Stable Diffusion 3.5 Large." The model is not installed. Expect the source, size, and license, and no installation without consent.
