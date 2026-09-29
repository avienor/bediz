# Architecture deepening

**Status:** Approved ticket plan (2026-09-29). Tickets 01–09 are ready for agent in dependency order.

An architecture review of the most frequently changed areas (generation, the Capability Matrix, the CLI remote adapter, and `doctor`) found five places where knowledge that belongs to one module is spread across several, or where one rule is enforced by hand at every call site. This plan deepens those modules. Most tickets are behavior-preserving refactors. Three tickets (04, 07, 09) change public behavior, and every one of those changes is additive under schema version 1 (V1 spec §25, ADR-0022).

The accepted product boundary is the V1 specification, in particular §9, §10, §11, §12, §13, §14, §16, §17, and §25. Relevant ADRs: ADR-0007 (single Result Envelope), ADR-0008 (tested versions and unsafe retries), ADR-0009 (Execution Receipts), ADR-0011 (typed, family-aware core generation), ADR-0017 (verified Recall only), ADR-0020 (gateway statuses on mutations), ADR-0022 (schema version 1 stays additive), and ADR-0023 (image-to-image as a Source Image). A contradiction with the V1 specification or an ADR is an escalation, not permission to change the contract silently.

## Decisions

Recorded with the user on 2026-09-29. The user delegated internal design choices and decided the three public-behavior questions below.

### Scope

- In scope: the five deepening candidates listed under *Modules*.
- Deferred: a typed InvokeAI module above the HTTP client with one shared, stateful test fake. Tickets 05–09 consolidate most InvokeAI call sites first; reassess that candidate after they land.
- Refactor tickets (01, 02, 03, 05, 06, 08) change no command, flag, Request Document member, Result Envelope field, error code, exit status, human-readable message, or request InvokeAI receives. The existing public tests and golden enqueue fixtures are their contract evidence.

### Modules

1. **Model Family registry owns everything that varies with the family and the Generation Mode.** For each registered family and mode, that is: the capability entry evaluated before upload or enqueue, dimension alignment (8 for Anima and SDXL, 16 for FLUX.1), the tested scheduler set, applicable component kinds, profile applicability, compilation, component keys for the receipt, and the Recall patch and `not_restored` fields. Generate submission does not branch on a family's base name. Tested scheduler sets are recorded once per family beside that family's capability entry, so generation, upscale, and profile validation read the same set.
2. **Direct Execution module owns the lifecycle of graph-producing operations.** Its steps run in this order: every local and remote preflight check; one Source Image upload immediately before one enqueue; UI Synchronization, reported only as a warning; then waiting, or `--no-wait`. It enforces two rules in one place: every failure after a Source Image upload carries the uploaded Image Reference, and profile preference warnings travel with failures before an accepted enqueue, except the malformed named upload Image Reference clarified in ticket 04. A failure after the enqueue is accepted, for example while waiting, does not carry them, exactly as today for both operations. The module sits above generation, upscale, and Recall, which removes the import cycle that the separate synchronization package exists to avoid. Generate and upscale plug in as two adapters: each supplies resolution, compilation, its Execution Receipt, and its Recall patch. The CLI compiles flags or a Request Document into the typed request and makes one call.
3. **Structured Error module maps domain failures to the public error contract.** It is parser-independent and turns a domain failure into a code, message, details, and warnings. The CLI adapter only writes the Result Envelope or the human-readable output. `doctor` keeps classifying its own diagnostic issues.
4. **One queue polling step, two traversal policies.** The queue module owns a single step that polls one item: queue inspection, the queue identity check, classification against the tested status set, backoff, and stop reporting on timeout or interruption. Two traversal policies use that step, and each keeps its current observable semantics:
   - `queue wait` polls every pending item in rounds and returns failed and canceled items as data.
   - Graph operations (`generate` and `upscale`) wait for their accepted items in queue item order. They also check the batch identity, and they return an operation failure at the first failed or canceled item.
   Graph operations keep their completion checks: the seed check, final non-intermediate image selection, and upscale scale verification. Merging the two policies into one round-based loop is rejected. It would change graph-operation behavior: with a failed first item and a later item that stays pending, generate fails immediately today, but a round-based wait that collects failures could time out instead.
5. **Compatibility Check module evaluates capability entries.** It checks an entry against a single installation snapshot (version, OpenAPI document, model inventory) and returns every failure in `doctor`'s failure vocabulary. `doctor` lists all failures. A command turns the first one into `unsupported_capability`. Special predicates, such as the Recall patch schema, the install schema, the starter catalog, and the Hugging Face login body, attach to their entries.

### Public behavior changes

- **A — generate profile warnings on failure (ticket 04).**
  - Generate mirrors upscale. When a Generation Profile component preference is skipped and a later step fails before an accepted enqueue, the failure Result Envelope still carries the `profile_preference_skipped` warnings.
  - Clarified during ticket 04, with the user's agreement: both operations preserve the existing exception for an upload response with an image name whose image or thumbnail URL cannot be normalized. This failure carries the uploaded Source Image but omits profile preference warnings. The same warning omission after an accepted enqueue is unchanged.
  - V1 §11.4 states this in the same terms as §13.2.
- **B — pending items in generate and upscale wait failures (ticket 07).**
  - `wait_timeout` and `interrupted` from `generate` and `upscale` add `pending_item_ids` to their details, with the same meaning as in `queue wait`: the accepted items not yet observed as terminal when the wait stopped, in queue item order.
  - Because graph operations wait in queue item order, this is the item being waited on plus every later accepted item. A later item can already be terminal in InvokeAI without having been observed. As with `queue wait`, the caller inspects the queue before acting.
  - All other details, codes, and messages stay the same.
- **C — `doctor` and command preflight agree (ticket 09).** Every InvokeAI requirement that a command checks before mutation or enqueue appears in a `doctor` capability row for that operation, evaluated by the same module. The guarantee is one-directional: when the rows that describe a call are compatible, that call's preflight passes. A row may be stricter than one of its callers when it describes several callers. Each call's rows are named explicitly:
  - Generate: the family and mode row. Its UI Synchronization also uses the Recall common requirements plus that family's additional Recall fields; for SDXL, that is `cfg_scale`.
  - Upscale: the family row. Its UI Synchronization uses the Recall common requirements.
  - Standalone `recall`: only the Recall common requirements.
  - `models install`: the generic row, plus the source-type rows below that apply to the request.
  - `auth huggingface login`: the login row.
  
  The Recall row is recorded as the common Recall requirements plus every family's additional Recall fields. `doctor` keeps reporting it exactly as today, including `incompatible_recall_schema:cfg_scale`, because the row also gates the UI Synchronization levels that V1 §10 defines. It is therefore stricter than standalone `recall`, which does not require `cfg_scale` (V1 §12). This asymmetry is documented, not removed. Changing the row's meaning would not be additive.

  Concretely:
  - `models install` gains three rows, following the `starter` precedent:
    - `family: "huggingface"`: the generic install requirements plus `GET /api/v2/models/hugging_face`.
    - `family: "path"`: the generic install requirements plus the `inplace` query parameter.
    - `family: "source_token"`: the generic install requirements plus the `access_token` query parameter.
  - The generic `models install` row keeps its current requirements.
  - New failure values follow the existing vocabulary: `missing_endpoint:GET /api/v2/models/hugging_face`, `incompatible_install_schema:inplace`, and `incompatible_install_schema:access_token`.
  - Text-to-image `generate` and `upscale` preflight also check their entries' endpoints, as image-to-image already does.

## Execution order

01 → 02 → 03 → 04 → 05 → 06 → 07 → 08 → 09. The blocking edges in each ticket are the real gates; the linear order is the agreed working sequence.

## Verification commands

Every ticket runs `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. A ticket that changes a request InvokeAI receives, or `doctor` output, reads `docs/agents/live-verification.md` and exercises the narrowest affected behavior against the supported local InvokeAI 6.14.x baseline when it is available. It records when a live check was unavailable or not required.
