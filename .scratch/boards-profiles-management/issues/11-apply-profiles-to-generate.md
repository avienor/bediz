# 11: Apply profiles to generate

**What to build:** A caller can run `bediz generate --profile NAME --prompt ...`, or send a Generation Request with `"profile":"NAME"`. The profile's `generate` section then supplies the model, the component preferences, and the technical settings that the request leaves unset. The Execution Receipt shows what the profile contributed.

**Blocked by:** 10 (Profile store).

**Execution route:** `frontier-owned`. This changes the resolution order of every registered generation family (Anima, SDXL, FLUX.1) and the Execution Receipt and warning contract. Those are sensitive public contracts with cross-family edge cases.

**Verification gate:** CLI-seam tests for each registered family cover the precedence (family default → profile → explicit value, per field, with width and height as a pair); a model taken from the profile when the request omits one; a request with neither model (`invalid_request` before the network); and an absent profile (`not_found` before the network). They also cover a profile without a `generate` section, which is `invalid_request`. Further tests cover an inapplicable profile setting for the resolved family (`invalid_request` with `source: "profile"` and the field), such as guidance with FLUX.1 schnell or `qwen3_encoder` with SDXL. Component tests cover a compatible preference being used, and incompatible, ambiguous, or missing preferences being skipped with `profile_preference_skipped` while resolution continues. Finally, the receipt must carry `submitted_request.profile` and `resolved_settings.profile`, and automatic Recall must use the resolved values. A live generation with a profile on 6.14.1 is reported, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Not required by this route.

**Escalate when:** The precedence cannot be implemented without changing the behavior of a request that has no profile. Also escalate when a family's existing validation order (§11.2) conflicts with applying the profile after main-model resolution.

**Permanent records:** V1 spec §8.3, §11.2, §11.4, and §11.6 record the `profile` field, the `--profile` flag, the precedence, the `profile_preference_skipped` warning, and the receipt fields. `CONTEXT.md` gains no new term (Generation Profile and Component Resolution already cover this). Tests record the contract.

**Status:** done

## Accepted behavior

- Bediz loads the profile during local validation, before any network request. When `profile` is used, the request's `model` becomes optional; the resolved model is the explicit value, else the profile's, and it is required.
- Settings: the request value wins, then the profile value, then the family default. After main-model resolution, a profile setting inapplicable to the resolved family is `invalid_request` with details `{"source":"profile","profile":NAME,"field":...}`. Bediz never silently drops it.
- Component selectors: an explicit request override wins. A profile preference is used when it resolves by exact key or unique name to exactly one model with the required base and type. A profile preference that is incompatible, ambiguous, or absent is skipped. The skip adds a V1 warning `{"code":"profile_preference_skipped","message":...,"details":{"profile":NAME,"component":KIND,"reason":"incompatible"|"ambiguous"|"not_found"}}`, which appears in both the Result Envelope and the receipt warnings. Automatic resolution then proceeds as usual. For SDXL, a compatible VAE preference acts as the override; otherwise the bundled VAE is used.
- A profile selector naming a component kind that does not apply to the family (such as `qwen3_encoder` for FLUX.1) is an inapplicable setting and returns `invalid_request`, not a skip.
- `submitted_request` keeps `profile` as given, and `resolved_settings.profile` names the applied profile. Standalone `recall` does not accept profiles.

- [x] The precedence and the model-from-profile behavior work for Anima, SDXL, and FLUX.1
- [x] Inapplicable profile settings are rejected; unusable component preferences are skipped with a warning
- [x] The receipt and warnings reflect the profile
- [x] Spec §8.3 and §11 are updated

## Comments

- 2026-09-24: Live InvokeAI 6.14.1 verification: `curl -fsS --max-time 3 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`; `go run ./cmd/bediz models list --json` confirmed the installed SDXL main and VAE.
- 2026-09-24: With an isolated `XDG_CONFIG_HOME`, `go run ./cmd/bediz profiles create --request - --json` created `profile_check` (Juggernaut-XL-v9, `sdxl-vae-fp16-fix`, 768×768, 10 steps, Euler, guidance 5). `go run ./cmd/bediz generate --request - --timeout 5m --json` with `{"schema_version":1,"profile":"profile_check","positive_prompt":"A small red ceramic cup on a wooden table, soft daylight","seed":42}` completed successfully. Receipt resolved the profile model and VAE keys, preserved `submitted_request.profile` and `resolved_settings.profile`, and recorded image `13c7043c-cce5-43a6-b857-b9dae2f3b7b7.png` (queue item 91). The sole warning was `ui_sync_partial`, in both receipt and envelope. The generated image remains in the user's gallery.
- 2026-09-24: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` all passed.
