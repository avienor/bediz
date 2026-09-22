# 05: Add a self-cleaning image upload E2E

**What to build:** Extend the live harness with a deterministic image-upload round trip. The test creates a uniquely identifiable small PNG locally, uploads it once through the real Bediz binary, validates the returned Image Reference, and confirms the same normalized resource through image get and list operations. It then removes only the image created by that test through the backend cleanup API because the Bediz image-delete command belongs to a later delivery slice. The test must preserve enough evidence for safe manual cleanup if automatic cleanup fails.

**Blocked by:** 04: Add a live read-only E2E gate.

**Execution route:** `worker + independent review` — the mutation is limited to a uniquely named fixture, cleanup is targeted and observable, and the public result is strongly verifiable.

**Verification gate:** On the supported local baseline, the upload is sent once and returns a successful V1 envelope containing an absolute Image Reference. The stored image is a non-intermediate user image with no implicit board, resize, or injected metadata; image get and list can observe it; targeted cleanup succeeds; and a final lookup confirms removal. A cleanup failure fails the gate and reports the exact test-created image identifier without deleting any unrelated resource. Existing mock tests continue to verify uncertain outcomes and the no-retry mutation rule, and all project-defined Go verification commands pass.

**Escalate when:** The backend does not return a stable identifier before cleanup is needed; cleanup could target anything other than the test-created image; a failed upload has an unknown outcome that cannot be resolved safely by inspection; the live payload contradicts the accepted OpenAPI contract; or the operation adds an implicit board, resize, metadata, or retry.

**Status:** done

- [x] A unique PNG fixture completes upload, get, and list verification through the real binary.
- [x] Image origin, category, intermediate status, board absence, URLs, and normalized result envelope match the accepted contract.
- [x] Cleanup targets only the created fixture and proves its removal, with safe failure evidence.
- [x] Live and project-defined Go verification gates pass.

## Comments

Extended the opt-in live harness with a unique two-by-two PNG whose filename
and pixel values derive from a standard-library UUID. The real Bediz binary
uploads the fixture once, and the gate compares the complete normalized Image
Reference returned by upload with `images get` and the matching unboarded
`images list` entry. It also checks the external origin, user category, exact
dimensions, non-intermediate and unstarred flags, absent board, session, node,
and workflow fields, an empty backend metadata record, and absolute full-image
and thumbnail URLs.

The test records the local fixture name and SHA-256 digest before upload and the
stable InvokeAI image name immediately after a successful response. The digest
and reconstructible UUID-based fixture remain in failed-test output as manual
inspection evidence for an unknown upload outcome; the gate escalates without
retrying or guessing a deletion target. A subtest cleanup deletes only the
known image through InvokeAI's single-image backend endpoint, requires the
response to name exactly that deleted image with no failures, and then invokes
the real binary again to require an `images.get` `not_found` V1 envelope.
Cleanup uses a bounded client independent of the test context because Go
cancels `testing.T.Context()` before cleanup callbacks run.

The first live run exposed that cleanup-context lifecycle and failed before
issuing its DELETE. Its reported image identifier was deleted manually through
the same targeted endpoint and a 404 lookup confirmed removal. After the fix,
the complete live gate passed against InvokeAI 6.14.1 at
`http://127.0.0.1:9090`, including automatic cleanup and final lookup. The
ordinary test suite still skips the live gate when `BEDIZ_E2E_URL` is unset.
