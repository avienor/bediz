# 06: Images download

**What to build:** An agent can save a result image to the CLI machine. `bediz images download IMAGE_NAME --output PATH` writes the full-resolution image to that path and returns the image name, the absolute written path, the byte count, and the content type.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review`. It reads from InvokeAI and writes locally under a fully specified overwrite and atomicity rule that tests can verify.

**Verification gate:** CLI-seam tests cover a missing `--output` (exit 2), an existing target (`invalid_request`, `reason: "output_exists"`, no network request), a missing parent directory (exit 2), an absent image (`not_found`), a response above the configured size limit, and a local write failure (`output_write_failed`, no partial file left at the target). They also cover success with byte-identical content. The request accepts both flags and a Request Document. A live download of a generated image on 6.14.1 is reported, or the report says live verification was unavailable. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass.

**Review gate:** Review the fixed diff against spec §15, §15.1, and this ticket. Independently check that a failed download leaves no partial file at the target, that an existing file is never overwritten, including by a race between check and rename where the platform allows it, and that the size limit holds. Check that the target is published with a create-exclusive operation, not a plain rename. Overwriting a file or leaving a partial output blocks acceptance.

**Escalate when:** The 6.14.1 full-image endpoint needs something other than a plain GET, or create-exclusive publishing is impossible on a release target (Linux, Windows, macOS), or the target file system does not support it.

**Permanent records:** V1 spec §15.1 records the download contract. Tests record it.

**Status:** done

## Accepted behavior

- The request has `image_name` (exact) and `output` (a path on the CLI machine; a relative path resolves against the working directory, and the result reports the absolute path). V1 has no `--replace`.
- Bediz writes the content to a temporary file in the target directory and then publishes it with an operation that fails when the target exists, for example `os.Link(temp, target)` followed by removing the temporary file. Go's `os.Rename` must not be used, because it replaces an existing target on every release platform, and an existence check beforehand does not close the race. If the target appears between the pre-check and publishing, the result is `invalid_request` with `reason: "output_exists"`. The temporary file is removed on every path.
- A test proves that a target created after the pre-check but before publishing is left untouched.
- Download is a safe read and may be retried within the configured read-retry limit.

- [x] Download writes atomically and never overwrites
- [x] `doctor` registers download as read-only inspection
- [x] Spec §15.1 is updated

## Comments

- 2026-09-24, live verification on InvokeAI 6.14.1 (`http://127.0.0.1:9090`):
  - `bediz images download e189e208-8e64-421d-a3ed-0ded67f41567.png --output live.png --json` (a generated 768 × 768 image) succeeded with `size_bytes: 375685` and `content_type: "image/png"`. The written file was byte-identical to `curl …/api/v1/images/i/e189e208-8e64-421d-a3ed-0ded67f41567.png/full`.
  - Repeating the same command returned `invalid_request` with `reason: "output_exists"` and exit 2. The existing file was unchanged.
  - `bediz images download does-not-exist.png --output missing.png --json` returned `not_found` with exit 6, and no file was written.
  - `bediz doctor --json` reported `images.download` as compatible.
  - No temporary files were left behind. The live steps created no gallery images.
- The full-image route is a plain GET that accepts the bearer token, so escalation was not needed. Hard-link publishing works on Linux, on macOS, and on NTFS on Windows. A file system without hard links (FAT or exFAT) makes the download fail with `output_write_failed`. It never falls back to an overwrite.
- 2026-09-24: The download originally used the shared JSON response-size limit of 16 MiB, which a 4× upscale of a 1024 × 1024 image (about 20–27 MB) would exceed. The user approved a separate, fixed download limit of 256 MiB. It is not configurable in V1. Spec §15.1 and §15.3 record the limit and the `--timeout` note.
