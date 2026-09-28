# 05 — Offer a stdin path for the InvokeAI token

Status: resolved

## Problem
`--token` on every remote command and `config set --token` put the bearer token in argv (visible in `ps`, `/proc/*/cmdline`, shell history, and agent transcripts). `auth huggingface login` and `models install` already use `--token-stdin`, so this is inconsistent.

## Proposal
Add `config set --token-stdin`. Recommend `BEDIZ_TOKEN` or stored config in the skill and help text. Optionally deprecate `--token` on remote commands in a later spec version.

## Acceptance

- `config set --token-stdin` stores a non-empty InvokeAI bearer token read from standard input without requiring it in argv.
- Surrounding whitespace is trimmed. Empty input, read failures, and conflicts with `--token` or `--unset-token` return `invalid_request` (exit 2) without changing the stored configuration.
- Flag conflicts are rejected before reading standard input. Success, failure, and argument-error output do not echo the token.
- Remote help, README, and the canonical Agent Skill recommend `BEDIZ_TOKEN` or stored configuration. Existing `--token` support and connection precedence remain compatible.

## Comments

### 2026-09-28 — Confirmed and fixed

Confirmed at the public CLI seam before implementation: `go test ./internal/cli -run '^TestConfigSetTokenStdinPersistsConnectionToken$' -count=2` failed twice with `invalid_request`, exit 2, and `unknown flag: --token-stdin`. A temporary binary built from the original CLI through a Go overlay also rejected the flag. A dummy `--token` sent to a temporary local HTTP receiver was visible in `/proc/<pid>/cmdline`, confirming the argv exposure independently of command output.

The fix adopts the ticket's additive stdin proposal. `config set --token-stdin` reads and trims the token, then uses the existing atomic configuration writer. It can set or unset the URL at the same time. Conflicting token flags fail before stdin is consumed, including an explicitly empty `--token`. Empty and partially failed reads preserve the original URL and token. Configuration argument errors use a credential-safe message: a regression test first demonstrated that `--token-stdin=TOKEN` echoed the accidental token through the boolean flag parser. Both human and JSON output now omit that parser text.

The V1 spec, README, remote flag help, and canonical Agent Skill document the safe input path. `--token` is retained for V1 compatibility; choosing it still exposes its value in argv. Deprecation is deferred as proposed. No configuration schema or connection precedence changed, and no source-token or Hugging Face credential behavior changed.

Regression coverage uses `CLI.Run` / `CLI.NewWithIO`, its stdout, stderr, exit status, the persisted configuration through its public loader, and an HTTP receiver verifying the exact stored bearer header. It covers JSON and human output, empty and whitespace-only input, partial read errors, flag conflicts, accidental credentials in arguments, safe help guidance, and reuse of stored credentials by a later remote command.

Process and live verification used temporary binaries, isolated `XDG_CONFIG_HOME` directories, and randomly generated dummy tokens. Real credentials were not read or printed. Temporary artifacts were removed; user configuration and InvokeAI resources were unchanged.

- `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`.
- `go build -overlay <temporary-overlay.json> -o <temporary>/bediz-before ./cmd/bediz` built the original CLI for the missing-flag and argv probes.
- `go build -o <temporary>/bediz ./cmd/bediz` built the fixed binary.
- `bediz config set --url http://127.0.0.1:9090 --token-stdin --json`, with the dummy token supplied only through stdin, succeeded with one V1 Result Envelope and empty stderr. The token was absent from `/proc/<pid>/cmdline` and output; the stored token exactly matched the trimmed input. File and directory modes were `0600` and `0700`. The initial combined probe stopped at an immediate argv-read assertion; the final probe waited for the expected process arguments to become observable before checking them and passed.
- `bediz models list --url <temporary-local-receiver> --json` sent the exact stored bearer token without `--token`.
- `bediz models list --json` and `bediz models list --request - --json` succeeded against local InvokeAI 6.14.1 with the stored dummy token. The latter consumed `{"schema_version":1}` on stdin, confirming that stored authentication leaves stdin available for a Request Document. Both returned exit 0, one V1 Result Envelope, empty stderr, and no token. These were read-only checks; the local single-user baseline does not validate the dummy bearer token, so the separate HTTP receiver verifies the actual header value.

Verification passed:

- `go test ./internal/cli -run '^TestConfigSetToken|^TestConnectionTokenHelpRecommendsSafeInput$' -count=1`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`
- `git diff --check`
