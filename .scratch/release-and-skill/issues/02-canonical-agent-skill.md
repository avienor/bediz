# 02: Canonical agent skill

**What to build:** The repository ships `skills/bediz/SKILL.md` in the Agent Skills format: a `name`, a trigger-oriented `description`, and `metadata.bediz-version` (the Bediz version it matches) in the frontmatter, and a body that teaches a creative agent to use Bediz. `npx skills add avienor/bediz` discovers it at this path. The skill covers every §19 behavior:

- Run `bediz doctor --json` when capability state is unknown, and act on its capability matrix.
- Inspect installed models and use exact identifiers.
- Translate user intent into typed Request Documents or flags, with `--json`.
- Read the Result Envelope, structured error codes, and exit statuses.
- Improve unspecified prompts and settings, and preserve every prompt, model, and setting the user fixes (ADR-0014).
- Handle `selection_required` by choosing among the returned candidates or asking the user, handle `outcome_unknown` by inspecting state before any resubmission, and report `ui_sync_partial` and `ui_sync_failed` honestly.
- Use browser research for model discovery, and leave installation and execution to Bediz.
- Follow the §18 installation-consent rule, and supply `--yes` only for a destructive action the user approved.

Before writing, read the repository's `writing-for-agents` skill and its `SKILL-MECHANICS.md`, and apply them to the description, the information hierarchy, and pruning. The skill has no graph-building logic and no InvokeAI payloads. A test also keeps the skill aligned with the CLI: every `bediz` command and flag the skill shows must be accepted by the real CLI.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned`. The skill encodes product judgment and the consent policy, and automated checks cannot verify its instructional quality.

**Verification gate:** The alignment test passes and fails when a referenced command or flag is removed. A line-by-line check against §18, §19, ADR-0014, and `CONTEXT.md` terms shows full coverage without contradiction. `npx skills add ./ --list`, or the tool's equivalent local-path listing, discovers the `bediz` skill. A trigger check with representative prompts shows that the description triggers on Bediz and InvokeAI image-generation, upscale, model, queue, and gallery requests. It must not trigger on unrelated coding or image-editing requests that Bediz does not handle. Record the prompt set and its results in the ticket comments; any skill-evaluation tool may run it. The exact tag-pinned installation command is verified against a tagged public repository in a scratch project and recorded in the ticket comments. Try the tree form `https://github.com/<owner>/<repo>/tree/<tag>/skills/<name>` first, and the tag archive form `https://github.com/<owner>/<repo>/archive/refs/tags/<tag>.tar.gz` next. The installed files must come from the tag, not from the default branch. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Escalate when:** Neither tag-pinned form installs a skill from the tag, which breaks spec.md decision 7; the skill needs behavior the CLI lacks, or the spec and the CLI disagree on a command the skill teaches.

**Permanent records:** V1 spec §19 records the skill format, its location, its declared version, and the verified tag-pinned `npx skills add` command. `CONTEXT.md` is updated only if the skill needs a new canonical term. The alignment test records the contract.

**Status:** ready-for-agent

## Accepted behavior

- One canonical skill, at `skills/bediz/SKILL.md`. Agent-specific adapters are out of scope.
- The skill uses canonical `CONTEXT.md` terms and public commands only.
- `metadata.bediz-version` states which Bediz version the skill matches, so an agent can compare it with `bediz version`. It is updated to the new version before each release tag, and ticket 03's workflow enforces this.

- [ ] The skill covers every §19 behavior
- [ ] The alignment test guards commands and flags
- [ ] The trigger check passes and is recorded
- [ ] The skill is discoverable by `npx skills add`
- [ ] The tag-pinned installation command is verified and recorded
- [ ] Spec §19 is updated
