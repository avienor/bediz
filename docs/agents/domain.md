# Domain Docs

This is a single-context repo. Before exploring or changing an area, read:

- `CONTEXT.md` at the repo root, the project's vocabulary.
- The ADRs in `docs/adr/` that touch the area.

When your output names a domain concept (an issue title, a proposal, a test name), use the term `CONTEXT.md` defines and avoid the synonyms it lists. A concept missing from the glossary is either language the project does not use or a real gap; note a gap for `/domain-modeling`.

When your output contradicts an ADR, say so explicitly instead of silently overriding it, for example: _Contradicts ADR-0008 (restrict graph operations and retries), but worth reopening because…_
