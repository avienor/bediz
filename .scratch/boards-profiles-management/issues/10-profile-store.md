# 10: Profile store

**What to build:** A user or agent can save, inspect, list, and delete named Generation Profiles on the CLI machine. The commands are `bediz profiles create --request PATH|-`, `profiles list`, `profiles get NAME`, and `profiles delete NAME --yes`. Profiles are local JSON documents, and none of these commands contact InvokeAI.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The document schema and replacement rules are fully decided below, the behavior is local and reversible, and tests with a temporary configuration directory can verify it.

**Verification gate:** CLI-seam tests use a temporary user configuration directory. They cover a valid create; an invalid name; each forbidden field (`positive_prompt`, `negative_prompt`, `seed`, `board_id`, `source`, any token) rejected as an unknown field; an empty profile with no section; unpaired width and height; bounds violations; and the §8.1 strict document rules. Replacement tests cover an existing name without `--replace` (`reason: "profile_exists"`), `--replace` without `--yes` (exit 2), and `--replace --yes` (atomic replacement). Tests also cover list order, a missing profile on get or delete (`not_found`), delete without `--yes`, and a corrupt stored file (`invalid_configuration` naming the profile). No test makes a network request. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §8.1, §16, §18, ADR 0013, and this ticket. Independently check the forbidden-field rejection, the atomic replacement (a failed write leaves the old profile intact), and the name validation, which must stop path traversal. A secret-capable field, a non-atomic replacement, or path traversal blocks acceptance.

**Escalate when:** A profile field looks like it needs family-specific structure beyond the two sections below, or the profile shape conflicts with ADR 0013.

**Permanent records:** V1 spec §16 records the Profile Document schema, the storage location, the name rule, and the command contracts. `CONTEXT.md`'s Generation Profile definition already matches and needs no change. Tests record the contract.

**Status:** done

## Accepted behavior

- **Name:** `^[a-z0-9][a-z0-9_-]{0,63}$`. It is stored as `<user config dir>/bediz/profiles/<name>.json`, next to the existing `config.json`.
- **Profile Document:** `{"schema_version":1,"name":"...","generate":{...},"upscale":{...}}`. At least one section is required. The `name` must match the name the file is stored under.
- **`generate` section:** `model`, `components` (`vae`, `qwen3_encoder`, `t5_encoder`, `clip_embed`), `width` and `height` (together), `steps`, `scheduler`, `guidance`, and `output_count`, all optional.
- **`upscale` section:** `model`, `components` (`upscale_model`, `tile_controlnet`, `vae`), `scale`, `creativity`, `structure`, `steps`, `scheduler`, `guidance`, `tile_size`, and `tile_overlap`, all optional.
- Create validates only family-independent rules: positive steps and output count, positive paired dimensions, a finite guidance of at least 1, a scheduler from any registered family's list, the §13.1 upscale bounds, and non-empty selectors. Family applicability is checked when the profile is used (tickets 11 and 12).
- Create stores the document exactly as validated. `profiles get` returns the stored document. `profiles list` returns `{"profiles":[{"name":..., "sections":["generate", ...]}]}` sorted by name, and ignores files whose names do not match the name rule.
- Replacing an existing profile requires both `--replace` and `--yes`. It writes to a temporary file and renames it into place, with no revision history. Deleting requires `--yes`. Write failures return `configuration_write_failed`.

- [x] All four commands work locally with the strict document rules
- [x] Forbidden content cannot be stored
- [x] Replacement is gated and atomic
- [x] Spec §16 is updated
