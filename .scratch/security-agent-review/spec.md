# Security, code, and agent-usability review — v1.0.0-rc.1 (e913df9)

Status: review complete, no code changed
Date: 2026-09-25

## Scope and baseline

Read: `internal/httpclient`, `internal/config`, `internal/cli` (cli, auth, remote error mapping), `internal/huggingface`, `internal/models` (install, civitai), `internal/images` (upload, download), `install.sh`, `.github/workflows/release.yml`, `skills/bediz/SKILL.md`, and the token sections of `docs/spec/v1.md`.
Probed a locally built binary against an unreachable target to capture real error envelopes.

- `go vet ./...` — pass
- `go test ./...` — pass
- `govulncheck ./...` (v1.8.0) — no vulnerabilities found
- Not run: `go test -race`, live InvokeAI checks.

## What is already solid

- Mutations are sent once, never follow redirects, and map gateway 502/503/504 to `outcome_unknown`.
- Token-bearing requests go through `DoJSONPrivate`, which removes the URL and backend body from errors.
- Source tokens are refused on a non-loopback plain-HTTP InvokeAI target. Civitai tokens go only to the fixed HTTPS metadata host, and redirects are refused.
- The config file is written atomically with mode 0600 inside a 0700 directory.
- Downloads use a temp file and a hard link, so no file is overwritten. Body sizes are bounded.
- Civitai, Hugging Face, and URL references are validated strictly.
- The release workflow pins actions by SHA, uses least-privilege permissions, and verifies checksums. The installer verifies SHA256SUMS.

## Findings (most severe first)

| # | Area | Severity | Ticket |
| --- | --- | --- | --- |
| 1 | Agent usability | High | [01](issues/01-invalid-request-field-details.md): `invalid_request` has no `details.field`, but the skill tells agents to read it |
| 2 | Agent usability | High | [02](issues/02-surface-invokeai-rejection-detail.md): InvokeAI 4xx rejection text is dropped, so agents cannot self-correct |
| 3 | Security | Medium | [03](issues/03-upload-rejects-non-images.md): `images upload` and upscale `path` send non-image files, such as `~/.ssh/id_rsa`, to InvokeAI |
| 4 | Security | Medium | [04](issues/04-source-token-plain-http-artifact.md): a source token can be attached to a plain `http://` artifact URL |
| 5 | Security | Medium | [05](issues/05-token-in-argv.md): `--token` puts the InvokeAI token in argv and shell history, and there is no stdin alternative for `config set` |
| 6 | Supply chain | Medium | [06](issues/06-installer-unpinned-npx.md): `install.sh` runs `npx -y skills` unpinned; releases have no provenance attestation |
| 7 | Agent usability | Low | [07](issues/07-doctor-unreachable-noise.md): an unreachable `doctor` returns about 10 KB, mostly misleading `missing_properties` |
| 8 | Security | Low | Safe GETs follow redirects with the bearer header. Go strips `Authorization` only on a cross-domain redirect, not on a same-host https→http downgrade. Consider `CheckRedirect` refusing a scheme downgrade when a token is set. No ticket. |

## Suggested order

1 and 2 together: one small change to the error mapping, with the largest agent win. Then 3, then 4. Items 5 and 6 need a product decision because they touch spec §98 and the installer contract.
