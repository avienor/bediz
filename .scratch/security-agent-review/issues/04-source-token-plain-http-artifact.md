# 04 — Refuse a source token for a plain-HTTP artifact URL

Status: resolved

## Problem
`validateArtifactURL` accepts `http://` artifact URLs. With `--token-stdin`, Bediz passes `access_token` to InvokeAI, which then presents it to that artifact host, over plaintext when the URL is `http://`. The spec's plain-HTTP refusal covers only the InvokeAI target, not the artifact origin.

## Proposal
When a source token is present, require an `https` artifact URL, with the same loopback exception as the target. Add one spec sentence.

## Acceptance
`models install --source-type url --source http://host/x.safetensors --token-stdin` fails with `invalid_request` before any mutation.

## Comments

### 2026-09-28 — Confirmed and fixed

Confirmed through `CLI.Run` before changing the implementation. `go test ./internal/cli -run '^TestModelsInstallProtectedURLRejectsPlainHTTPBeforeMutation$' -count=2` failed twice with exit status 0 and an accepted installation mutation. Bediz checked the InvokeAI target's transport, but `validateArtifactURL` accepted non-loopback HTTP artifacts even when the request included a source token.

The shared artifact validator now requires HTTPS or the same loopback exception as the InvokeAI target when a source token is present: case-insensitive `localhost` or a loopback IP address. A direct URL fails as `invalid_request` with exit status 2 before installation. The same validation runs during starter preflight, so an unsupported HTTP starter source or dependency returns `unsupported_capability` before any job is submitted. HTTPS, HTTP loopback sources, and unprotected HTTP installs keep their existing behavior. The V1 spec records the artifact-origin requirement explicitly.

Regression coverage uses the public CLI seam, checks both flags and Request Documents, rejects DNS, private IPv4, IPv6, and misleading `localhost` subdomains, and verifies the allowed loopback and token-free cases. A same-origin HTTP starter and dependency cannot bypass the guard. Running the rejection tests against the original `install.go` through a temporary Go overlay failed for both input forms and the starter path; the fixed implementation passes.

Live verification used temporary binaries, isolated configuration directories, and randomly generated dummy tokens supplied only through standard input. No real credentials were used and no model was installed. Temporary probe files were removed after verification.

- `curl -fsS --max-time 5 http://127.0.0.1:9090/api/v1/app/version` returned `6.14.1`.
- `python /tmp/bediz-source-token-review.CliCS0/live_probe.py /tmp/bediz-source-token-review.CliCS0/bediz-before before` first tried a temporary HTTP receiver on this machine's private address. Bediz accepted job 0, but InvokeAI's private-address download guard rejected it before contacting the receiver. The guard was left enabled; this probe alone did not establish token transmission.
- An HTTP control request to `http://httpbingo.org/bearer` returned 401 without Authorization and 200 with a dummy Bearer token.
- `python /tmp/bediz-source-token-review.CliCS0/public_http_probe.py /tmp/bediz-source-token-review.CliCS0/bediz-before before` ran `bediz-before models install --source-type url --source http://httpbingo.org/bearer --token-stdin --url http://127.0.0.1:9090 --json`. Bediz returned exit 0 and job 1; InvokeAI downloaded the endpoint's 86-byte authenticated response over HTTP. The job then failed because the JSON response was not a model, with no registered model or inventory change. This confirmed the source token reached a non-loopback plain-HTTP host.
- `python /tmp/bediz-source-token-review.CliCS0/public_http_probe.py /tmp/bediz-source-token-review.CliCS0/bediz-after after` ran the same command against the fixed binary. It returned one V1 Result Envelope with `invalid_request`, exit 2, empty stderr, and no new install job or model inventory change. Neither binary exposed the dummy token in its output.
- After confirming the install registry contained only the two failed probe jobs (0 and 1), `DELETE /api/v2/models/install` removed them; a subsequent `GET /api/v2/models/install` returned an empty list. No pre-existing job was removed.

Verification passed:

- `go test ./internal/cli ./internal/models -run 'TestModelsInstall(ProtectedURL|AllowsLoopback|ProtectedStarter)|TestInstallProtectedURL|TestProtectedStarter' -count=1`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`
- `git diff --check`
