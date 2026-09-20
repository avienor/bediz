---
status: accepted
---

# Keep creative discretion in the agent skill

The creative agent may improve prompts and choose unspecified models and technical settings without asking about every minor creative choice. It must preserve any prompt, model, or setting the user explicitly fixes. Bediz itself will make no creative decisions; the agent skill translates intent into explicit deterministic operations.

The skill may install a model without another confirmation when the user explicitly requested that model be installed or used. If an ordinary creative operation reveals that a missing model is needed, the skill will first report the source, approximate download size, and known license information and obtain installation consent. It will prefer an already installed compatible model when that satisfies the request.

The repository will contain one canonical `skills/bediz/SKILL.md`. Agent environments that support the format may consume it directly, while other environments may use thin installation adapters derived from it. Graph construction, API payloads, and compatibility logic remain in the Go binary rather than being duplicated in agent instructions.

## Consequences

Users can delegate prompt and setting work without making Bediz nondeterministic. Large or license-sensitive downloads do not occur merely because an agent found them useful, and multiple agent integrations share the same behavioral source without reimplementing the controller.
