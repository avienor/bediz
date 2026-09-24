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

**Status:** ready-for-agent

## Accepted behavior

- The Bediz binary gains no install or skill command.
- The skill always comes from the same tag as the binary.
- The agent asks before it chooses the skill scope and before it modifies PATH.

- [ ] README installation section is verified on Linux
- [ ] `INSTALLATION.md` passes a fresh-agent walkthrough
- [ ] Tag-pinned skill installation is confirmed, and the skill and binary versions match
- [ ] A skill-driven end-to-end generation succeeds live
- [ ] The consent behavior for a missing model is observed
- [ ] `doctor` and the capability matrix agree, and the released archives are reproducible
- [ ] Spec §19 and §21 are updated
