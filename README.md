# Bediz

Bediz is a deterministic Go CLI for controlling a local InvokeAI installation. The V1 contract is defined in [`docs/spec/v1.md`](docs/spec/v1.md).

The current implementation includes the first two V1 delivery slices:

- per-user connection configuration with flag, environment, file, and default precedence;
- a bounded HTTP client with bearer authentication, safe-read retries, and unknown-outcome classification for mutations;
- the versioned JSON result and error envelope;
- `version`, with concise human output and a stable V1 JSON result envelope;
- `doctor`, which checks InvokeAI version compatibility (`>= 6.14.1, < 6.15.0`), the OpenAPI endpoints required by implemented capabilities, and readiness for the implemented inspection and upload operations;
- safe installed-model, gallery-image, and queue inspection;
- single-file image upload with supported-version validation, no automatic mutation retry, and `outcome_unknown` reporting when the transport result is inconclusive.

Generation is not advertised as a capability until its implementation slice is complete. It and the remaining management commands will be added in the later V1 slices described by the specification.

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
