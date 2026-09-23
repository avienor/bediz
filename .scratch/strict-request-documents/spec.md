# Strict Request Documents

## Problem

Every command that accepts `--request` reads its Request Document through one shared loader. The operation commands (`generate`, `recall`, `models install`, and `upscale`) are the ones where a lenient document reaches InvokeAI as a mutation; the read commands (such as `models list`, `queue`, and `images`) share the same loader and the same rules. That loader is lenient in three ways that V1 §8.1 ("Unknown request fields are validation errors. Bediz does not ignore misspelled or inapplicable settings.") does not intend:

- **Case-folded member names:** `{"MODEL": "m"}` is accepted as `model`.
- **Duplicate members:** `{"model": "a", "model": "m"}` is accepted, and the last value silently wins.
- **Explicit nulls:** outside `upscale`, `{"seed": null}` is accepted as an absent field and silently acquires a default. `upscale` already rejects explicit nulls (V1 §13) through its own decoding step.

Each case passes local validation and reaches InvokeAI.

## Accepted behavior

The following applies to every Request Document, at every nesting depth:

- Member names match the documented names exactly, including case. A name that differs only in case is an unknown field.
- A repeated member name is `invalid_request`.
- An explicit `null` member value or list element is `invalid_request`. An optional field is omitted, never null.
- A document that is not exactly one JSON value remains `invalid_request`.
- A document that is not a JSON object, including a bare `null`, is `invalid_request`.

All of these checks fail with `invalid_request` (exit status 2) before any network request. Valid documents keep their current meaning. Operation flags are not affected.

## Out of scope

- The connection configuration file. It is not a Request Document, although it uses a similar lenient decoder.
- Error message wording. Error codes and exit statuses are the contract.

## Tickets

1. `issues/01-exact-request-document-members.md`
2. `issues/02-reject-explicit-null-request-values.md`
3. `issues/03-reject-null-list-elements.md`
4. `issues/04-request-documents-are-objects.md`

Both tickets start after the `generative-upscale` pull request is merged and this branch is rebased onto `master`, so the `upscale` path is covered.
