# 03: Manage Hugging Face authentication through InvokeAI

**What to build:** `auth huggingface status`, `login --token-stdin`, and `logout` expose InvokeAI's Hugging Face token support. Bediz keeps no Hugging Face token store. Login consumes a token only from standard input and rejects a configured non-loopback plain-HTTP target before sending the credential; status and logout never return the token. Each command provides human output or exactly one V1 JSON Result Envelope with stable error and exit behavior. `doctor` registers read-only status against compatible endpoint checks and login/logout only for the tested InvokeAI range with their exact mutation methods.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — login and logout change remote authentication state and secret-handling failures have a high cost.

**Verification gate:** Public-interface tests cover the three commands, valid/invalid/unknown backend status, empty token, rejected login, failed logout, one final envelope, exit statuses, and redaction of a sentinel token from all outputs and errors. A transport fixture rejects token-bearing login over non-loopback plain HTTP before transmission while allowing the documented loopback and HTTPS paths. Transport tests prove login and logout mutations are not retried and report `outcome_unknown` when inconclusive. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`. Read-only live status may be checked on local InvokeAI 6.14.1; live login/logout require a disposable authorized token and restoration of the original state, or are reported unavailable.

**Review gate:** Not required by this route.

**Escalate when:** InvokeAI's authentication state differs from its tested API, token data leaks, a live check would alter another user's session, the design requires Bediz-owned credential storage, or repeated repairs fail.

**Permanent records:** V1 spec and tests — define normalized status/result and unknown-outcome behavior. Current `CONTEXT.md` terms and ADRs remain sufficient unless the ownership boundary changes.

**Status:** done

- [x] Status reports InvokeAI's Hugging Face state without exposing a token.
- [x] Login and logout update InvokeAI's state once and provide parseable success or structured failure.
- [x] Bediz stores no Hugging Face credential and does not register unverified support.

Live read-only status and `doctor` capability checks passed against local InvokeAI 6.14.1. Live login/logout were unavailable without a disposable authorized token and safe restoration of the original state; mutation behavior was verified with transport fixtures.
