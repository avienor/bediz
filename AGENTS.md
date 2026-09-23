# AGENTS.md

## Sources of truth

- Before changing commands, public JSON, compatibility, capability coverage, or other product behavior, read `docs/spec/v1.md`.
- Before changing an accepted design, read the relevant record in `docs/adr/`. Update the spec or ADR explicitly when the accepted behavior changes.
- Before naming public commands, types, fields, errors, or documentation concepts, read `CONTEXT.md` and use its canonical terms.

## Agent skills

### Issue tracker

Issues and specs are tracked as local Markdown under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Domain docs

This is a single-context repo with `CONTEXT.md` at the root and ADRs under `docs/adr/`. See `docs/agents/domain.md`.

## Revisions and discoveries

- Treat specs, ADRs, tickets, and tests as revisable records of the current agreement. Evidence that invalidates an assumption is a reason to revise the agreement, not preserve it in code.
- When implementation or live verification contradicts an assumption, pause the affected slice, distinguish an implementation defect from a product or design change, and report the impact on code, tests, docs, and tickets. Ask the user to decide changes to product behavior, scope, or hard-to-reverse trade-offs.
- Keep the sources of truth aligned with the decision: update `CONTEXT.md` for terminology, the V1 spec for behavior, tests for the accepted contract, and affected tickets or dependencies. Supersede an accepted ADR with a new ADR instead of rewriting its history.

## Architecture

- Keep Cobra at the CLI adapter seam. Domain validation, request resolution, graph compilation, and InvokeAI behavior belong in parser-independent modules.
- Compile operation flags and JSON request documents into the same typed operation values.
- Preserve deterministic behavior: reject unknown or inapplicable fields and return structured choices instead of selecting ambiguously.
- Keep InvokeAI as the source of truth for jobs and audit data; do not introduce a duplicate local store.

## Public contracts

- In `--json` mode, write exactly one final V1 result envelope to stdout. Send diagnostics and progress to stderr.
- Preserve the documented exit statuses and stable structured error codes.
- Resolve connection settings in the precedence defined by the V1 spec. Keep tokens out of logs, output, receipts, and diagnostics.
- Send each mutation once. If its transport result is inconclusive, return `outcome_unknown`.
- Run graph-producing operations only for capability-matrix combinations and InvokeAI versions tested as supported.

## Development workflow

- For behavior changes and bug fixes, follow `.agents/skills/tdd/SKILL.md`. Agree on the public seam, then work one failing behavior test to the minimum passing implementation at a time.
- Test through public module interfaces. At the CLI seam, assert argv, stdout, stderr, exit status, and externally visible state; keep Cobra command nodes, private helpers, and internal call counts outside the test surface.
- Keep changes within the requested vertical slice; defer unrelated refactors and speculative abstractions.

## Verification

A Go change is complete when all applicable commands pass:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go mod verify`

For InvokeAI integration changes, also exercise the narrowest affected behavior against the supported local InvokeAI baseline when available, and report when live verification was unavailable. Before a live check, model installation, or InvokeAI UI observation, read `docs/agents/live-verification.md`.

## Git and pull requests

- Use `feature/<short-kebab-name>` for new branches unless the user specifies a name.
- Keep branch names, commit messages, PR titles, and PR descriptions tool-neutral. Describe the change and its outcome without assistant or tool branding.
- Include the exact verification commands and any live InvokeAI check in the PR description.
