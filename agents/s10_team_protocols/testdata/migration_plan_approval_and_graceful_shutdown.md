Prepare a risky auth migration handoff in the current working directory.

Rules:
1. Lead must coordinate the work. Lead must not create or edit the target files directly.
2. Use only the current session tools.
3. Keep all work inside this directory.
4. Because this is a risky change, the teammate must submit a plan for approval before writing any migration document.
5. After all deliverables are finished, lead must shut the teammate down gracefully using the shutdown protocol.

Workflow:
1. Spawn teammate `alice` with role `migration-engineer`.
2. Ask `alice` to prepare an auth migration runbook for moving from legacy tokens to signed session tokens.
3. Before writing any file, `alice` must submit a plan for approval.
4. The first plan must be rejected because it is missing:
- a rollback section
- a validation checklist
5. Lead must send feedback that clearly asks for both missing items.
6. `alice` must submit a revised plan.
7. Lead must approve the revised plan.
8. Only after plan approval, `alice` must create `__RUNBOOK_PATH__` with content that includes all of these sections:
- `# Auth Migration Runbook`
- `## Rollout`
- `## Validation`
- `## Rollback`
9. `alice` must also create `__CHECKLIST_PATH__` with the exact content:

```text
precheck=done
canary=required
rollback=ready
```

10. After both files are ready, `alice` must send lead this exact message:
`migration-ready::__RUNBOOK_PATH__`

11. After lead receives that message, lead must request graceful shutdown for `alice`.
12. `alice` should approve shutdown after the work is finished.

Final reply requirements:
- mention that the initial plan was rejected
- mention that the revised plan was approved
- mention `migration-ready`
- mention that `alice` shut down gracefully
- mention both `__RUNBOOK_PATH__` and `__CHECKLIST_PATH__`
- mention that rollback and validation were included
