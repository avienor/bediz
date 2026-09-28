# 01: Share Source Image resolution between upscale and generation

**What to build:** Upscale's Source Image handling becomes a parser-independent module that `generate` can reuse in ticket 02. It moves out of the upscale module. Upscale then consumes the shared module and keeps exactly its current public behavior. The behavior that moves:

- **Source shape validation:** `{"type":"image"|"path","reference":...}`.
- **Local path checks:** absolute path, readable regular file, and the image-content check shared with `images upload`.
- **Existing-image confirmation:** `not_found` for a missing image, and `invalid_invokeai_response` for a contradictory image name.
- **Single upload:** sent once, immediately before the enqueue, after every validation and resolution step. It is never retried, and an inconclusive result is `outcome_unknown`.
- **Upload-failure reporting:** the error after a successful upload carries the complete uploaded Image Reference. The CLI maps it to `source_image` and `source_uploaded: true` in the structured error details.

This is a prefactor: no command, flag, Request Document member, result field, error code, exit status, or request ordering changes.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. The change is mechanical and reversible, and the existing public upscale tests fully describe the behavior to preserve.

**Verification gate:**
- Every existing upscale public-seam test passes. Assertions stay unmodified, and only a test's own compile-level imports may change. The tests cover both source kinds, upload ordering, upload failures, `source_uploaded` error details, and profile interplay.
- No public test anywhere is weakened or deleted.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- No live InvokeAI check is required, because no request InvokeAI receives changes. State that in the ticket comments.

**Review gate:**
- A separate reviewer starts from this ticket, the feature spec (`.scratch/img2img/spec.md`), and V1 spec §13.1–13.3 and §15.1, and reviews a fixed base/head diff.
- The reviewer independently confirms three things:
  - no test assertion changed;
  - the upload still happens only after every validation, resolution, and invocation check, and immediately before the enqueue;
  - every post-upload failure path still reports `source_image` and `source_uploaded: true`.
- Blocking findings: any public behavior change, any reordering of network requests, a weakened or removed test, or the shared module depending on the CLI parser or on upscale-specific settings.

**Escalate when:**
- Preserving behavior would require changing a public test assertion.
- The shared seam cannot be expressed without knowledge of upscale or generation settings.
- Repeated repair loops fail.

**Permanent records:** None. Terminology, accepted design, and behavior stay unchanged, and the existing public tests remain the contract evidence.

**Status:** ready-for-agent

- [ ] Upscale uses the shared Source Image module, and its public behavior is byte-for-byte unchanged at the CLI seam.
- [ ] The shared module is independent of the CLI parser and ready for `generate` to consume.
- [ ] All verification commands pass and the absence of a live check is recorded.
