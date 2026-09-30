# 06: Teach the agent skill and README LoRAs

**What to build:** A Creative Agent using the canonical Bediz skill applies LoRAs through `generate`, finds compatible ones, and still declines what Bediz cannot do. People reading the README learn that LoRAs are supported.

- **Description:** The skill's trigger `description` covers generation with LoRAs. It still excludes inpainting, other image editing, and developing Bediz.
- **Scope:** The scope statement lists LoRAs for Anima, SDXL, and FLUX.1 generation in both Generation Modes. For inpainting, outpainting, Canvas layers, reference images, ControlNet-guided generation, and LoRAs in upscale, it still tells the user to continue in the InvokeAI web interface.
- **Readiness:** The skill explains the `doctor` rows with `setting: "loras"`. A LoRA request needs both the family's `setting: "loras"` row and its Generation Mode row to be compatible.
- **Finding LoRAs:** `bediz models list --type lora --base <family base> --json`. A LoRA's base must equal the main model's base. The skill uses `trigger_phrases` and `default_weight` when present, and otherwise the model description.
- **Usage:** The skill teaches `--lora <selector>[=<weight>]`, repeated and in order, and the Request Document member `loras` with `model` and optional `weight`. It prefers Model Keys when a name contains `=` or is ambiguous.
- **Creative Discretion:**
  - The agent chooses LoRAs and weights when the user did not fix them.
  - When the agent writes or improves the prompt, it includes the trigger phrases of each LoRA it applies.
  - When the user fixed the prompt verbatim, the agent keeps it unchanged (V1 spec §3.1, ADR-0014). It tells the user which trigger phrases are missing and that the LoRA's effect may be weaker without them.
  - Omitting a weight uses the LoRA's recorded default, otherwise 0.75. Valid weights run from −10 to 10.
  - A LoRA or weight the user states is preserved exactly. Generation Profiles hold no LoRAs.
- **Installation:** A missing LoRA follows the existing Installation Consent rule: report its source, approximate size, and known license, and install only after consent unless the user asked for that LoRA. The consent recorded for this feature's development verification is not a product rule.
- **Failures:** The skill maps `selection_required` with kind `lora`, `unsupported_capability` for a wrong base or type, and `invalid_request` with a `loras` field path to their corrections. The existing Unknown Outcome, `wait_timeout`, and `interrupted` guidance is unchanged.
- **Warnings:** The skill explains three things:
  - UI Synchronization replaces the web interface's LoRA list after every generation and upscale.
  - `ui_sync_failed` on a LoRA name collision leaves the web interface unsynchronized.
  - Quick consecutive runs whose Recalls overlap can leave an earlier LoRA in the web interface, and the Execution Receipt is the authoritative record.

  It reports `ui_sync_partial` honestly.
- **README:** The feature list names LoRAs, and the unsupported-features sentence no longer lists them.

**Blocked by:**
- 01: Apply LoRAs to SDXL generation.
- 02: Apply LoRAs to Anima generation.
- 03: Apply LoRAs to FLUX.1 generation.
- 04: Report trigger phrases and default LoRA weights in `models list`.
- 05: Restore the exact LoRA list through UI Synchronization.

**Execution route:** `frontier-owned`. The description wording decides which user requests reach Bediz, and the acceptance evidence is a sampled trigger check rather than a deterministic test.

**Verification gate:**
- The skill alignment test passes with every new command path and long flag the skill shows.
- The skill contains no graph-building or payload logic.
- The skill's LoRA, readiness, installation, and synchronization guidance matches the V1 spec and ADR-0024. No text tells the agent to change a prompt the user fixed.
- The automated trigger check used for earlier skill changes is repeated. The removed image-to-image ticket 05 in git history records its last prompts and results. The method is:
  - a headless agent run in a scratch project holding a copy of the changed skill, with the user's global skills loaded as distractors;
  - one run per prompt, `Skill` allowed, and shell, file-write, web, and agent tools disallowed;
  - a trigger is a `Skill` call naming `bediz`.

  The check runs the existing prompts plus new ones. At least two LoRA prompts are expected to trigger, for example "Generate a portrait with the Alien Style LoRA in InvokeAI" and "Use my detail LoRA at 0.6 on this Anima image". "Train a LoRA on my photos" and "Inpaint the sky in this InvokeAI image" are expected not to trigger. Record the tooling, prompts, and results in the ticket comments.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Not required by this route.

**Escalate when:**
- The trigger check shows a regression on any existing prompt.
- LoRA training, inpainting, or other editing prompts start to trigger the skill.
- Teaching LoRAs would require the skill to state behavior the V1 spec does not record.

**Permanent records:**
- The agent skill.
- `README.md`.
- `CHANGELOG.md` under Unreleased, recording the skill change.
- No V1 spec, ADR, or glossary change: tickets 01–05 record the behavior. The skill's `metadata.bediz-version` changes only at release time, as V1 spec §19 defines.

**Status:** implemented

- [x] The skill routes LoRA requests to `generate`, finds compatible LoRAs, applies Creative Discretion to LoRAs and weights, keeps prompts the user fixed, and follows Installation Consent.
- [x] The skill still declines unsupported editing and LoRA training, and the trigger check confirms both behaviors.
- [x] The README describes LoRAs as a supported capability.

## Comments

**2026-09-29, implementation:** The canonical skill teaches ordered LoRAs for Anima, SDXL, and FLUX.1 in both Generation Modes. It covers the family and mode readiness rows, compatible inventory filters and metadata, flags and Request Documents, exact selectors, omitted and explicit weights, trigger phrases under Creative Discretion, fixed prompts, Installation Consent, structured failures, and the limits of UI Synchronization. Unsupported editing, LoRAs in upscale, and LoRA training remain outside Bediz. The README names LoRAs as supported, and the Unreleased changelog records the skill change. No V1 spec, ADR, glossary, or Go implementation change was needed; `metadata.bediz-version` remains `v1.0.0`.

**Verification:** All of the following passed:

```sh
go test ./internal/cli -run 'TestAgentSkill' -count=1
go test ./...
go test -race ./...
go vet ./...
go mod verify
python /home/nyx/.codex/skills/.system/skill-creator/scripts/quick_validate.py /home/nyx/Workspace/bediz/skills/bediz
git diff --check
```

The existing alignment test checks all shown command paths and long flags, including `models list --type lora --base` and repeatable `--lora`. Alignment and skill format validation were repeated after the review correction. The skill contains no graph-building or private InvokeAI payload logic. The Unknown Outcome, `wait_timeout`, and `interrupted` guidance is unchanged. This documentation slice did not require another live InvokeAI execution or model installation; tickets 01–05 retain the integration evidence. Development verification consent was not added to the product skill.

**Code review:** The `implement` workflow requests review even though this route has no separate review gate. Two independent reviewers inspected the WIP diff against start-of-task commit `68e17a7abc42b5d39d04f5a160816a344d811a3b` before commit.

### Standards

One repeated Generation Profile exclusion sentence was removed from the LoRA section under the skill-writing single-source rule. The authoritative rule remains in the profile paragraph. Re-review found no remaining documented-standard breaches or applicable baseline code smells. Creative Discretion, Installation Consent, canonical terminology, and the skill/controller boundary remain aligned with the reviewed standards.

### Spec

No findings. The documentation covers the requested LoRA routing, readiness, discovery, ordered flags and Request Documents, weights, trigger phrases with fixed prompts preserved, Installation Consent, failure corrections, synchronization warnings, and README/Unreleased updates. No missing content requirements, scope creep, or behavior inconsistent with the V1 spec and ADRs was found.

Final review: Standards 0 remaining findings; Spec 0 findings.

**Automated trigger check (2026-09-29):** The 20 exact prompts from the removed image-to-image ticket were recovered with `git show 070d17a:.scratch/img2img/issues/05-skill-and-readme-image-to-image.md`, including its original 14 prompts and six image-to-image/editing additions. Six new prompts cover LoRA generation, compatible discovery, a fixed prompt and weight, training, and inpainting.

Tooling was Claude Code 2.1.284 headless, model `claude-opus-5-5`, with one fresh invocation per prompt, a two-turn bound, and up to three independent invocations running concurrently. The scratch project holds a copy of the canonical skill and linked installation reference. An isolated `CLAUDE_CONFIG_DIR` links the user's global skill directories, excluding the duplicate personal `bediz`; global and synchronized plugin skills remain loaded as distractors. Every positive result's expanded skill content names the scratch project's `.claude/skills/bediz` directory. The init records show only `Read` and `Skill` available, and no observed call used another tool.

The per-prompt argument list was:

```sh
claude -p '<prompt>' --model claude-opus-5-5 --max-turns 2 \
  --output-format stream-json --verbose --allowedTools Skill \
  --disallowedTools Bash,Write,Edit,MultiEdit,NotebookEdit,WebFetch,WebSearch,Agent,Task \
  --tools Skill,Read --permission-mode dontAsk \
  --strict-mcp-config --mcp-config '{"mcpServers":{}}' --no-session-persistence
```

A trigger is an assistant `Skill` tool call with `skill: "bediz"`. The initial suite passed all 26 routing expectations. After the review removed one repeated body sentence, the six new prompts were repeated on the final skill copy and passed 6 of 6; the trigger frontmatter was byte-for-byte unchanged, so the original 20 routing results remain applicable. The final merged results are below. Every invocation produced a result record and empty stderr. Prompts 4, 9, 16, and 18 reached `error_max_turns` with process status 1; the other 22 results were `success` with process status 0. These are routing observations, not claims of operation completion.

The four new positive LoRA prompts triggered. Training, inpainting, background removal, outpainting, and unrelated development prompts did not trigger Bediz. None of the existing 20 prompts regressed.

| # | Prompt | Expected | Observed | Final result |
| --- | --- | --- | --- | --- |
| 1 | Generate a moody cyberpunk street at night on my InvokeAI | trigger | trigger | success |
| 2 | Bediz ile SDXL modeliyle bir portre üret | trigger | trigger | success |
| 3 | Upscale the last image in my InvokeAI gallery 2x | trigger | trigger | success |
| 4 | Find a good anime FLUX model and install it for InvokeAI | trigger | trigger | error_max_turns |
| 5 | What's stuck in my InvokeAI queue? Cancel it | trigger | trigger | success |
| 6 | Make a board called Moodboard in InvokeAI and put the next renders there | trigger | trigger | success |
| 7 | Save these SDXL settings as a generation profile for Bediz: 30 steps, cfg 6, 1216x832 | trigger | trigger | success |
| 8 | Delete the blurry images from my InvokeAI gallery | trigger | trigger | success |
| 9 | Fix the failing Go test in internal/cli | none | none | error_max_turns |
| 10 | Remove the background from this photo: ~/Pictures/me.jpg | none | none | success |
| 11 | Inpaint the sky in this InvokeAI image | none | none | success |
| 12 | Resize these PNGs to 512px with ImageMagick | none | none | success |
| 13 | Write a Python script that calls the Stable Diffusion API | none | none | success |
| 14 | Generate a logo with DALL-E | none | none | success |
| 15 | Use image-to-image in InvokeAI to turn gallery image source.png into a watercolor landscape while preserving its composition. | trigger | trigger | success |
| 16 | Bediz ile /home/nyx/Pictures/landscape.png dosyasından image-to-image üret; SDXL kullan ve strength 0.35 olsun. | trigger | trigger | error_max_turns |
| 17 | Use Anima image-to-image on my InvokeAI gallery image source.png to make an anime landscape. | trigger | trigger | success |
| 18 | Use FLUX.1 image-to-image in InvokeAI with /home/nyx/Pictures/street.png; keep the layout but make it cyberpunk. | trigger | trigger | error_max_turns |
| 19 | Remove the background from this photo | none | none | success |
| 20 | Outpaint this InvokeAI image to extend its borders | none | none | success |
| 21 | Generate a portrait with the Alien Style LoRA in InvokeAI | trigger | trigger | success |
| 22 | Use my detail LoRA at 0.6 on this Anima image | trigger | trigger | success |
| 23 | Find compatible LoRAs for my FLUX.1 model in InvokeAI | trigger | trigger | success |
| 24 | Bediz ile SDXL portre üret; Detail LoRA ağırlığı 0.6 olsun, prompt aynen "a portrait" kalsın. | trigger | trigger | success |
| 25 | Train a LoRA on my photos | none | none | success |
| 26 | Remove a person from this InvokeAI photo using inpainting | none | none | success |

Initial suite skill SHA-256: `5afbdfe2090e0f411f1666a01d6783512fec47e9d2cb84ab3a40edcda07ba09a`. Final canonical skill and copied skill SHA-256: `4531885b19933bc10fec9cf2679576e23f60579c3ab1797e9ea623b0daf45694`. Unchanged trigger frontmatter SHA-256: `f9641e738f1bdc307e2b6eec5db859147960288a0c8ad6c79c1e60a006d0122f`.

The harness, prompts, metadata, streams, stderr files, and summaries are under `/tmp/bediz-lora-skill-rg7grr51/`; `run.py`, `prompts.json`, and `results.json` describe the check. `initial/` preserves the first full suite; root-level records 21–26 contain the final repeat. These are scratch artifacts, not release files. The temporary credential link was removed after the checks.
