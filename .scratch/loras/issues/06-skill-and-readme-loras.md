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

**Status:** ready-for-agent

- [ ] The skill routes LoRA requests to `generate`, finds compatible LoRAs, applies Creative Discretion to LoRAs and weights, keeps prompts the user fixed, and follows Installation Consent.
- [ ] The skill still declines unsupported editing and LoRA training, and the trigger check confirms both behaviors.
- [ ] The README describes LoRAs as a supported capability.
