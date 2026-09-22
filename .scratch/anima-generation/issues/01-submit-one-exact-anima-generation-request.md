# 01: Submit one exact Anima Generation Request

**What to build:** Make `generate --no-wait` accept the approved schema-version-1 Generation Request through either convenience flags or a Request Document, resolve the explicitly supplied Anima main model and components to exact Model Keys, compile the tested InvokeAI 6.14.x Anima Execution Graph, submit it once to the `default` queue, and return an accepted Execution Receipt. The graph contains the Anima model loader, positive and negative text conditioning, seed, denoise, metadata, and image decode path needed for one output. Generated node identifiers may vary, but node types, typed edges, resolved values, and metadata must match the versioned fixture. The capability remains unadvertised until ticket 05.

**Blocked by:** None (can start immediately).

**Execution route:** `frontier-owned` — this establishes the first graph-producing adapter and the sensitive public Generation Request and accepted Execution Receipt contracts.

**Verification gate:** Public CLI tests prove that flags and Request Documents compile to the same operation, mixed inputs and unknown fields fail before network access, JSON mode writes exactly one Result Envelope, and the fake InvokeAI server receives exactly one enqueue mutation with the versioned graph fixture. An inconclusive enqueue response must produce `outcome_unknown` without retry. A live InvokeAI 6.14.1 check must accept the graph and return ordered queue identifiers. All applicable project verification commands must pass.

**Escalate when:** The live OpenAPI or native Anima graph contradicts the approved fixture; the model inventory cannot provide the full identifiers required by graph nodes; enqueue response ordering is insufficient for later seed mapping; implementing the accepted JSON contract requires changing another public operation; or two repair attempts fail for the same underlying reason.

**Permanent records:** V1 spec and tests — record the exact Generation Request fields, accepted Execution Receipt shape, Anima graph contract, and mutation safety evidence. No new ADR or terminology change is expected.

**Status:** done

- [x] `generate --no-wait` supports `--model`, `--prompt`, `--negative-prompt`, dimensions, steps, scheduler, guidance, seed, `--board`, `--vae`, and `--qwen3-encoder`, with `output_count` restricted to one for this first tracer bullet.
- [x] A schema-version-1 Request Document supports the equivalent typed fields, rejects unknown fields, and cannot be mixed with operation flags.
- [x] The exact Anima main model, VAE, and Qwen3 encoder selectors resolve before compilation; incompatible selectors fail before enqueue.
- [x] The pure compiler emits the tested 6.14.x node and edge topology plus complete generation metadata for the explicit request.
- [x] Enqueue is sent once, never automatically retried, and its accepted result includes `submitted_request`, explicit `resolved_settings`, `queue`, and an empty `outputs` list.
- [x] JSON stdout, human output, structured error codes, and exit statuses follow the V1 result contract.
- [x] The narrow behavior tests and `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify` pass; live enqueue evidence is recorded.

## Comments

Implemented the first exact Anima Direct Execution tracer through the public
`generate --no-wait` CLI seam. Convenience flags and the schema-version-1
Request Document compile to the same typed Generation Request. Validation,
supported-version enforcement, exact Anima main/VAE/Qwen3 component resolution,
and complete InvokeAI Model Identifier checks all occur before graph compilation
and enqueue.

The pure compiler emits the versioned InvokeAI 6.14.x fixture containing the
Anima loader, positive and negative conditioning paths, seed, denoise, complete
generation metadata, and image decode path. Node payloads are typed Go values;
typed edges connect the graph. The accepted Execution Receipt contains the
submitted request, explicit resolved settings and Model Keys, ordered queue
identifiers, and an empty outputs array.

Public tests prove flag/document parity, unknown and mixed input rejection
before network access, stable selection candidates, incompatible and incomplete
model rejection, a single enqueue mutation, `outcome_unknown` for disconnected
or incomplete successful enqueue responses, rejection of non-finite guidance,
and exactly one JSON result envelope. The Capability Matrix remains unchanged,
so Anima Direct Execution is not advertised before ticket 05.

Repository verification passed:

```text
go test ./...        ok
go test -race ./...  ok
go vet ./...         clean
go mod verify        all modules verified
```

Live verification against InvokeAI 6.14.1 accepted the typed graph in the
`default` queue as batch `11965fd7-8e47-46da-a4ee-33a8d12e4144` with ordered
item IDs `[6]`; item 6 subsequently completed without an error.

The final two-axis review reported no Standards findings and no Spec findings.
