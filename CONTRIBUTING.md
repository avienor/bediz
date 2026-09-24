# Contributing

Read [AGENTS.md](AGENTS.md) for the project's sources of truth, architecture rules, and workflow. Product behavior is defined in [docs/spec/v1.md](docs/spec/v1.md), and the vocabulary in [CONTEXT.md](CONTEXT.md).

## Build

```sh
go build -o bediz ./cmd/bediz
./bediz version
```

A local `go build` reports the version and VCS data that Go records, and `dev` when there is none. A `go install github.com/avienor/bediz/cmd/bediz@vX.Y.Z` build reports `vX.Y.Z` without a commit or date.

## Verify

```sh
go test ./...
go test -race ./...
go vet ./...
go mod verify
```

The ordinary checks do not contact InvokeAI. To display the live-gate status explicitly without requesting a live run, use:

```sh
go test -count=1 -v ./e2e
```

This reports `live E2E: NOT REQUESTED` when `BEDIZ_E2E_URL` is unset. To run the opt-in gate against the local InvokeAI 6.14.1 baseline:

```sh
BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e
```

To also verify URL model installation and job status against that baseline, run the separate opt-in gate:

```sh
BEDIZ_E2E_URL=http://127.0.0.1:9090 BEDIZ_E2E_MODEL_INSTALL=1 go test -count=1 -v ./e2e -run '^TestLiveURLModelInstall$'
```

This downloads a public, revision-pinned 4.8 MB SD1 LoRA from Hugging Face, checks `models install`, `models status`, and `models list` through the real binary, then removes only the model it installed. It requires outbound HTTPS access from InvokeAI. It skips unless both environment variables are set.

The gate builds the real `bediz` binary and invokes `doctor`, `models list`, bounded image, queue, and board listing, a board lookup, a self-cleaning image upload round trip, and a self-cleaning Anima generation round trip through the process boundary. The upload case creates a unique 2-by-2 PNG, verifies the same normalized Image Reference through `images upload`, `images get`, and `images list`, then deletes exactly that image through the InvokeAI backend API. A final `images get` must return the V1 `not_found` envelope. The generation case submits a canonical request with an explicit seed, waits for queue completion, validates the Execution Receipt and accessible Image Reference, and cleans up the resulting image through the InvokeAI backend API before the gate reports `live E2E: VERIFIED`.

Every case checks process exit statuses, one V1 JSON result envelope on standard output, empty standard error, supported-version readiness, normalized result fields, page bounds where the public command supports them, and secret redaction. Upload assertions also cover image origin and category, exact dimensions, intermediate status, board absence, metadata-related fields, and an empty backend metadata record, and absolute image URLs. Cleanup failures name the exact test-created image so it can be removed manually without touching unrelated resources; the fixture filename and SHA-256 digest remain in failed-test output as inspection evidence when an upload outcome is unknown. Empty pre-existing model, image, and queue collections are valid. The URL must not contain credentials, a query, or a fragment; an authentication requirement causes the gate to fail instead of accepting or printing a real token. Use `-count=1` as shown so a live result is never served from the Go test cache.

Before a live check, read [docs/agents/live-verification.md](docs/agents/live-verification.md).

## Release

Maintainers build a release from a clean checkout of its version tag:

```sh
git checkout v1.0.0
go run ./tools/release v1.0.0
```

The command writes `bediz_<version>_linux_amd64.tar.gz`, `bediz_<version>_darwin_arm64.tar.gz`, `bediz_<version>_windows_amd64.zip`, and `SHA256SUMS` to `dist/<version>`, or to the directory given with `-out`, which must not exist yet. It refuses to run unless all of these hold:

- the version has the form `vX.Y.Z` or `vX.Y.Z-<prerelease>`;
- a tag with that exact name points to the checked-out commit;
- the working tree has no modified files and no untracked files that Git does not ignore;
- `metadata.bediz-version` in `skills/bediz/SKILL.md` equals the version;
- `go env GOVERSION` equals the `toolchain` directive in `go.mod`.

`go run ./tools/release -check vX.Y.Z` checks these conditions without building anything.

The command must run on Linux amd64, macOS arm64, or Windows amd64. It checks the finished archive for that host by running `bediz version --json` and comparing the reported version, commit, and date with the tag. Two runs on the same tag with the pinned toolchain produce byte-identical archives, so a published release can be rebuilt and compared with `SHA256SUMS`. See [ADR-0021](docs/adr/0021-build-releases-reproducibly-with-a-repository-local-command.md) for the fixed build and archive settings.

To publish a release, set `metadata.bediz-version` in `skills/bediz/SKILL.md` to the new version, commit, and push a tag with that name:

```sh
git tag v1.0.0
git push origin v1.0.0
```

Pushing a `v*` tag runs the release workflow (`.github/workflows/release.yml`). It checks the tag with `-check`, then runs `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` with the pinned toolchain. It builds the archives with the release command and publishes them and `SHA256SUMS` as a GitHub Release for the tag. A tag with a prerelease suffix, such as `v1.0.0-rc.1`, is published as a prerelease. When any step fails, nothing is published. Merges to `master` publish nothing. The workflow never replaces an existing release. If publishing fails after the release was created, GitHub can keep a draft release for the tag; delete that draft before re-running the workflow. To check a published release, download its assets and run `sha256sum -c SHA256SUMS`. A local build of the same tag produces the same `SHA256SUMS`.
