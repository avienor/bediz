# 04: Upscale a local image file

**What to build:** Let `bediz upscale` accept a Source Image from the CLI machine. The request uses `source: {"type": "path", "reference": "<absolute path>"}`, or the `--image-path` flag. `--image` and `--image-path` are mutually exclusive, and exactly one is required. The rules, recorded in the `generative-upscale` feature spec:

- **Before network:** The path must be absolute and name a readable regular file. Otherwise the request is `invalid_request` and nothing is sent.
- **Upload timing:** The upload happens after every validation and resolution step (version, main model, Upscale Component Set, OpenAPI vocabulary) and immediately before the enqueue. An invalid or unresolvable request therefore never leaves an uploaded image.
- **Upload behavior:** The upload follows `images upload`: a non-intermediate user image, no resizing, no injected metadata, and no board. It is sent once and never retried. An inconclusive upload returns `outcome_unknown` and nothing is enqueued. A conclusive upload rejection is `invokeai_operation_failed`.
- **Receipt:** A successful upscale returns the uploaded Image Reference as `source_image` with `source_uploaded: true`.
- **Output dimensions:** The expected output dimensions come from the uploaded Image Reference's dimensions, using the rule from ticket 02.
- **Later failures:** After a successful upload, every later failure reports the complete uploaded Image Reference in its structured error details. This covers enqueue rejection, an inconclusive or malformed enqueue response (`outcome_unknown`), a malformed or unknown queue response (`invalid_invokeai_response`), a failed or canceled item, `scale_not_applied`, `wait_timeout`, and `interrupted`. Bediz never deletes the uploaded image.
- **Doctor:** `upscale/sdxl` and `upscale/sd-1` add the tested image upload endpoint (`POST /api/v1/images/upload`) to their endpoint requirements. Without it, both entries are reported incompatible with the missing endpoint named, because the upscale contract now includes local-file sources.

**Blocked by:** 02: Upscale an existing InvokeAI image with an SDXL model.

**Execution route:** `worker + independent review` — the mutation order and failure reporting are fully specified, and CLI-seam tests that observe the exact InvokeAI request sequence reliably detect an incorrect implementation.

**Verification gate:**
- Public-seam tests assert the full InvokeAI request sequence, not just the final result, and prove:
  - a relative, missing, directory, or unreadable path → `invalid_request` before network;
  - a resolution failure (for example Tile ControlNet `selection_required`) → no upload;
  - exactly one upload followed by exactly one enqueue on success, with `source_uploaded: true` and the uploaded Image Reference in the receipt;
  - an inconclusive upload → `outcome_unknown` with no enqueue;
  - a rejected upload → `invokeai_operation_failed`;
  - every post-upload failure reports the complete uploaded Image Reference (name, dimensions, and URLs, not only the name), with no delete request. This covers a rejected enqueue, an inconclusive enqueue, a malformed enqueue response, a malformed or unknown-status queue response, a failed item, a canceled item, `scale_not_applied`, `wait_timeout`, and `interrupted`;
  - expected output dimensions derived from the uploaded image's dimensions, including an unaligned source;
  - `doctor` reporting both upscale entries incompatible when the upload endpoint is missing, and compatible otherwise;
  - both source flags supplied, or neither → `invalid_request`.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.
- Live against the local InvokeAI 6.14.1 baseline, following the repository's live-verification guide: one waited SDXL upscale from a small local PNG completes, the uploaded source appears in the gallery as Uncategorized, and the receipt names it. Report any live step that was unavailable, and list the gallery images it created.

**Review gate:**
- Scope: a fixed base/head diff.
- The reviewer independently checks the request-sequence assertions against the upload-timing rule and confirms that no code path uploads before resolution completes or retries the upload.
- The reviewer checks that token and URL redaction rules are unchanged in the new error details.
- Blocking findings: an upload before validation or resolution completes; a second upload or enqueue; an automatic delete; a post-upload failure without the complete uploaded Image Reference; `doctor` advertising upscale without the upload endpoint; a success receipt without `source_uploaded`.

**Escalate when:** InvokeAI's upload response lacks a field needed for the Image Reference. A post-upload failure path cannot carry the uploaded reference without changing the Structured Error contract. Live upload behavior differs from `images upload`. Repeated repair loops fail.

**Permanent records:** V1 spec and tests. §13 records the path source, the upload timing and single-upload rule, `source_uploaded`, and post-upload failure reporting. §10 records the upload endpoint requirement of the upscale entries. No new terminology (Source Image already covers upload) and no ADR (ADR 0012 already requires reporting without deleting).

**Status:** completed

- [x] `bediz upscale --image-path` uploads a local image once, after all resolution and immediately before a single enqueue, and records it in the receipt.
- [x] Every failure after a successful upload reports the uploaded image, and none deletes it.

## Comments

- 2026-09-24: The first waited live `upscale --image-path` of a generated 512 × 512 local PNG with `Juggernaut-XL-v9`, the SDXL Tile ControlNet, scale 2, steps 10, the default tile size 1024, and seed 42 uploaded the source as `d4b6b13d-ffe6-4840-8613-289ce5736239.png`, then queue item 77 failed with CUDA out of memory on the 8 GB GPU (environment limit). The `invokeai_operation_failed` details carried the queue identity, the complete uploaded Image Reference (name, 512 × 512, absolute image and thumbnail URLs, category `user`, origin `external`), and `source_uploaded: true`. Nothing was deleted.
- 2026-09-24: A retry with tile size 512 and steps 4 (seed 42) uploaded `56447ed6-ae79-4c5a-8e6d-2d2c6d5271e1.png` and returned a successful receipt with `source_uploaded: true`, that source as `source_image`, expected 1024 × 1024, and queue item 78 (batch `a0f56d04-97e8-4ec0-b38e-02ba85a90842`) with final image `4eb05337-c435-46c0-8628-87ff91e2b4ca.png` at 1024 × 1024 and seed 42. An independent `images list --board none` showed both uploaded sources as non-intermediate `user` images with no board (Uncategorized). The gallery was checked through the API, not in a browser. `doctor` reported `ready: true` with `upscale/sdxl` and `upscale/sd-1` compatible now that they require the upload endpoint.
- Gallery images created by these checks, left in InvokeAI for the user to remove: uploaded sources `d4b6b13d-ffe6-4840-8613-289ce5736239.png` and `56447ed6-ae79-4c5a-8e6d-2d2c6d5271e1.png`; output `4eb05337-c435-46c0-8628-87ff91e2b4ca.png`; intermediates `d0527b07-b925-4436-8dea-e80996c9021a.png` and `e1ced763-5e07-4072-bc01-b056346b55ae.png` (item 77), and `668e1f1c-4ba0-4205-8696-f5487359d621.png` and `7b9d2337-12bb-48cf-8697-1ccf2a7ab6bb.png` (item 78).
- 2026-09-24: `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
