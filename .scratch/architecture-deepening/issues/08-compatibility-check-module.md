# 08: Evaluate capability entries with one Compatibility Check module

**What to build:** A parser-independent Compatibility Check module evaluates one capability entry against one installation snapshot (InvokeAI version, OpenAPI document, model inventory). It returns every failure in `doctor`'s existing failure vocabulary.

- The endpoint, invocation, and installed-model predicates exist once.
- The special predicates attach to their capability entries instead of `doctor` branching on the operation name. They are the Recall patch schema, the install schema, the starter catalog response, and the Hugging Face login body.
- `doctor` builds its capability report from the module.
- `generate` and `upscale` preflight evaluate their entries through the module and turn the first failure into `unsupported_capability` with today's message.
- Each evaluation decodes the OpenAPI document once.

The command-side checks of `recall`, `models install`, and `auth huggingface login` stay as they are in this ticket; ticket 09 moves them. Which requirements generate and upscale check also stays the same. Text-to-image and upscale still check only invocations here.

This is a prefactor. `doctor` JSON and human output, and every command's result, error code, and message, stay unchanged. Source of truth: `.scratch/architecture-deepening/spec.md` (Modules 5), V1 spec §5 and §10, and ADR-0008.

**Blocked by:** 01 (Model Family registry owns Generation Mode requirements and alignment), so that the registry decides which entry generate evaluates.

**Execution route:** `worker + independent review`. The existing `doctor` fixture tests pin the public report byte for byte, and the command tests pin the preflight messages.

**Verification gate:**
- Every existing `doctor`, generate, and upscale test passes with unmodified assertions. The `doctor` report for every existing fixture is unchanged.
- New table tests at the module's interface run over the existing InvokeAI 6.14 OpenAPI fixture and variants without HTTP. They cover each predicate, including the special ones.
- `doctor` no longer branches on an operation name to decide a predicate.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required, because neither the requests nor `doctor` output change. Record that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, V1 spec §10, and a fixed base/head diff.
- The reviewer independently confirms each of the following:
  - each predicate has one implementation;
  - `doctor` failure strings and their order are unchanged;
  - generate and upscale preflight messages are unchanged;
  - no request is added or removed.
- Blocking findings:
  - any change to `doctor` output or command results;
  - a predicate still implemented twice;
  - the module depending on `doctor` report types or the CLI parser.

**Escalate when:**
- The existing `doctor` output depends on an evaluation order the module cannot reproduce.
- A special predicate cannot be attached to an entry without changing `doctor` output.
- Repeated repair loops fail.

**Permanent records:** Tests: the module table tests become evidence for the predicates. Terminology (Compatibility Check is already defined), accepted design, and behavior are otherwise unchanged.

**Status:** implemented

- [x] One Compatibility Check module evaluates entries over a single snapshot, and `doctor`, generate, and upscale use it.
- [x] Every predicate, including the special ones, has one implementation and a table test.
- [x] `doctor` output and all command results are unchanged, and all verification commands pass.

## Comments

- The module tests cover version, endpoint, invocation, installed-model, Recall, install, starter catalog, and Hugging Face login predicates using the InvokeAI 6.14 fixture and variants without HTTP. Existing `doctor`, generate, and upscale tests passed without assertion changes.
- Verification: `go test ./...`, `go test -race ./...`, `go vet ./...`, `go mod verify` all passed. No live InvokeAI check was required because requests and `doctor` output did not change.
- Independent spec review found no blocking issue. Standards review found no hard violation; its test-only decode helper finding was addressed before the final commit.
