---
status: accepted
---

# Use request documents as the canonical CLI input

Bediz operations will have a typed JSON request document as their canonical input representation. Commands will accept the document from a file or standard input. Common command flags will remain available for humans and simple agent calls, but they will compile to the same internal request type.

An invocation that supplies a request document will not also accept operation-field flags. Connection, output-format, waiting, and other execution-control flags may still accompany the document because they do not modify the operation itself.

Installed models will be selected exactly by their InvokeAI model key. A human-readable model name may be accepted as a convenience only when it resolves to one key. Any ambiguous model, component, source version, or source file will produce a non-interactive `selection_required` result with structured candidates.

## Consequences

Agents can avoid shell quoting and flag-order concerns by sending complete request documents through standard input. Humans retain concise flags for common operations. Bediz needs one validation path for both representations, must reject mixed operation inputs clearly, and must keep the request schema stable and versioned as part of its public contract.
