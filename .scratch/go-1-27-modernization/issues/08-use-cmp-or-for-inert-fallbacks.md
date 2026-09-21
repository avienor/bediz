# 08: Use cmp.Or only for inert fallback values

**What to build:** Apply the Modern Go Guidelines `cmp_or` rule to simple fallback chains whose arguments are already-evaluated values or constants: HTTP timeout, maximum response body size, user agent, unknown diagnostic display value, and unset stored-URL display value. Preserve validation of negative numeric options, custom-client retry semantics, and all human-readable output exactly.

**Blocked by:** None (can start immediately).

**Execution route:** `worker + independent review` — this is low-risk mechanical cleanup, and review can conclusively verify that eager argument evaluation introduces no side effects.

**Verification gate:** Default and explicit HTTP option tests cover zero, positive, and negative values as applicable; configuration and readiness human-output tests compare exact fallback text. Review confirms every `cmp.Or` argument in scope is inert. All project-defined Go checks pass.

**Escalate when:** A fallback argument performs I/O, mutation, allocation whose eager execution matters, or error-producing computation; an option's zero value needs semantics other than the currently accepted default; or exact human output would change.

**Status:** ready-for-agent

- [ ] Eligible zero-value fallback chains use `cmp.Or` only with inert arguments.
- [ ] HTTP defaults, negative-value validation, and custom-client retry defaults remain unchanged.
- [ ] Human-readable unknown and unset values remain byte-for-byte unchanged.
- [ ] Focused fallback tests and all repository verification commands pass.
