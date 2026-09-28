# 02 — Surface InvokeAI's rejection reason on conclusive 4xx

Status: resolved

## Problem
`classifyRemote` maps `HTTPError` to "InvokeAI rejected the operation" plus `status` only. The backend body (such as a FastAPI 422 `detail` array naming the invalid graph field) is discarded, so an agent cannot tell a bad value from a version mismatch.

## Proposal
For non-private requests only, add a bounded, sanitized `details.invokeai_detail` (parsed FastAPI `detail`, truncated). Private (token-bearing) requests keep today's behaviour. Decide in spec whether this is part of the stable contract or best-effort.

## Acceptance
A fake backend returning 422 with a `detail` array yields that detail in the envelope. A `DoJSONPrivate` failure still omits it.

## Comments

### 2026-09-28 — Confirmed and fixed

Confirmed through the public CLI seam. Before the fix, `go test ./internal/cli -run '^TestInvokeAIRejectionIncludesValidationDetail$' -count=1` failed because the 422 envelope contained only `{"status":422}`. A live read-only `models scan` against InvokeAI 6.14.1 reproduced the same loss for a 400 rejection: InvokeAI returned a string `detail`, while Bediz emitted only the status.

The CLI discarded the HTTP error body, and the HTTP client truncated that body to 2048 bytes before callers could parse longer validation arrays. The client now extracts a separate bounded diagnostic from the original 4xx response, and the CLI exposes it as optional `error.details.invokeai_detail` on `invokeai_operation_failed`. The user confirmed testing through `CLI.Run` and `Client.DoJSONPrivate` and treating this as best-effort diagnostic information. The V1 spec documents its field name, sanitization, and bounds; presence and backend text remain optional and InvokeAI-dependent.

String details and validation arrays are supported. Arrays retain only messages, types, and valid locations; input values, exception context, and other backend fields are discarded. Configured bearer tokens are redacted before text truncation, control and format characters are normalized, and truncation preserves UTF-8. Output is bounded to 4096 encoded JSON bytes, eight entries, sixteen location segments, 512-byte messages, and 128-byte types and string location segments. Malformed or unsupported details are omitted without changing the error classification. `DoJSONPrivate` returns neither the backend body nor the extracted diagnostic, including through the CLI's installation and Hugging Face authentication paths.

Updated the pre-existing folder-rejection test to the accepted diagnostic contract while retaining its assertion that other backend fields remain hidden. Regression coverage includes JSON mutations, streamed uploads, safe reads, private mutations, long responses, Unicode truncation, token redaction, malformed JSON and locations, unsupported shapes, and server-error omission.

Verification passed:

- `go test ./internal/cli ./internal/httpclient -run '^(TestInvokeAIRejection|TestPrivateMutation)' -count=1`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`
- `git diff --check`

Live verification used a temporary binary built with `go build -o "$probe_root/bediz" ./cmd/bediz`, an isolated configuration directory, and an empty `BEDIZ_TOKEN`:

- `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`.
- `bediz models scan --path /home/nyx/Workspace/bediz/go.mod --url http://127.0.0.1:9090 --json` returned exit status 6, empty stderr, exactly one V1 Result Envelope, and `invokeai_operation_failed` with the following details. This exercised the real backend rejection through the read-only scan endpoint. The temporary binary and configuration directory were removed afterwards.

```json
{
  "status": 400,
  "invokeai_detail": "The search path '/home/nyx/Workspace/bediz/go.mod' could not be scanned"
}
```
