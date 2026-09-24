# 12: Apply profiles to upscale

**What to build:** A caller can run `bediz upscale --profile NAME --image ...`, or send an Upscale Request with `"profile":"NAME"`. The profile's `upscale` section then supplies the main model, the Upscale Component Set preferences, and the upscale settings that the request leaves unset. The rules match ticket 11.

**Blocked by:** 10 (Profile store), 11 (Apply profiles to generate).

**Execution route:** `worker + independent review`. Ticket 11 settles the precedence, warning, and receipt pattern. This ticket applies the same accepted rules to the fully specified upscale fields, and the existing upscale tests give strong regression coverage.

**Verification gate:** CLI-seam tests for SDXL and SD1.5 cover the per-field precedence against the §13.1 defaults; a model from the profile; no model anywhere (`invalid_request`); an absent profile (`not_found`); and a profile without an `upscale` section (`invalid_request`). They also cover a compatible Tile ControlNet preference being used as the caller's Tile choice, and incompatible, ambiguous, or missing `tile_controlnet`, `upscale_model`, or `vae` preferences being skipped with `profile_preference_skipped`, falling back to the existing §13.2 resolution. Another test shows that a profile carrying a `generate` section only fails for upscale but still works for generate. The receipt tests cover `submitted_request.profile` and `resolved_settings.profile`. Tests also confirm that a local `path` source is still uploaded only after every profile-dependent resolution succeeds. A live profile-driven upscale on 6.14.1 is reported, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §13, ticket 11's recorded spec changes, and this ticket. Independently check that no upload happens before profile resolution fails, that Tile ControlNet is never chosen automatically without a preference, and that the upscale receipt follows the generate pattern. An early upload, an inferred Tile choice, or a divergence from the ticket 11 contract blocks acceptance.

**Escalate when:** Applying the ticket 11 pattern to upscale requires a different warning or receipt shape.

**Permanent records:** V1 spec §13.1 through §13.3 record the `profile` field, the `--profile` flag, and the receipt fields. Tests record the contract.

**Status:** done

## Accepted behavior

- The precedence, the inapplicable-setting rejection, the skip warning, and the receipt fields are identical to ticket 11, applied to the `upscale` section fields.
- A compatible `tile_controlnet` preference counts as the caller's explicit Tile choice. Without a usable preference, Bediz keeps the rule of never choosing automatically (`selection_required` with kind `tile_controlnet`).
- The profile model's base still has to be `sd-1` or `sdxl` with variant `normal`.

- [x] Upscale applies profiles with the ticket 11 contract
- [x] Upload ordering and the Tile ControlNet rule are preserved
- [x] Spec §13 is updated

## Comments

- 2026-09-24: CLI-seam tests cover SDXL and SD1.5 settings precedence, model selection, profile errors before network access, compatible and skipped component preferences, Tile selection after a skipped preference, receipt fields and warnings, and local source upload ordering.
- 2026-09-24: Live InvokeAI 6.14.1 verification: `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`; `go run ./cmd/bediz models list --url http://127.0.0.1:9090 --json` confirmed the SD1.5 main, Spandrel upscale model, and SD1.5 Tile ControlNet. With isolated `XDG_CONFIG_HOME`, `go run ./cmd/bediz profiles create --request - --json` created `upscale_check_12` with those model keys, scale 2, steps 10, tile size 512, and overlap 32. `go run ./cmd/bediz upscale --request - --url http://127.0.0.1:9090 --timeout 5m --json` used that profile and existing source `4222e92d-6b8c-43a9-8a61-94e170fbd8ec.png` at 512×512. It completed queue item 92 with 1024×1024 image `0c2562c1-66af-4086-960c-5572ad29b3fa.png`. Both receipt profile fields were present; the sole warning was `ui_sync_partial` in receipt and envelope. The output image remains in the user's gallery.
- 2026-09-24: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
