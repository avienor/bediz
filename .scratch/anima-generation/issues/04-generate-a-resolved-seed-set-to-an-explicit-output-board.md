# 04: Generate a Resolved Seed Set to an explicit Output Board

**What to build:** Extend the completed Anima path to generate any positive `output_count` as one InvokeAI batch with one ordered queue item per output. Resolve every seed before enqueue: omitted seeds become independent random unsigned 32-bit values, while an explicit seed is used first and incremented for later outputs with unsigned 32-bit wraparound. Apply the optional exact Output Board identifier to every image node, or omit it so results remain Uncategorized. Return one ordered Execution Receipt that associates each queue item, resolved seed, and resulting Image Reference without guessing.

**Blocked by:** 02: Poll one Anima execution to a completed Execution Receipt; 03: Resolve Anima settings and components deterministically.

**Execution route:** `worker + independent review` — batch payloads, seed arithmetic, board propagation, queue ordering, and receipts have deterministic fixtures and a small live batch provides strong independent evidence.

**Verification gate:** Pure tests cover random seed assignment, explicit sequences, unsigned 32-bit wraparound, positive output counts, and deterministic batch construction. Public tests prove the single enqueue contains the ordered batch data, the returned item order is preserved, every completed output maps to the same-position seed, a board identifier reaches every output, omission sends no board, and partial or contradictory results fail explicitly. A live two-output generation must return two distinct item IDs and Image References with verifiable seeds. All applicable project verification commands must pass.

**Escalate when:** InvokeAI does not guarantee enqueue item order for batch data; a queue item yields zero or multiple final images contrary to the contract; item metadata contradicts positional seed association; the backend partially accepts a batch; an Output Board needs lookup or creation to work; or two repair attempts fail for the same underlying reason.

**Permanent records:** V1 spec and tests — record multi-output batch semantics, unsigned seed sequencing, ordered receipt mapping, and explicit Output Board behavior. ADR-0009 and ADR-0010 remain unchanged.

**Status:** done

- [x] `output_count` defaults to one and rejects non-positive values before network access.
- [x] All output seeds are resolved before enqueue and recorded in order in `resolved_settings.seeds`.
- [x] Explicit seed sequences increment with unsigned 32-bit wraparound; omitted seeds are independent valid random values.
- [x] One enqueue mutation creates one ordered item per requested output; no item is submitted separately or retried.
- [x] A supplied `board_id` is applied to every output image node; omission submits no board identifier.
- [x] A completed receipt contains ordered output records with matching `item_id`, `seed`, and Image Reference; missing or contradictory associations fail explicitly.
- [x] The narrow behavior tests and `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass; live two-output evidence is recorded.

## Comments

Anima resolution now fixes the complete Resolved Seed Set before enqueue.
Omitted seeds consume one independent unsigned 32-bit random value per output;
an explicit seed increments with unsigned wraparound. Any positive
`output_count` is accepted, while zero and negative counts fail before network
access.

The InvokeAI 6.14 adapter compiles those seeds into one zipped batch datum on
the integer seed node. InvokeAI expands batch values in listed order but
returns the inserted item identifiers newest-first, so the versioned adapter
reverses only the backend seed payload. This preserves InvokeAI's returned
`item_ids` order while keeping it positionally aligned with
`resolved_settings.seeds`. The output image node is shared by every expanded
item: a supplied exact `board_id` is present there once and therefore applies
to every output; omission emits no board field.

Waiting polls every returned item in order and verifies its recorded batch
seed before associating the single Image Reference. Missing, malformed,
duplicate, or contradictory seed metadata and contradictory queue identities
return `invalid_invokeai_response`; a failed or canceled item makes the whole
generation fail even if earlier items completed. Incomplete or partially
accepted enqueue responses return `outcome_unknown` and are never retried.

Repository verification passed:

```text
go test ./...        ok
go test -race ./...  ok
go vet ./...         clean
go mod verify        all modules verified
```

Live verification against InvokeAI 6.14.1 submitted one two-output mutation:
batch `689e18b6-e560-4089-9cdd-57e080692548` returned ordered items `[14, 13]`
with resolved seeds `[424242, 424243]`. Item metadata independently recorded
`14 → 424242` and `13 → 424243`. The completed receipt returned distinct images
`7ed3ea4a-60c7-4372-b883-03ea906dd523.png` and
`b6beb4d4-90a6-4825-9bc5-40a8287f94f6.png`; both full-image URLs returned HTTP
200 `image/png` (76357 and 70710 bytes). This run omitted `board_id`, and both
image records reported `board_id: null`, matching Uncategorized behavior.
Deterministic public payload tests cover the complementary explicit-board case
and prove `board_id` reaches the shared output node for every batch item.
