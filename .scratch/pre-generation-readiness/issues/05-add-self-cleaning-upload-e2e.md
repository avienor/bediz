# 05: Add a self-cleaning image upload E2E

**What to build:** Extend the live harness with a deterministic image-upload round trip. The test creates a uniquely identifiable small PNG locally, uploads it once through the real Bediz binary, validates the returned Image Reference, and confirms the same normalized resource through image get and list operations. It then removes only the image created by that test through the backend cleanup API because the Bediz image-delete command belongs to a later delivery slice. The test must preserve enough evidence for safe manual cleanup if automatic cleanup fails.

**Blocked by:** 04: Add a live read-only E2E gate.

**Execution route:** `worker + independent review` — the mutation is limited to a uniquely named fixture, cleanup is targeted and observable, and the public result is strongly verifiable.

**Verification gate:** On the supported local baseline, the upload is sent once and returns a successful V1 envelope containing an absolute Image Reference. The stored image is a non-intermediate user image with no implicit board, resize, or injected metadata; image get and list can observe it; targeted cleanup succeeds; and a final lookup confirms removal. A cleanup failure fails the gate and reports the exact test-created image identifier without deleting any unrelated resource. Existing mock tests continue to verify uncertain outcomes and the no-retry mutation rule, and all project-defined Go verification commands pass.

**Escalate when:** The backend does not return a stable identifier before cleanup is needed; cleanup could target anything other than the test-created image; a failed upload has an unknown outcome that cannot be resolved safely by inspection; the live payload contradicts the accepted OpenAPI contract; or the operation adds an implicit board, resize, metadata, or retry.

**Status:** ready-for-agent

- [ ] A unique PNG fixture completes upload, get, and list verification through the real binary.
- [ ] Image origin, category, intermediate status, board absence, URLs, and normalized result envelope match the accepted contract.
- [ ] Cleanup targets only the created fixture and proves its removal, with safe failure evidence.
- [ ] Live and project-defined Go verification gates pass.
