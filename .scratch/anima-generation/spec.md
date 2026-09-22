# Anima generation feature spec

**Status:** approved

## Source of truth

This feature implements delivery step 3 of the accepted V1 specification: end-to-end Anima generation, polling, and Execution Receipt. The governing behavior is in V1 §§ 5, 8–11, 17, 22–24 and ADR-0002, ADR-0003, ADR-0006 through ADR-0011, and ADR-0015.

## Scope

- Compile a typed Generation Request into the tested InvokeAI 6.14.x Anima Execution Graph.
- Submit the graph to the `default` queue exactly once.
- Support both accepted `--no-wait` results and waiting by default for terminal queue state.
- Return an Execution Receipt with the submitted request, resolved settings, exact model and component keys, queue identifiers, Resolved Seed Set, Image References, Output Board, and warnings.
- Register and advertise Anima Direct Execution only after the complete slice and live baseline verification pass.

Generation Recall and UI Synchronization are delivery step 4 and are not part of this feature. Public `queue wait`, Generation Profiles, board discovery, model installation, and additional model-family adapters are also out of scope.

## Generation Request contract

The schema-version-1 Request Document has these fields:

- `schema_version`: required and equal to `1`.
- `model`: required string selector. A Model Key resolves exactly; a model name is accepted only when it resolves uniquely to one installed Anima main model.
- `positive_prompt`: required non-empty string.
- `negative_prompt`: optional string; omission resolves to an empty string for Anima.
- `width` and `height`: optional integers supplied together. Omission resolves both to `1024`; resolved values are positive multiples of 8.
- `steps`: optional positive integer, default `30`.
- `scheduler`: optional Anima scheduler, default `euler`. Supported values are `euler`, `heun`, `dpmpp_2m`, `dpmpp_2m_sde`, `er_sde`, and `lcm`.
- `guidance`: optional number greater than or equal to `1`, default `4.5`.
- `seed`: optional unsigned 32-bit integer.
- `output_count`: optional positive integer, default `1`. InvokeAI queue-capacity rejection remains a conclusive operation failure rather than a Bediz-specific arbitrary maximum.
- `board_id`: optional exact Output Board identifier. Omission submits no board and leaves images Uncategorized.
- `components`: optional typed object with `vae` and `qwen3_encoder` string selectors. A Model Key resolves exactly; a name is accepted only when unique.

Unknown fields and family-inapplicable fields are invalid. Operation flags compile into this same request. The convenience flags are `--model`, `--prompt`, `--negative-prompt`, `--width`, `--height`, `--steps`, `--scheduler`, `--guidance`, `--seed`, `--output-count`, `--board`, `--vae`, and `--qwen3-encoder`. Operation flags cannot be combined with `--request`.

Generation Profile input is not accepted until the profile delivery slice exists.

## Resolution contract

The main model must resolve to an installed Anima main model. The first supported component combination is an Anima VAE and a Qwen3 encoder; the OpenAPI-described FLUX VAE fallback remains unsupported until separately tested and added to the Capability Matrix.

For each component, an explicit request selector wins. Otherwise one compatible installed component is selected. Multiple compatible components return `selection_required` with stable candidate identifiers. No compatible component returns `missing_component` with the component type and known installation guidance. Resolution and compatibility failures occur before enqueue.

When no seed is supplied, Bediz assigns one independent random unsigned 32-bit seed per output before submission. With an explicit seed, later outputs increment sequentially and wrap within the unsigned 32-bit range. The complete Resolved Seed Set is fixed before the mutation.

## Execution Receipt contract

The successful `generate` Result Envelope uses its `data` as the Execution Receipt and contains:

- `submitted_request`: the canonical Generation Request before defaults and automatic component resolution.
- `resolved_settings`: prompts, dimensions, steps, scheduler, guidance, output count, board identifier, exact `model_key`, typed `component_keys` (`vae` and `qwen3_encoder`), and ordered `seeds`.
- `queue`: `queue_id`, `batch_id`, and ordered `item_ids`.
- `outputs`: ordered records containing `item_id`, `seed`, and the resulting Image Reference. This is empty for a successful `--no-wait` result.

The Result Envelope's `warnings` array carries synchronization warnings when step 4 later adds UI Synchronization. It is empty for this feature unless another non-fatal warning is explicitly defined.

Each multi-output seed is associated with the queue item at the same position in InvokeAI's ordered enqueue result. A completed receipt must verify the association from item metadata/results; a contradiction is an escalation rather than a guessed mapping.

## Waiting and errors

Waiting has no total timeout by default. `--timeout` bounds the local wait when supplied, while each HTTP request retains a bounded transport timeout. Timeout and local interruption do not cancel the InvokeAI item.

Stable errors introduced or used by this feature are:

- `selection_required`, exit status 3, with structured candidates.
- `missing_component`, exit status 4, with component details and known installation guidance.
- `unsupported_capability`, exit status 4, before graph submission.
- `wait_timeout`, exit status 6, with queue identifiers and an explicit statement that execution was not cancelled.
- `interrupted`, exit status 130, with queue identifiers when enqueue already succeeded.
- `outcome_unknown`, exit status 6, when the enqueue transport result is inconclusive; Bediz does not retry.
- `invokeai_operation_failed`, exit status 6, for a conclusive failed/cancelled queue item without exposing server tracebacks.

## Verification baseline

The local baseline is InvokeAI 6.14.1 with an installed Anima main model, Anima VAE, and Qwen3 encoder. A completed native Anima queue item supplies the reference graph topology. Automated verification must use versioned graph/OpenAPI fixtures and public module or CLI seams; live verification must enqueue the smallest meaningful supported generation and verify the resulting Image Reference.
