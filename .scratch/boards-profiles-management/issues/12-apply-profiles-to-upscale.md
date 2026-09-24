# 12: Apply profiles to upscale

**What to build:** A caller can run `bediz upscale --profile NAME --image ...`, or send an Upscale Request with `"profile":"NAME"`. The profile's `upscale` section then supplies the main model, the Upscale Component Set preferences, and the upscale settings that the request leaves unset. The rules match ticket 11.

**Blocked by:** 10 (Profile store), 11 (Apply profiles to generate).

**Execution route:** `worker + independent review`. Ticket 11 settles the precedence, warning, and receipt pattern. This ticket applies the same accepted rules to the fully specified upscale fields, and the existing upscale tests give strong regression coverage.

**Verification gate:** CLI-seam tests for SDXL and SD1.5 cover the per-field precedence against the §13.1 defaults; a model from the profile; no model anywhere (`invalid_request`); an absent profile (`not_found`); and a profile without an `upscale` section (`invalid_request`). They also cover a compatible Tile ControlNet preference being used as the caller's Tile choice, and incompatible, ambiguous, or missing `tile_controlnet`, `upscale_model`, or `vae` preferences being skipped with `profile_preference_skipped`, falling back to the existing §13.2 resolution. Another test shows that a profile carrying a `generate` section only fails for upscale but still works for generate. The receipt tests cover `submitted_request.profile` and `resolved_settings.profile`. Tests also confirm that a local `path` source is still uploaded only after every profile-dependent resolution succeeds. A live profile-driven upscale on 6.14.1 is reported, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §13, ticket 11's recorded spec changes, and this ticket. Independently check that no upload happens before profile resolution fails, that Tile ControlNet is never chosen automatically without a preference, and that the upscale receipt follows the generate pattern. An early upload, an inferred Tile choice, or a divergence from the ticket 11 contract blocks acceptance.

**Escalate when:** Applying the ticket 11 pattern to upscale requires a different warning or receipt shape.

**Permanent records:** V1 spec §13.1 through §13.3 record the `profile` field, the `--profile` flag, and the receipt fields. Tests record the contract.

**Status:** ready-for-agent

## Accepted behavior

- The precedence, the inapplicable-setting rejection, the skip warning, and the receipt fields are identical to ticket 11, applied to the `upscale` section fields.
- A compatible `tile_controlnet` preference counts as the caller's explicit Tile choice. Without a usable preference, Bediz keeps the rule of never choosing automatically (`selection_required` with kind `tile_controlnet`).
- The profile model's base still has to be `sd-1` or `sdxl` with variant `normal`.

- [ ] Upscale applies profiles with the ticket 11 contract
- [ ] Upload ordering and the Tile ControlNet rule are preserved
- [ ] Spec §13 is updated
