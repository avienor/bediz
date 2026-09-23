# 02: Install a protected URL with a temporary token

**What to build:** A protected generic URL can use `--token-stdin` as a temporary source access token. The token is execution input that Bediz never persists or emits in a Request Document, local configuration, result, receipt, or diagnostic. The tested InvokeAI 6.14.1 generic POST accepts `access_token` as a query parameter: Bediz sends it only to the configured InvokeAI target and redacts the complete request URL from every error and diagnostic path. InvokeAI writes that token in plaintext to a temporary install marker while a remote download runs, removes the marker on normal terminal handling, and can retain it for paused or interrupted work; its or a proxy's access logs may also contain the query. This accepted upstream lifecycle must be documented without promising end-to-end non-persistence. Reject token-bearing installation over non-loopback plain HTTP before any remote call. A Request Document may come from a file with token input from standard input; `--request -` together with `--token-stdin` fails before any remote call because both require the same input stream. The InvokeAI connection token remains separate from this source token.

**Blocked by:** 01: Install a model from an exact URL and inspect its job.

**Execution route:** `frontier-owned` — source credentials are sensitive, and the tested InvokeAI API receives the access token as a request parameter.

**Verification gate:** Public-interface and transport tests cover successful protected installation, missing or empty token, incompatible standard-input options, insecure non-loopback target rejection, authentication failure, lost mutation response, and single submission. Use a sentinel token to assert that standard output, standard error, structured errors, rendered URLs, network/unknown-outcome error paths, diagnostics, and test-visible client failures do not contain it; verify the native POST receives the token only in its required parameter. Document the tested InvokeAI 6.14.1 marker lifecycle in V1 and verify no Bediz-owned storage is introduced. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`; perform a narrow live protected-source check only with an authorized disposable fixture, otherwise record unavailability.

**Review gate:** Not required by this route.

**Escalate when:** Bediz leaks a token through an HTTP/client error, diagnostic, or result; the configured InvokeAI/proxy exposure is unacceptable for the intended deployment; live authentication differs from the tested contract; scope expands to persistent credential storage; or repeated repairs fail for the same reason.

**Permanent records:** V1 spec, ADR 0018, and tests — preserve Bediz's no-storage boundary, InvokeAI's plaintext temporary marker lifecycle, the standard-input conflict, secure target requirement, native query-parameter exposure, and secret-free failure behavior. A new ADR is required if implementation chooses a different credential-transport architecture.

**Status:** done

- [x] A protected generic URL produces an inspectable installation job using one temporary source token.
- [x] Source tokens never appear in Bediz output or durable state, and conflicting standard-input modes fail before mutation.
- [x] An inconclusive installation response returns `outcome_unknown` without automatic retry.

Live protected-source verification was unavailable: local InvokeAI 6.14.1 was reachable, but no authorized disposable protected artifact fixture was supplied.
