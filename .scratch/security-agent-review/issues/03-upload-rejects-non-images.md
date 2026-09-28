# 03 — Reject non-image files before uploading

Status: resolved

## Problem
`images.PrepareUpload` / `uploadContentType` (`internal/images/images.go:270-285`) returns the sniffed type even when it is not `image/*` and uploads anyway. `bediz images upload /home/u/.ssh/id_rsa` streams the key to InvokeAI, which may be remote over HTTPS. InvokeAI rejects it afterwards, but by then the bytes have left the machine. With an agent driving the CLI, a prompt-injected path is a realistic way to exfiltrate data. The same path is used by upscale `source.type: "path"`.

## Proposal
Fail with `invalid_request` before any network access unless Go's HTTP content-type detection identifies the file as `image/*`. InvokeAI 6.14.1 accepts image media types and decodes them with Pillow; its upload endpoint is not limited to PNG, JPEG, and WebP. Do not fall back to the file extension alone.

## Acceptance
Uploading a text file returns exit 2 and sends no request (fake server sees zero calls), including when it has an image extension. The same holds for upscale local-path sources, through flags and Request Documents. Recognized image content still uploads with its detected media type, including when its extension is unrelated.

## Comments

### 2026-09-28 — Confirmed and fixed

Confirmed through `CLI.Run` with a synthetic text file named `source.png`. Before the fix, `go test -count=1 ./internal/cli -run '^TestImagesUploadRejectsNonImageBeforeNetwork$'` failed with `code=6`, `requests=2`, and `uploaded_text=true`: the version request and upload both reached the fake InvokeAI, and the text bytes reached its upload handler before rejection.

`uploadContentType` returned non-image media types and substituted an image media type from the filename when possible. It now requires content detected as `image/*` and removes the extension fallback. `PrepareUpload` rejects the file locally as `invalid_request`, closes it, and stops both `images upload` and upscale before version inspection or any other network request. The V1 spec documents the shared content check. Full image decoding remains InvokeAI's responsibility.

The original three-format assumption was incorrect: the installed InvokeAI 6.14.1 router (`/home/nyx/Workspace/image-studio/invokeai/.venv/lib/python3.12/site-packages/invokeai/app/api/routers/images.py`) checks the image media type and opens the bytes with Pillow. The fix accepts recognized image signatures instead of introducing that restriction. Coverage confirms PNG, JPEG, WebP, and GIF uploads with a `.txt` extension, the correct multipart media type, and unchanged bytes. Content without a recognized image signature is rejected regardless of its extension.

Regression coverage checks extensionless text, text with image extensions, empty files, and binary non-images across upload and upscale flags and Request Documents. All 32 rejection cases return one V1 Result Envelope, `invalid_request`, exit status 2, empty stderr, and zero HTTP requests.

Verification passed:

- `go test -count=1 ./internal/cli ./internal/images -run 'TestImagesUpload|TestUploadUsesImageContentTypeForMisleadingExtension|TestUpscaleLocalPath'`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`
- `git diff --check`

Live verification:

- `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`.
- `BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e -run '^TestLiveGate/image_upload_round_trip_self-cleans$'` passed. This selected only the image-upload round trip: the real binary uploaded a unique 2-by-2 PNG, verified it through image inspection and listing, and deleted only the test-created image `e29ea580-8ad7-43d5-bbcc-446d75ba6471.png`. The final image inspection confirmed removal.
