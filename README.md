# Bediz

Bediz is a deterministic Go CLI for controlling a local InvokeAI installation. The V1 contract is defined in [`docs/spec/v1.md`](docs/spec/v1.md).

The current implementation includes these V1 capabilities:

- per-user connection configuration with flag, environment, file, and default precedence;
- a bounded HTTP client with bearer authentication, safe-read retries, and unknown-outcome classification for mutations;
- the versioned JSON result and error envelope;
- `version`, with concise human output and a stable V1 JSON result envelope;
- `doctor`, which checks InvokeAI version compatibility (`>= 6.14.1, < 6.15.0`), the OpenAPI endpoints and schemas required by implemented capabilities, and separate readiness for Anima Direct Execution and Parameter Recall;
- safe installed-model, gallery-image, and queue inspection;
- model installation through InvokeAI jobs from exact URLs, Hugging Face repositories with explicit artifact choices, Civitai versions with explicit version and file choices, server paths (in-place registration, or `--move --yes`), and starter models whose catalog dependencies are submitted as separate jobs and skipped when already installed, plus current install-job status inspection;
- protected URL, Civitai, and Hugging Face installs with a temporary `--token-stdin` source token that Bediz never stores or prints;
- Hugging Face authentication through InvokeAI with `auth huggingface status`, `login --token-stdin`, and `logout`;
- single-file image upload with supported-version validation, no automatic mutation retry, and `outcome_unknown` reporting when the transport result is inconclusive;
- Anima text-to-image Direct Execution with deterministic model and component resolution, ordered multi-output seed resolution, graph compilation targeting the tested InvokeAI 6.14.x baseline, safe queue-polling to an Execution Receipt, and `--no-wait` support;
- SD1.5 and SDXL Generative Upscale from an existing InvokeAI image or an absolute local file uploaded once before enqueue, with explicit Tile ControlNet selection, single enqueue, output-dimension verification, and an Execution Receipt;
- manual Anima Parameter Recall and automatic generation and upscale UI Synchronization after enqueue, with the verified `partial` level on stock InvokeAI 6.14.x.

On a compatible 6.14.x installation, `doctor --json` reports `generate` and `upscale` as compatible and `ui_sync.generate` and `ui_sync.upscale` as `partial` when the tested Recall endpoint and patch schema are present. Missing Recall requirements appear under the separate `recall` capability; they do not make Direct Execution incompatible. An unsupported InvokeAI version is not advertised as ready for generation or Recall.

The partial Handoff restores positive and negative prompts, the exact Anima main model, dimensions, steps, and the first output seed in an open InvokeAI browser after Recall is accepted. It does not restore scheduler, guidance, VAE, Qwen3 encoder, output count, or Output Board controls. The visible queue item, result image, metadata, and complete Execution Receipt retain the resolved settings and every output seed. A successful generation carries `ui_sync_partial`; if the Recall patch fails, generation still succeeds with `ui_sync_failed`. Recall API acceptance does not prove that a browser was open to receive the event. V1 does not support `recall --replace` or claim `full` UI Synchronization.

After an accepted upscale enqueue, including `--no-wait`, Bediz sends one Recall patch with the resolved prompts, exact main model, steps, and seed. It does not restore the source image, Spandrel model, scale, creativity, structure, Tile ControlNet, tile size, tile overlap, scheduler, guidance, VAE, or Output Board in the Upscale panel. A successful patch carries `ui_sync_partial`; if Recall fails, the upscale outcome and its Execution Receipt are unchanged and carry `ui_sync_failed`.

Additional management commands will be added in later V1 slices described by the specification.

## Build and run

```text
go build -o bediz ./cmd/bediz
./bediz version
./bediz version --json
./bediz doctor
./bediz doctor --json
./bediz models list --json
./bediz models install --source-type url --source https://example.org/model.safetensors --json
./bediz models install --source-type huggingface --source org/repo --json
./bediz models install --source-type huggingface --source org/repo --artifact https://huggingface.co/org/repo/resolve/main/model.safetensors --json
./bediz models install --source-type starter --source STARTER_SOURCE --json
./bediz models install --source-type civitai --source 'https://civitai.com/models/MODEL_ID?modelVersionId=VERSION_ID' --json
./bediz models install --source-type civitai --source 'https://civitai.com/models/MODEL_ID' --json
./bediz models install --source-type civitai --source VERSION_ID --file-id FILE_ID --json
./bediz models install --source-type path --source /server/models/model.safetensors --json
./bediz models install --source-type path --source /server/models/model.safetensors --move --yes --json
./bediz models status --job-id 0 --json
./bediz auth huggingface status --json
printf '%s' "$HF_TOKEN" | ./bediz auth huggingface login --token-stdin --json
./bediz auth huggingface logout --json
./bediz images list --json
./bediz images get IMAGE_NAME --json
./bediz images upload /absolute/path/to/image.png --json
./bediz images download IMAGE_NAME --output ./image.png --json
./bediz queue list --json
./bediz queue get ITEM_ID --json
./bediz queue wait ITEM_ID [ITEM_ID...] --timeout 10m --json
./bediz queue cancel ITEM_ID --json
./bediz queue clear --yes --json
./bediz boards list --json
./bediz boards get BOARD_ID_OR_NAME --json
./bediz boards create "Board name" --json
./bediz generate --model "Anima Base 1.0" --prompt "a lighthouse in a storm" --json
./bediz upscale --image IMAGE_NAME --model "Juggernaut-XL-v9" --tile-controlnet TILE_MODEL_KEY --scale 2 --json
./bediz upscale --image-path /absolute/path/to/image.png --model "Juggernaut-XL-v9" --tile-controlnet TILE_MODEL_KEY --scale 2 --json
./bediz recall --model "Anima Base 1.0" --prompt "a lighthouse in a storm" --seed 42 --json
```

Inspection commands return normalized Bediz records rather than raw InvokeAI response documents. List output is page-bounded, image selectors use stable InvokeAI image names, and queue listing hydrates only the requested page of lightweight summaries. On supported InvokeAI 6.14.x versions, preserving queue order and total count requires reading the complete lightweight item-ID index; the configured HTTP response-size limit bounds that response, and Bediz never fetches execution graphs while listing.

Model installation accepts an exact HTTP(S) artifact URL without userinfo, query, or fragment. Its job ID identifies only a job in the current InvokeAI registry; after a restart, use `models list` and the InvokeAI install job list before deciding whether to resubmit an uncertain installation. `models status` returns a safe projection of the job currently under that ID.

`./bediz models scan --path /absolute/server/folder --json` lists model files found in a folder on the InvokeAI server and whether each is installed. The command reads server state and does not install anything. The path is resolved on the server, even when Bediz runs on another machine.

`./bediz models delete MODEL_KEY --yes --json` removes one installed model selected by its exact key and returns the key, name, base, type, and format it had. InvokeAI deletes the files of a model in its managed models directory; an in-place registration loses only its record, and its source file stays where it was.

Civitai installation requires an exact version ID or a model page URL with `modelVersionId`. One file is selected directly; among multiple files, exactly one primary is selected. Otherwise the JSON result returns numeric file choices; resubmit the same version with `--file-id` or `source.file_id`. A model page URL without `modelVersionId` never picks a version, even when only one exists: the JSON result returns numeric version choices to resubmit as the exact version reference. A direct Civitai download URL is a `url` source and follows the direct URL validation rules.

Each operation also accepts a schema-versioned request document from a file or standard input. Operation arguments and flags cannot be mixed with `--request`:

```text
printf '%s\n' '{"schema_version":1,"offset":0,"limit":20}' |
  ./bediz images list --request - --json
```

Connection settings resolve in this order:

1. `--url` and `--token` on the selected remote command
2. `BEDIZ_URL` and `BEDIZ_TOKEN`
3. the per-user Bediz configuration file
4. `http://127.0.0.1:9090`

Manage persisted settings with:

```text
bediz config get
bediz config set --url http://127.0.0.1:9090
bediz config set --token TOKEN
bediz config set --unset-token
```

Tokens are never included in command output. The configuration file is written atomically with user-only permissions.

## Verify

```text
go test ./...
go test -race ./...
go vet ./...
go mod verify
```

The ordinary checks do not contact InvokeAI. To display the live-gate status
explicitly without requesting a live run, use:

```text
go test -count=1 -v ./e2e
```

This reports `live E2E: NOT REQUESTED` when `BEDIZ_E2E_URL` is
unset. To run the opt-in gate against the local InvokeAI 6.14.1 baseline:

```text
BEDIZ_E2E_URL=http://127.0.0.1:9090 go test -count=1 -v ./e2e
```

To also verify URL model installation and job status against that baseline,
run the separate opt-in gate:

```text
BEDIZ_E2E_URL=http://127.0.0.1:9090 BEDIZ_E2E_MODEL_INSTALL=1 go test -count=1 -v ./e2e -run '^TestLiveURLModelInstall$'
```

This downloads a public, revision-pinned 4.8 MB SD1 LoRA from Hugging Face,
checks `models install`, `models status`, and `models list` through the real
binary, then removes only the model it installed. It requires outbound HTTPS
access from InvokeAI. It skips unless both environment variables are set.

The gate builds the real `bediz` binary and invokes `doctor`, `models list`,
bounded image, queue, and board listing, a board lookup, a self-cleaning image upload round trip, and a
self-cleaning Anima generation round trip through the process boundary. The upload case creates a unique 2-by-2 PNG,
verifies the same normalized Image Reference through `images upload`,
`images get`, and `images list`, then deletes exactly that image through the
InvokeAI backend API. A final `images get` must return the V1 `not_found`
envelope. The generation case submits a canonical request with an explicit
seed, waits for queue completion, validates the Execution Receipt and accessible
Image Reference, and cleans up the resulting image through the InvokeAI backend API
before the gate reports `live E2E: VERIFIED`.

Every case checks process exit statuses, one V1 JSON result envelope on
standard output, empty standard error, supported-version readiness, normalized
result fields, page bounds where the public command supports them, and secret
redaction. Upload assertions also cover image origin and category, exact
dimensions, intermediate status, board absence, metadata-related fields, and
an empty backend metadata record, and absolute image URLs. Cleanup failures
name the exact test-created image so it can be removed manually without
touching unrelated resources; the fixture filename and SHA-256 digest remain
in failed-test output as inspection evidence when an upload outcome is unknown.
Empty pre-existing model, image, and queue collections are valid. The URL must not contain
credentials, a query, or a fragment; an authentication requirement causes the
gate to fail instead of accepting or printing a real token. Use `-count=1` as
shown so a live result is never served from the Go test cache.

## Release

Maintainers build a release from a clean checkout of its version tag:

```text
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

```text
git tag v1.0.0
git push origin v1.0.0
```

Pushing a `v*` tag runs the release workflow (`.github/workflows/release.yml`). It checks the tag with `-check`, then runs `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` with the pinned toolchain. It builds the archives with the release command and publishes them and `SHA256SUMS` as a GitHub Release for the tag. A tag with a prerelease suffix, such as `v1.0.0-rc.1`, is published as a prerelease. When any step fails, nothing is published. Merges to `master` publish nothing. The workflow never replaces an existing release. If publishing fails after the release was created, GitHub can keep a draft release for the tag; delete that draft before re-running the workflow. To check a published release, download its assets and run `sha256sum -c SHA256SUMS`. A local build of the same tag produces the same `SHA256SUMS`.

A `go install github.com/avienor/bediz/cmd/bediz@vX.Y.Z` build reports `vX.Y.Z` without a commit or date. A local `go build` reports the version and VCS data that Go records, and `dev` when there is none.

## License

Bediz is licensed under the [Apache License 2.0](LICENSE).
