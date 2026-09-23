# Bediz

This context describes Bediz, a deterministic controller through which a human and an AI agent work with the same local InvokeAI installation. The agent chooses creative operations; Bediz applies them programmatically, while the human can observe the results in the web interface and take over the work when needed.

## Language

**Bediz**:
The open-source deterministic controller and CLI that applies explicit creative-agent operations to a running InvokeAI installation.
_Avoid_: AI assistant, InvokeAI fork, browser bot

**Collaborative Control**:
The human and agent working sequentially or concurrently on the same InvokeAI installation and creative resources.
_Avoid_: Full UI automation, remote desktop control

**Capability Coverage**:
The set of durable InvokeAI operations the agent can perform through commands; it does not mean reproducing every web-interface gesture or component.
_Avoid_: Pixel parity, DOM parity, clicking every button

**Contract Parity**:
Equivalent behavior for explicitly supported parameters and InvokeAI versions, without promising implementation parity with every frontend branch or hidden browser default.
_Avoid_: Frontend parity, best-effort fallback

**Capability Matrix**:
The tested record of which controller operations, model families, settings, and InvokeAI versions are supported.
_Avoid_: Aspirational feature list

**Compatibility Check**:
An assessment of whether a running InvokeAI installation exposes the versions, invocation schemas, and installed model components required by a controller capability.
_Avoid_: Connectivity check, blind version check

**Supported Version Range**:
An InvokeAI version interval for which a capability's behavior has been tested and recorded in the capability matrix. Endpoint presence alone does not make a graph-producing capability supported.
_Avoid_: Latest version, schema-compatible guess

**Handoff**:
The human continuing an agent-created or agent-modified job, image, or other creative resource in the InvokeAI web interface.
_Avoid_: Screen sharing

**Direct Execution**:
The agent submitting a complete operation to InvokeAI and starting it without requiring the human to press Invoke.
_Avoid_: UI clicking

**Execution Receipt**:
The final record of a direct execution, containing the submitted request, fully resolved settings, exact model and component keys, queue identifiers, per-output seeds, image references, and synchronization warnings.
_Avoid_: Local audit log, raw execution graph

**Generation Request**:
A self-contained description of one generation operation whose resolved behavior does not depend on transient browser state.
_Avoid_: Current UI settings, implicit browser defaults

**Prompt Pair**:
A required positive prompt and an optional negative prompt within a generation request. A non-empty negative prompt is valid only for model families whose supported capability includes it.
_Avoid_: Prompt template, silently ignored negative prompt

**Request Document**:
The canonical typed JSON representation of an operation request, accepted from a file or standard input. Command flags are a convenience representation of the same request and are not mixed with request-document fields.
_Avoid_: CLI transcript, partial configuration override

**Schema Version**:
The required integer identifying the public structure and semantics of a request document or result envelope. Unknown fields are validation errors within a schema version.
_Avoid_: InvokeAI version, Bediz binary version

**Result Envelope**:
The single final JSON object emitted to standard output in machine-output mode, containing the schema version, outcome, operation, data or structured error, and warnings.
_Avoid_: Progress event, log line, JSON stream

**Structured Error**:
A stable machine-readable error code with a human-readable message and relevant details. It carries precise failure meaning while the process exit status communicates only a broad failure category.
_Avoid_: Raw backend response, stack trace

**Unknown Outcome**:
A result indicating that Bediz cannot determine whether InvokeAI accepted a state-changing request because the connection failed before a conclusive response. The caller inspects InvokeAI state before deciding whether to submit another request.
_Avoid_: Operation failure, automatic retry

**Core Generation**:
The cross-family generation capability comprising prompts, model selection, dimensions, steps, seed, scheduler, guidance, output count and destination, queue control, result retrieval, and UI synchronization.
_Avoid_: Advanced generation, full UI parity

**Exact Dimensions**:
The resolved output width and height submitted for generation. Both values are supplied together or both come from defaults, and they satisfy the selected model family's supported bounds and alignment.
_Avoid_: UI aspect lock, width-only request

**Applicable Setting**:
A typed generation field whose meaning and support are defined for the selected model family. Supplying a typed field to an unsupported family is an error rather than a request to ignore it.
_Avoid_: Arbitrary advanced option, best-effort parameter

**Resolved Seed Set**:
The exact seed assigned to each requested output before execution. Without an explicit seed, each output receives a valid random seed; with an explicit seed, subsequent outputs increment from that seed.
_Avoid_: UI random toggle, unresolved random seed

**Output Board**:
The optional InvokeAI board identifier used to organize generated images in the gallery. An output with no board identifier appears as Uncategorized; this destination never depends on the board currently selected in a browser.
_Avoid_: Filesystem directory, active UI board

**Generation Profile**:
A named, schema-versioned set of local technical defaults that is applied after model-family defaults and before explicit request values. It excludes prompts, seeds, source images, output destinations, and secrets.
_Avoid_: Current UI state, hidden defaults

**Creative Discretion**:
The creative agent's authority to improve prompts and choose unspecified models and settings while preserving every constraint the user stated explicitly.
_Avoid_: Controller default, permission to override the user

**Installation Consent**:
The user's authorization for a creative agent to download a missing model after being told its source, approximate size, and known license information. A direct request to install or use a named model already supplies this consent.
_Avoid_: Approval for generation, blanket download permission

**Model Family**:
A group of models that share an execution-graph shape, required component types, and compatible generation settings.
_Avoid_: Individual checkpoint, display name

**Model Variant**:
The InvokeAI-recorded subtype of a Model Family, such as FLUX.1 dev or schnell, that can change applicable settings and defaults.
_Avoid_: Display name, model format

**Model Key**:
The InvokeAI-assigned identifier used to select one installed model exactly. A display name is only a valid selector when it resolves to one model key.
_Avoid_: Ambiguous model name, gallery position

**Component Resolution**:
The deterministic selection of compatible installed component models required by a generation request, subject to profile preferences and explicit overrides.
_Avoid_: First available model, random selection

**Model Source Reference**:
A user- or agent-supplied locator for an installable model artifact, such as a direct URL, a Hugging Face reference, a local path, or a Civitai model-version page.
_Avoid_: Search query, model recommendation

**Source Resolution**:
The deterministic conversion of a model source reference into one exact installable artifact. Resolution may return explicit choices when a source identifies multiple versions or files, but it never chooses a version merely because it is the newest.
_Avoid_: Model discovery, ranking, guessing

**Selection Required**:
A structured, non-interactive result containing candidate identifiers when an operation cannot resolve one exact model, version, file, or component. The caller selects a candidate and submits a new request.
_Avoid_: Interactive prompt, implicit first choice

**Civitai Version Reference**:
A Civitai page URL or identifier that names a specific model version and can be resolved through Civitai's model-version metadata. A model page that does not identify a version is not an exact version reference.
_Avoid_: Generic Civitai model page, Civitai search result

**Execution Graph**:
The backend-executable recipe of invocation nodes and data-flow edges for one operation; it underlies Generate and Canvas as well as user-authored workflows.
_Avoid_: Saved workflow, workflow-editor layout

**Graph Compiler**:
The deterministic component that translates a normalized operation into a model-family-specific execution graph.
_Avoid_: Graph executor, creative agent

**Parameter Recall**:
The agent loading a coherent set of generation parameters into the open InvokeAI web interface without starting generation, so the human can review or continue the work.
_Avoid_: Direct execution, setting a single widget

**UI Synchronization**:
Reflecting the parameters of the latest direct execution in the InvokeAI web interface so the human can see and continue the agent's work.
_Avoid_: UI automation

**Synchronization Level**:
The capability-matrix value describing how much of an operation InvokeAI can restore in its web interface: full when supported resolved fields are populated, partial when the queue, result, metadata, and receipt are visible but operation-specific controls cannot all be restored.
_Avoid_: Best-effort claim, hidden synchronization failure

**Image Reference**:
A stable InvokeAI image identifier paired with its metadata and an accessible local file or preview path.
_Avoid_: Screen position, gallery index

**Source Image**:
The exact InvokeAI image used as an operation input. A local image path is first uploaded and resolved to an InvokeAI image identifier, and that upload is recorded in the execution receipt.
_Avoid_: Selected gallery thumbnail, browser file input

**Generative Upscale**:
The InvokeAI tiled multi-diffusion operation that enlarges a source image with a Spandrel model and regenerates detail with a compatible main model and Tile ControlNet.
_Avoid_: Image resize, standalone RealESRGAN invocation

**Upscale Component Set**:
The compatible main model, Spandrel upscale model, Tile ControlNet, and optional VAE resolved for one generative upscale request.
_Avoid_: Single upscale model, arbitrary installed models

**Creative Agent**:
The external AI system that interprets the user's intent and chooses prompts, settings, resources, and operations.
_Avoid_: Controller, CLI

**Controller**:
The deterministic software that validates explicit operations and applies them to InvokeAI without making creative decisions or calling an LLM.
_Avoid_: Agent, autonomous prompt writer

**Agent Skill**:
Instructions that teach a creative agent to translate user intent into deterministic controller operations without containing graph-building logic.
_Avoid_: Controller implementation, model-family adapter
