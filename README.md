# Bediz

Bediz is a deterministic Go CLI for controlling a local InvokeAI installation. The V1 contract is defined in [`docs/spec/v1.md`](docs/spec/v1.md).

The current implementation includes the first three V1 delivery slices:

- per-user connection configuration with flag, environment, file, and default precedence;
- a bounded HTTP client with bearer authentication, safe-read retries, and unknown-outcome classification for mutations;
- the versioned JSON result and error envelope;
- `version`, with concise human output and a stable V1 JSON result envelope;
- `doctor`, which checks InvokeAI version compatibility (`>= 6.14.1, < 6.15.0`), the OpenAPI endpoints and invocation schemas/properties required by implemented capabilities, and readiness for inspection, upload, and Anima text-to-image generation;
- safe installed-model, gallery-image, and queue inspection;
- single-file image upload with supported-version validation, no automatic mutation retry, and `outcome_unknown` reporting when the transport result is inconclusive;
- Anima text-to-image direct execution with deterministic model and component resolution, ordered multi-output seed resolution, graph compilation targeting the tested InvokeAI 6.14.x baseline, safe queue-polling to an Execution Receipt, and `--no-wait` support.

Generation UI Synchronization and Recall are delivery step 4 and are not yet advertised. The remaining management commands will be added in later V1 slices described by the specification.

## Build and run

```text
go build -o bediz ./cmd/bediz
./bediz version
./bediz version --json
./bediz doctor
./bediz doctor --json
./bediz models list --json
./bediz images list --json
./bediz images get IMAGE_NAME --json
./bediz images upload /absolute/path/to/image.png --json
./bediz queue list --json
./bediz queue get ITEM_ID --json
./bediz generate --model "Anima Base 1.0" --prompt "a lighthouse in a storm" --json
```

Inspection commands return normalized Bediz records rather than raw InvokeAI response documents. List output is page-bounded, image selectors use stable InvokeAI image names, and queue listing hydrates only the requested page of lightweight summaries. On supported InvokeAI 6.14.x versions, preserving queue order and total count requires reading the complete lightweight item-ID index; the configured HTTP response-size limit bounds that response, and Bediz never fetches execution graphs while listing.

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

The gate builds the real `bediz` binary and invokes `doctor`, `models list`,
bounded image and queue listing, a self-cleaning image upload round trip, and a
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
