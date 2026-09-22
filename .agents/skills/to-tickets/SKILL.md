---
name: to-tickets
description: Break a plan, spec, or conversation into agent-ready tracer-bullet tickets with blocking edges, execution routes, verification gates, and escalation conditions, then publish them to the configured tracker.
disable-model-invocation: true
---

# To Tickets

Break a plan, spec, or conversation into a set of **tickets**: tracer-bullet vertical slices, each declaring the tickets that **block** it.

The issue tracker and triage label vocabulary should have been provided to you. If not, tell the user to run `/setup-matt-pocock-skills`.

## Process

### 1. Gather context

Work from whatever is already in the conversation context. If the user passes a reference (a spec path, an issue number or URL) as an argument, fetch it and read its full body and comments.

### 2. Explore the codebase (optional)

If you have not already explored the codebase, do so to understand the current state of the code. Ticket titles and descriptions should use the project's domain glossary vocabulary, and respect ADRs in the area you're touching.

Look for opportunities to prefactor the code to make the implementation easier. "Make the change easy, then make the easy change."

### 3. Draft vertical slices

Break the work into **tracer bullet** tickets.

<vertical-slice-rules>

- Each slice cuts a narrow but COMPLETE path through every layer (schema, API, UI, tests): vertical, NOT a horizontal slice of one layer
- A completed slice is demoable or verifiable on its own
- Each slice is sized to fit in a single fresh context window
- Any prefactoring should be done first

</vertical-slice-rules>

Give each ticket its **blocking edges**: the other tickets that must complete before it can start. A ticket with no blockers can start immediately.

#### Make each ticket execution-ready

A ticket may be marked `ready-for-agent` only when:

- Product, public-contract, and architecture decisions needed for the slice are resolved. Surface unresolved decisions during the user quiz instead of delegating them to the implementer.
- The ticket is self-contained for a fresh context. Record the accepted behavior and relevant source-of-truth references rather than relying on unrecorded conversation context.
- Acceptance criteria are observable and identify the applicable project-defined checks or live verification. An executor's completion claim is not evidence.

Declare the ticket's **Permanent records**: every durable source that the accepted change must update (`CONTEXT.md` for terminology, the V1 spec for behavior or public contracts, a new or superseding ADR for an accepted design decision, and tests for contract evidence), or `None` with a brief reason when terminology, accepted design, and behavior remain unchanged. If implementation evidence contradicts this declaration, escalate before changing the agreement.

Classify each ticket by ambiguity, blast radius, failure cost, and verification strength—not by code volume or expected token use:

- **`worker + independent review`**: the accepted behavior is complete, the scope is bounded and reversible, and automated or live checks can reliably detect an incorrect implementation. This route requires a separate review phase; it is not permission to accept the worker's completion claim.
- **`frontier-owned`**: implementation still requires product or design judgment, changes a sensitive contract, has difficult-to-reverse effects, or cannot be adequately verified by the available checks.

<worker-review-protocol>

For every `worker + independent review` ticket:

1. The worker implements the slice and runs the verification gate, then leaves the ticket open as awaiting independent review. Its test results and completion claim are review inputs, not acceptance evidence on their own.
2. A different reviewer starts from a fresh context with the ticket, its source-of-truth references, the worker's evidence, and a fixed base/head diff. Before making any fixes, the reviewer evaluates both spec fidelity and repository standards, and independently checks the riskiest acceptance evidence instead of merely trusting the worker's summary.
3. Acceptance-blocking findings return the ticket to implementation. After fixes, rerun the affected verification and review the changed diff. Mark the ticket completed only when the verification gate passes and no acceptance-blocking review finding remains.

Use the configured tracker's equivalent of an awaiting-review state. If it has none, leave the ticket open and record that it is awaiting independent review; do not use `completed` as the handoff state.

Independence and capability are separate requirements: the reviewer must be both separate from the implementation context and capable of evaluating the ticket's highest-risk claims. Keep model and vendor names out of tickets; describe the required review capability and evidence so the route remains stable as models change.

</worker-review-protocol>

Record a **verification gate** that states what behavioral evidence must exist before the ticket is accepted. For `worker + independent review`, also record a **review gate** that states the review scope, the evidence the reviewer must validate independently, and which findings block acceptance. Record **escalation conditions** for contradictions with sources of truth, unexpected live-system behavior, material scope expansion, or repeated failed repair loops.

**Wide refactors are the exception to vertical slicing.** A **wide refactor** is one mechanical change (rename a column, retype a shared symbol) whose **blast radius** fans across the whole codebase, so a single edit breaks thousands of call sites at once and no vertical slice can land green. Don't force it into a tracer bullet; sequence it as **expand–contract**. First expand: add the new form beside the old so nothing breaks. Then migrate the call sites over in batches sized by blast radius (per package, per directory), each batch its own ticket blocked by the expand, keeping CI green batch to batch because the old form still exists. Finally contract: delete the old form once no caller remains, in a ticket blocked by every migrate batch. When even the batches can't stay green alone, keep the sequence but let them share an integration branch that all block a final integrate-and-verify ticket; green is promised only there.

### 4. Quiz the user

Present the proposed breakdown as a numbered list. For each ticket, show:

- **Title**: short descriptive name
- **Blocked by**: which other tickets (if any) must complete first
- **What it delivers**: the end-to-end behaviour this ticket makes work
- **Execution route**: `worker + independent review` or `frontier-owned`, with a brief reason
- **Verification gate**: the evidence required to accept the ticket
- **Review gate**: for `worker + independent review`, what the separate reviewer must examine and independently validate before acceptance; otherwise `Not required by this route`
- **Escalate when**: ticket-specific conditions that require a new decision or stronger owner
- **Permanent records**: `CONTEXT.md`, V1 spec, ADR, tests, or `None`, with a brief reason

Ask the user:

- Does the granularity feel right? (too coarse / too fine)
- Are the blocking edges correct: does each ticket only depend on tickets that genuinely gate it?
- Are the execution routes, verification gates, and review gates appropriate?
- Has any unresolved product, public-contract, or architecture decision been left to an implementer?
- Should any tickets be merged or split further?

Iterate until the user approves the breakdown.

### 5. Publish the tickets to the configured tracker

Publish the approved tickets. **How** depends on the tracker `/setup-matt-pocock-skills` configured; the tickets are the same either way, only the shape of the blocking edges changes:

- **Local files** → write one file per ticket under `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01` in dependency order (blockers first). Each file's "Blocked by" lists the numbers/titles it depends on. Use the per-ticket file template below: one ticket per file, never a single combined file.
- **A real issue tracker (GitHub, Linear, …)** → publish one issue per ticket in dependency order (blockers first) so each ticket's blocking edges can reference real identifiers. Use the platform's native blocking / sub-issue relationship where it has one; otherwise set each ticket's "Blocked by" to the blocking issues. Apply the `ready-for-agent` triage label unless instructed otherwise; the tickets are agent-grabbable by construction.

Work the **frontier**: any ticket whose blockers are all done. For a purely linear chain that means top to bottom.

Do NOT close or modify any parent issue.

<local-ticket-template>

# <NN>: <Ticket title>

**What to build:** the end-to-end behaviour this ticket makes work, from the user's perspective, not a layer-by-layer implementation list.

**Blocked by:** the numbers/titles of the tickets that gate this one, or "None (can start immediately)".

**Execution route:** `worker + independent review` or `frontier-owned` — one sentence explaining why.

**Verification gate:** the observable evidence and applicable project-defined checks required for acceptance.

**Review gate:** for `worker + independent review`, the fixed-diff review scope, the acceptance evidence to validate independently, and which findings block acceptance; otherwise "Not required by this route".

**Escalate when:** the ticket-specific conditions that require a new decision or stronger owner.

**Permanent records:** `CONTEXT.md` / V1 spec / ADR / tests / None — a brief reason.

**Status:** ready-for-agent

- [ ] Acceptance criterion 1
- [ ] Acceptance criterion 2

</local-ticket-template>

<issue-template>

## Parent

A reference to the parent issue on the tracker (if the source was an existing issue, otherwise omit this section).

## What to build

The end-to-end behaviour this ticket makes work, from the user's perspective, not layer-by-layer implementation.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2

## Blocked by

- A reference to each blocking ticket, or "None (can start immediately)".

## Execution route

`worker + independent review` or `frontier-owned` — one sentence explaining why.

## Verification gate

The observable evidence and applicable project-defined checks required for acceptance.

## Review gate

For `worker + independent review`, the fixed-diff review scope, the acceptance evidence to validate independently, and which findings block acceptance. Otherwise, "Not required by this route".

## Escalate when

The ticket-specific conditions that require a new decision or stronger owner.

## Permanent records

`CONTEXT.md` / V1 spec / ADR / tests / None — a brief reason.

</issue-template>

In either form, avoid specific file paths or code snippets: they go stale fast. Exception: if a prototype produced a snippet that encodes a decision more precisely than prose can (state machine, reducer, schema, type shape), inline it and note briefly that it came from a prototype. Trim to the decision-rich parts, not a working demo, just the important bits.
