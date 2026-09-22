# 03: Resolve Anima settings and components deterministically

**What to build:** Extend the working Anima path so a Generation Request may omit family-defaulted settings and component selectors without depending on browser state. Resolve values in the approved order, assign the Anima defaults of 1024 by 1024 pixels, 30 steps, Euler scheduling, guidance 4.5, one output, and an empty negative prompt, and assign an unsigned 32-bit random seed before submission when none is supplied. Resolve the required Anima VAE and Qwen3 encoder deterministically: an explicit compatible selector wins, exactly one compatible installed component is selected automatically, ambiguity returns candidates, and absence returns actionable missing-component details. Generation Profiles and the FLUX VAE fallback remain unsupported in this slice.

**Blocked by:** 01: Submit one exact Anima Generation Request.

**Execution route:** `frontier-owned` — this fixes the tested component-compatibility boundary, defaults, and stable structured errors that define the Anima capability.

**Verification gate:** Pure resolution tests cover every precedence and validation branch, including exact keys, unique names, ambiguous names, multiple compatible components, missing components, wrong model families/types, paired dimensions, alignment, scheduler values, guidance, steps, and seed bounds. Public tests prove all failures occur before enqueue and expose stable codes and candidate details. A live check must resolve the installed Anima main model, Anima VAE, and Qwen3 encoder to their exact keys and complete a generation using omitted defaults. All applicable project verification commands must pass.

**Escalate when:** Model metadata and OpenAPI disagree about component compatibility; a compatible VAE or encoder cannot be identified without guessing; a model-specific default conflicts with the accepted Anima family default; implementing resolution requires Generation Profiles or installation; the FLUX VAE fallback becomes necessary; or two repair attempts fail for the same underlying reason.

**Permanent records:** V1 spec and tests — record exact family defaults, validation bounds, selector behavior, supported component combination, `selection_required`, and `missing_component`. No new ADR or terminology change is expected.

**Status:** done

- [x] The canonical request uses `model`, `positive_prompt`, optional `negative_prompt`, paired `width`/`height`, `steps`, `scheduler`, `guidance`, `seed`, `output_count`, optional `board_id`, and typed `components` selectors.
- [x] Omitted Anima settings resolve to the approved defaults without reading frontend or browser state.
- [x] Resolved dimensions are positive multiples of 8, steps are positive, guidance is at least 1, schedulers come from the approved Anima set, and seeds are unsigned 32-bit values.
- [x] Main models resolve only to installed Anima main models; names are accepted only when unique.
- [x] Explicit compatible component selectors win; otherwise exactly one installed Anima VAE and Qwen3 encoder are selected automatically.
- [x] Ambiguity returns `selection_required` with stable candidates; absence returns `missing_component` with component type and known installation guidance; neither path mutates InvokeAI.
- [x] Unsupported InvokeAI versions, missing invocation fields, and untested component combinations return `unsupported_capability` before graph submission.
- [x] The narrow behavior tests and `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass; live default-resolution evidence is recorded.

## Comments

Implemented a pure Anima resolver that applies the accepted family defaults,
validates paired dimensions and every supported setting, assigns an unsigned
32-bit random seed, and resolves the main model, Anima VAE, and Qwen3 encoder
from installed model metadata. Explicit keys and unique names use the same
compatibility checks; omitted component selectors auto-resolve only a single
compatible component. Ambiguous choices are sorted by Model Key, malformed
candidates fail explicitly, and absent components return `missing_component`
with their required base, type, and installation guidance.

The public generation path now validates the supported InvokeAI version and
the required Anima invocation schemas and fields before reading inventory,
compilation, or enqueue. Request validation, component ambiguity, missing
components, incomplete identifiers, unsupported combinations, and OpenAPI
incompatibility all have public CLI coverage proving that no enqueue occurs.
The V1 spec records the exact defaults, bounds, scheduler set, supported
component combination, and structured-error behavior.

Live verification against InvokeAI 6.14.1 used only Anima main Model Key
`06409299-d28f-4c00-8416-4d23cb1b8358` and a positive prompt. Bediz resolved
defaults to 1024 by 1024, 30 steps,
`euler`, guidance 4.5, one output, an empty negative prompt, and random seed
`4069510419`; it resolved VAE key
`0b5d352a-7cc7-424d-ac04-d8613c33508e` and Qwen3 encoder key
`d9532f83-f24d-41cc-a63b-ba560b078e88`. Batch
`4eb8a5c9-4863-4ae7-8d49-503e1dcc3a22`, item `12`, completed as image
`a5ffb64c-7ecd-4962-ae58-67550db3287a.png`; its full-image endpoint returned
HTTP 200 `image/png` (277954 bytes).

Repository verification passed:

```text
go test ./...        ok
go test -race ./...  ok
go vet ./...         clean
go mod verify        all modules verified
```

The two-axis review findings were addressed: the ticket state and evidence are
current; component requirements are typed; repeated CLI preflight setup is
centralized; incompatible unique-name selectors match exact-key behavior;
ambiguity rejects incomplete candidate identifiers; and public tests cover
unsupported versions and component combinations before enqueue. The final
standards and spec re-review reported no remaining findings.
