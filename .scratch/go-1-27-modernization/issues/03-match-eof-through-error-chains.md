# 03: Match EOF through error chains at stream boundaries

**What to build:** Apply the Modern Go Guidelines `errors_is` rule at Bediz's request-document and upload-content stream boundaries. End-of-input remains a successful terminal condition where it is already accepted, while malformed input, multiple JSON values, and non-EOF read failures retain their current structured error behavior.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — the code change is mechanical, but review should confirm that canonical Request Document validation is unchanged.

**Verification gate:** Tests cover one valid Request Document, a trailing second value, malformed JSON, a small readable upload, and a closed-file or equivalent non-EOF upload read failure. The same CLI error codes and exit statuses are observed before and after the refactor, and all project-defined Go checks pass.

**Escalate when:** Wrapped EOF would cause Bediz to accept a Request Document or upload that the V1 contract currently rejects, a decoder exposes more than one semantic value, or preserving behavior requires changing canonical Request Document rules.

**Status:** done

- [x] EOF sentinel checks use error-chain-aware matching at both affected stream boundaries.
- [x] Exactly-one-value Request Document validation remains strict.
- [x] Upload file validation and content detection retain their current user-visible failures.
- [x] Focused tests and all repository verification commands pass.

## Comments

Implementation:

- `internal/cli/cli.go` (`loadRequestDocument`): the trailing-value check now reads `if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) { return nil } else if err != nil { … }` with `return errors.New("request document must contain exactly one JSON value")` as the fallthrough. The three outcomes and both message strings are unchanged; the shape now mirrors the existing `ensureEOF` helper in `internal/config/config.go`, so both stream boundaries read the same way. `DisallowUnknownFields` and the first strict decode are untouched.
- `internal/images/images.go` (`uploadContentType`): `err != io.EOF` became `!errors.Is(err, io.EOF)`; `errors` was added to the imports.
- Repo-wide audit: `grep` for `==`/`!=` against `io.EOF`, `os.Err*`, `context.Canceled`, `context.DeadlineExceeded`, `http.Err*`, and `fs.Err*` returns no matches, so no raw sentinel comparison remains anywhere.

Tests (public seams only — `package cli_test` driving `cli.New`, asserting argv, exit status, stderr, and the stdout envelope):

- `TestModelsListRejectsNonCanonicalRequestDocuments` gained a `malformed JSON` row (`{"schema_version":1`), joining the existing `unknown field` and `trailing value` rows; one valid Request Document stays covered by `TestModelsListAcceptsRequestDocument` and `TestModelsListAcceptsRequestDocumentFromStandardInput`.
- New `TestImagesUploadRejectsUnreadableFileContentBeforeConnecting` uploads `/proc/self/mem`: a regular file whose reads fail with EIO at offset zero, i.e. the ticket's "closed-file or equivalent non-EOF upload read failure". It asserts exit status 2, `invalid_request`, operation `images.upload`, and empty stderr, and it skips on non-Linux platforms. A small readable upload stays covered by `TestImagesUploadJSONUploadsOneValidatedLocalFile` and `internal/images/images_test.go`.
- The first draft of the new test also asserted the human-readable message prefix `read upload file:`; independent review flagged that as wording coupling, since spec v1.md §6 makes the code the stable contract and the message human-readable detail. Dropped — the sibling tests assert codes only, and the read boundary stays uniquely identified because the input opens and stats as a regular file, so any swallowed read error reaches the connection attempt instead of `invalid_request`.

Behavior preservation evidence (throwaway, not committed): old and new binaries were built from the same tree with only `internal/cli/cli.go` and `internal/images/images.go` stashed, then run against 11 CLI scenarios — valid request document, trailing second value, trailing value after whitespace, malformed JSON, empty stdin, unknown field, trailing scalar, unreadable regular file, readable small file, missing file, directory path. Exit status, stdout envelope, and stderr were byte-identical for every scenario.

Mutation evidence (throwaway copy of the tree, not committed): with the request-document EOF branch weakened to accept any non-error as end of input, the `trailing value` case fails (exit 5 instead of 2); with the upload read check removed, the new upload test fails (exit 5 instead of 2). Both tests therefore fail on the plausible regression each one defends.

Verification (Go 1.27, module `go 1.27.0`, fresh `-count=1` runs):

```text
go test -count=1 ./...        ok (all packages)
go test -race -count=1 ./...  ok (all packages)
go vet ./...                  clean
go mod verify                 all modules verified
```

Independent review (two axes, uncommitted diff):

- Spec axis: no missing, partial, or unrequested behavior. Both named boundaries use `errors.Is`; all five gate scenarios are covered; canonical Request Document validation stays strict (unknown fields rejected, a decoded trailing value still returns the exactly-one-value error); the escalation trigger does not fire because neither `encoding/json` nor `os.File` wraps `io.EOF` at these sites (`io.ErrUnexpectedEOF` is a distinct sentinel that `errors.Is(·, io.EOF)` does not match), corroborated by the byte-identical scenario run.
- Standards axis: no documented-standard violations. Two P3 judgement calls: the message-prefix coupling (acted on, see above) and the `else if` after a returning branch (kept — `internal/config/config.go:203` uses the identical shape for the same decode-then-check problem, and no linter or documented standard covers control-flow shape). No remaining `io.EOF` sentinel comparison anywhere; the new test runs through `t.Context()` and stays at the CLI seam.
