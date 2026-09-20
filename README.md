# Bediz

Bediz is a deterministic Go CLI for controlling a local InvokeAI installation. The V1 contract is defined in [`docs/spec/v1.md`](docs/spec/v1.md).

The current implementation is the first V1 foundation slice. It includes:

- per-user connection configuration with flag, environment, file, and default precedence;
- a bounded HTTP client with bearer authentication, safe-read retries, and unknown-outcome classification for mutations;
- the versioned JSON result and error envelope;
- `doctor`, which checks InvokeAI version compatibility (`>= 6.14.1, < 6.15.0`), required OpenAPI endpoints and invocation fields, Anima component models, and UI synchronization capability.

Generation and management commands will be added in the later V1 slices described by the specification.

## Build and run

```text
go build -o bediz ./cmd/bediz
./bediz doctor
./bediz doctor --json
```

Connection settings resolve in this order:

1. `doctor --url` and `doctor --token`
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
go vet ./...
```
