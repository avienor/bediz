# 05: Run generate through a Direct Execution module

**What to build:** A parser-independent Direct Execution module owns the whole generate lifecycle. The steps run in this order:

1. Every local and remote preflight check:
   - request and profile validation;
   - Source Image preparation;
   - the supported-version check;
   - model inventory;
   - family resolution;
   - the compatibility check.
2. One Source Image upload, immediately before one enqueue.
3. UI Synchronization, reported only as a warning.
4. Waiting, or `--no-wait`.

The module enforces two rules in one place:

- every failure after a Source Image upload carries the uploaded Image Reference;
- profile preference warnings travel with failures before an accepted enqueue, with the malformed named upload Image Reference exception clarified in ticket 04.

Neither rule is repeated in individual steps. The warning rule is deliberately limited to failures before an accepted enqueue. A failure after the enqueue is accepted, for example while waiting, does not carry profile warnings, exactly as today. Extending it there is a separate behavior change and not part of this ticket.

Ticket 04 also preserves the existing exception when an upload returns an image name but its image or thumbnail URL cannot be normalized: the failure carries the uploaded Source Image without profile preference warnings. Preserve the public tests for this exception as well as the ordinary pre-enqueue warning cases.

The module sits above generation and Recall. The generate-specific parts are resolution, compilation, the Execution Receipt, and the Recall patch; a generate adapter supplies them at the module's seam. Upscale joins that seam as the second adapter in ticket 06. The interface stays small: the typed request, the connection, and the wait options go in, and the Execution Receipt with its warnings, or an error, comes out. The CLI `generate` command compiles flags or a Request Document into the Generation Request and makes one call.

This is a prefactor. No command, flag, Request Document member, receipt field, error code, message, exit status, warning, or request InvokeAI receives changes, and the order of network requests is unchanged. Source of truth: `.scratch/architecture-deepening/spec.md` (Modules 2), V1 spec §11.5–11.7 and §12, ADR-0008, ADR-0009, ADR-0017, ADR-0020, and ADR-0023.

**Blocked by:**
- 01 (Model Family registry owns Generation Mode requirements and alignment);
- 04 (Generate keeps profile preference warnings when a later step fails).

**Execution route:** `frontier-owned`. It establishes a new seam. The seam must be shaped so that upscale can join it without generate-specific knowledge, and its ordering invariants protect send-once mutation semantics.

**Verification gate:**
- Every existing public generate test passes with unmodified assertions, and every golden enqueue fixture is unchanged. This covers text-to-image, image-to-image for all families, synchronization, profiles, and `--no-wait`.
- New tests at the module's interface use an HTTP fake of InvokeAI and assert:
  - no upload and no enqueue when any preflight check fails;
  - exactly one upload and one enqueue on success;
  - compile, enqueue, and wait failures after an upload carry the uploaded Image Reference;
  - a pre-enqueue failure after a skipped profile preference carries the warnings, and a wait failure after an accepted enqueue does not;
  - a Recall failure is a warning, not a failure;
  - `--no-wait` sends no wait request.
- The CLI generate command contains no lifecycle ordering.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required, because no request changes. Record that in the ticket comments.

**Review gate:** Not required by this route. A standards and spec review of the diff is still recommended before ticket 06 starts.

**Escalate when:**
- The seam cannot be expressed without generate-specific knowledge in the module.
- An import cycle forces the module below Recall or generation.
- Preserving behavior requires changing a public assertion, a receipt field, or the order of network requests.
- A uniform rule would require carrying profile warnings on failures after the enqueue is accepted.
- Repeated repair loops fail.

**Permanent records:** Tests: the module-interface tests are the evidence for the ordering and post-upload invariants. Terminology (Direct Execution is already defined), accepted design, and behavior are otherwise unchanged.

**Status:** complete

- [x] Generate's full lifecycle runs through one parser-independent Direct Execution module, and the CLI makes one call.
- [x] Post-upload failure carrying and profile warnings on failure are enforced once, in the module.
- [x] Module-interface tests prove ordering, send-once behavior, and the post-upload invariant.
- [x] All public generate tests and fixtures are unchanged, and all verification commands pass.

## Comments

### 2026-09-29 — Implementation and verification

- `directexecution.Generate` accepts the typed Generation Request, connection, and wait options. Its shared lifecycle performs local preparation, the supported-version check, inventory, adapter resolution, compatibility checking, one Source Image upload, compilation and one enqueue, UI Synchronization, and waiting or no-wait. The shared core has no generation-specific types; upscale can supply the second adapter in ticket 06.
- Generation retains family/profile resolution, Source Image dimension checks, graph compilation, and Execution Receipt construction. Generate synchronization now sits in Direct Execution above generation and Recall. The CLI compiles its request and makes one call.
- One deferred failure rule retains profile preference warnings before a confirmed enqueue and the uploaded Source Image on every subsequent failure. The malformed named upload URL exception and the omission of profile warnings after acceptance remain unchanged.
- Module-interface HTTP-fake tests cover preflight failures without mutation, the complete request order, single upload/enqueue, compile preparation and enqueue failures, inconclusive enqueue results without retry, failed/canceled/malformed/unknown-status queue items, timeout and interruption, Recall failure as a warning, and no-wait without queue inspection. Both invalid image and thumbnail URL exceptions are covered.
- Every CLI test and golden enqueue fixture is byte-for-byte unchanged. The existing post-upload wait assertion moved unchanged from the generation wait test to the Direct Execution interface. Synchronization tests moved with their implementation and retain their assertions.
- Passed `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Narrow Direct Execution, generation, CLI generate, and Structured Error checks also passed.
- Independent Standards and Spec reviews used fixed base `03269d22562ffdf47369a908804ab130e6e9974a` and immutable head snapshot `d1cf51d3c1dfdfad9b7522f783217681d677803f`. Standards: zero documented violations and zero actionable smells. Spec: zero findings. Reviewed tree: `9a053645dbaeb9cd8630f841721ea9443f3f64e4`.
- No live InvokeAI check was required or run: this prefactor changes no request InvokeAI receives, as the verification gate specifies. Product behavior, terminology, the V1 specification, ADRs, and CHANGELOG therefore need no changes.
- Committed on the existing `master` branch as `bd0efa446418fb2986d17402a074f4be63d0880f` (`Run generation through direct execution`). The committed tree exactly matches the reviewed tree. This local ticket remains untracked with the pre-existing `.scratch/` files.
