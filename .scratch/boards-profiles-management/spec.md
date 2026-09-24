# Boards, profiles, and remaining safe management commands

Delivery slice 8 of `docs/spec/v1.md` §23. It completes the V1 command surface in §6 except the agent skill and release work (slice 9).

## Scope

- `boards list|get|create`
- `queue wait|cancel|clear`
- `images download|delete`
- `models scan|delete`
- `profiles list|get|create|delete`
- Applying a Generation Profile to `generate` and `upscale`

Board deletion, batch cancellation, bulk image or model deletion, and profile revision history stay out of scope.

## Accepted decisions

The user delegated these decisions on 2026-09-24. They follow from the product's deterministic, agent-first contract. Each ticket records the decisions it needs. The implementing ticket also writes them into the V1 spec.

1. **Board selectors.** `boards get` accepts an exact board identifier or a unique board name. `generate` and `upscale` keep an exact `board_id`, matching the §13.1 table. An ambiguous name returns `selection_required` with kind `board`. Its candidates are sorted by board identifier.
2. **Board names stay unique when Bediz creates them.** `boards create` rejects an exact existing board name with `invalid_request` and `reason: "board_name_exists"`. The error carries `board_ids`, every board with that exact name sorted by identifier. This keeps name selection usable and gives an agent the identifiers it needs. InvokeAI itself allows duplicates, so boards created elsewhere can still share a name. InvokeAI 6.14.1 scopes board visibility to the calling user unless that user is an admin, so every board lookup, including this check, covers only the boards the caller can see.
3. **Queue waiting is observation.** `queue wait` succeeds when every named item reaches a terminal state. Failed or canceled statuses are returned as data, not as an operation error. It accepts only positive item identifiers, like `queue get`.
4. **Cancellation is per item.** `queue cancel ITEM_ID` cancels the named item together with any items InvokeAI links to it in the same workflow-call chain, because InvokeAI 6.14.1 cancels the whole chain. Bediz's own generate and upscale batches create no chains. Batch cancellation is deferred because every receipt already lists item identifiers.
5. **Destructive selectors are exact.** `models delete` accepts only an exact Model Key. `images delete` accepts only an exact image name. Both require `--yes`. Neither accepts a name or a bulk selection.
6. **Download never overwrites.** `images download IMAGE_NAME --output PATH` requires a destination on the CLI machine. It fails before any network request with `invalid_request` and `reason: "output_exists"` when the target exists. It writes to a temporary file in the target directory and publishes it with a create-exclusive operation, such as a hard link, that fails when the target exists. A plain rename is not used because it can replace an existing file. V1 has no `--replace` for downloads.
7. **Profile document.** A Generation Profile is one schema-versioned JSON document. It has a `name` and optional `generate` and `upscale` sections, and at least one section is required. Each section holds only technical settings and component preferences of that operation. Prompts, seeds, sources, boards, and secrets are unknown fields.
8. **Profile use.** A request selects a profile with the `profile` field or `--profile`. Field precedence is family defaults, then profile, then explicit request values. A profile setting that is inapplicable to the resolved family is `invalid_request` naming the profile and field. A profile component preference is used only when it resolves to exactly one compatible model. Otherwise it is skipped with a `profile_preference_skipped` warning, and normal Component Resolution continues. Like every V1 warning, it has `code`, `message`, and `details`, and its specifics go in `details`.
9. **Queue clear follows InvokeAI scope.** On InvokeAI 6.14.1, an admin caller clears and cancels every item in the queue, while any other caller clears only their own items. Bediz reports InvokeAI's deleted count and does not widen or narrow that scope.
10. **Board order follows InvokeAI.** `boards list` returns boards in InvokeAI's newest-first order. InvokeAI 6.14.1 has no tiebreaker for equal creation times, so Bediz promises no order among boards with the same creation time.
