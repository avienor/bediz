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

**Status:** awaiting-review

## Accepted behavior

- One canonical skill, at `skills/bediz/SKILL.md`. Agent-specific adapters are out of scope.
- The skill uses canonical `CONTEXT.md` terms and public commands only.
- `metadata.bediz-version` states which Bediz version the skill matches, so an agent can compare it with `bediz version`. It is updated to the new version before each release tag, and ticket 03's workflow enforces this.

- [x] The skill covers every §19 behavior
- [x] The alignment test guards commands and flags
- [x] The trigger check passes and is recorded
- [x] The skill is discoverable by `npx skills add`
- [x] The tag-pinned installation command is verified and recorded
- [x] Spec §19 is updated

## Comments

**2026-09-24, implementation:** The skill is `skills/bediz/SKILL.md`, plus `skills/bediz/installing-models.md`, which it links. The installation-only branch (consent, browser discovery, sources, protected downloads, and job tracking) is disclosed there. `metadata.bediz-version` is `v1.0.0`, the expected first release. Ticket 03 must set it to the tag being released, including any prerelease tag. The alignment test is `internal/cli/skill_alignment_external_test.go`. It walks the whole command tree through `bediz ... --help` and fails when any of these is not accepted by the CLI:
- a `bediz` command path or long flag in any skill Markdown file;
- a two-word code span naming a command, such as `queue clear`;
- a long flag anywhere in the skill's code.

- **Guard check:** renaming `models install --token-stdin` in the CLI failed the test. Renaming `queue clear` also failed it, after the test learned to read prose command spans. A synthetic-document test pins both failure modes.
- **Discovery:** `npx skills add ./ --list` (skills CLI 1.7.0) listed exactly one skill, `bediz`. `npx skills add <repo path> --skill bediz -y` in a scratch project installed both `SKILL.md` and `installing-models.md`.
- **Tag-pinned install:** `avienor/bediz` is public but has no tag yet, so the mechanism was checked against the tagged public repository `mattpocock/skills`. In a scratch project, `npx skills add https://github.com/mattpocock/skills/tree/v1.0.1/skills/engineering/tdd -y` installed a `SKILL.md` with git blob `1ce5d212…`. That blob equals the file at tag `v1.0.1`; `main` has `8fc08671…`. `skills-lock.json` recorded `"ref": "v1.0.1"`. The tree form works, so the archive form was not needed. The recorded Bediz command is `npx skills add https://github.com/avienor/bediz/tree/<tag>/skills/bediz`. It has not run against a real Bediz tag; ticket 04 exercises it after the first tag.
- **Coverage check:** §18, §19, ADR-0014, and the `CONTEXT.md` terms were checked line by line, and an independent spec review cross-checked every command, field, error code, exit status, and result field against the code. The review found four inaccurate claims, and all four were fixed:
  - `--token-stdin` scope;
  - job IDs after an `outcome_unknown` install;
  - an uninspected `recall` retry;
  - "succeeded" versus "accepted" under `--no-wait`.
  No new `CONTEXT.md` term was needed.

**Trigger check:**

**How it was run (2026-09-24):**
- Tooling: Claude Code 2.1.281 headless (`claude -p`), model `claude-opus-5-5`.
- Project: a scratch project whose `.claude/skills/bediz/` is a copy of this commit's skill. The user's global skills stayed loaded as competing distractors.
- Each prompt ran once with `--max-turns 2`, `Skill` allowed, and shell, file-write, web, and agent tools disallowed.
- A prompt counted as "trigger" when the stream contained a `Skill` tool call with `skill: "bediz"`.
- All 14 runs ended in a result record with empty standard error.

**Result:** 14 of 14 passed.

| # | Prompt | Expected | Result |
| --- | --- | --- | --- |
| 1 | Generate a moody cyberpunk street at night on my InvokeAI | trigger | trigger |
| 2 | Bediz ile SDXL modeliyle bir portre üret | trigger | trigger |
| 3 | Upscale the last image in my InvokeAI gallery 2x | trigger | trigger |
| 4 | Find a good anime FLUX model and install it for InvokeAI | trigger | trigger |
| 5 | What's stuck in my InvokeAI queue? Cancel it | trigger | trigger |
| 6 | Make a board called Moodboard in InvokeAI and put the next renders there | trigger | trigger |
| 7 | Save these SDXL settings as a generation profile for Bediz: 30 steps, cfg 6, 1216x832 | trigger | trigger |
| 8 | Delete the blurry images from my InvokeAI gallery | trigger | trigger |
| 9 | Fix the failing Go test in internal/cli | none | none |
| 10 | Remove the background from this photo: ~/Pictures/me.jpg | none | none |
| 11 | Inpaint the sky in this InvokeAI image | none | none |
| 12 | Resize these PNGs to 512px with ImageMagick | none | none |
| 13 | Write a Python script that calls the Stable Diffusion API | none | none |
| 14 | Generate a logo with DALL-E | none | none |
