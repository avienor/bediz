# 01 — Name the offending field in `invalid_request` details

Status: resolved

## Problem
`skills/bediz/SKILL.md` exit-2 row: "Fix the field named in `error.details`". Only profile-setting errors set `details.field` (`internal/cli/remote.go:205`). All other `InvalidRequestError`s map with `nil` details (`remote.go` `classifyRemote`). Observed:

```
{"code":"invalid_request","message":"decode request document: json: unknown field \"widht\""}
{"code":"invalid_request","message":"width and height must be supplied together or both omitted"}
```

Agents must parse prose to find the field.

## Proposal
Give `operation.InvalidRequestError` an optional `Field` (and `Reason`), emit `details.field` (JSON-pointer or request-document key) when known, and do the same for document decode errors (unknown field / type mismatch).
Update spec error section plus the SKILL exit table, or change the skill wording if the product chooses not to.

## Acceptance
CLI-seam tests: unknown field, width without height, and a bad scheduler each return `details.field` naming the key.

## Comments

### 2026-09-28 — Confirmed and fixed

Confirmed through the public CLI seam: the unknown-member regression test failed with `invalid_request details=map[string]interface {}(nil)`. Dimension-pair and scheduler tests reproduced the same missing detail before their fixes. Domain errors carried no field metadata, document decoding used legacy error reporting that discarded typed JSON locations, and the CLI discarded details from document failures.

Added optional `operation.InvalidRequestError.Field` and preserved typed document field locations through the CLI. Unknown members, type mismatches, duplicate members, and explicit nulls now report dotted member paths, including zero-based list indexes. A generation request supplying only width reports the missing `height` key; only height reports `width`. Unsupported Anima, SDXL, and FLUX.1 schedulers report `scheduler`. Flags and Request Documents use the same keys. Profile Document decoding uses the same details. The V1 error contract and canonical skill now document the optional field and the fallback when no field is identified.

Verification passed:

- `go test ./internal/cli -run '^TestInvalidRequest' -count=1`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`
- `python3 /home/nyx/.codex/skills/.system/skill-creator/scripts/quick_validate.py skills/bediz`
- `git diff --check`

Live verification used a temporary binary built from this worktree with `go build -o <temporary-directory>/bediz ./cmd/bediz` and an isolated user configuration directory:

- `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned InvokeAI `6.14.1`.
- `bediz models list --request - --url http://127.0.0.1:9090 --json`, with `{"schema_version":1}` on stdin, identified installed Anima main key `06409299-d28f-4c00-8416-4d23cb1b8358`.
- `bediz generate --request - --no-wait --url http://127.0.0.1:9090 --json` ran separately with the three documents below. Every run returned exit status 2, empty stderr, exactly one V1 Result Envelope, and `invalid_request` with the expected field. No generation was submitted.

```json
{"schema_version":1,"model":"06409299-d28f-4c00-8416-4d23cb1b8358","positive_prompt":"validation check","widht":512}
{"schema_version":1,"model":"06409299-d28f-4c00-8416-4d23cb1b8358","positive_prompt":"validation check","width":512}
{"schema_version":1,"model":"06409299-d28f-4c00-8416-4d23cb1b8358","positive_prompt":"validation check","scheduler":"bad-scheduler"}
```

The respective `error.details.field` values were `widht`, `height`, and `scheduler`.
