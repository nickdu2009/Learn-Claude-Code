Prepare a small release-note handoff using persistent teammates in the current working directory.

Rules:
1. Lead must coordinate the work. Lead must not create or edit the target files directly.
2. Use only the current session tools.
3. Keep the workflow inside this directory.

Workflow:
1. Spawn teammate `alice` with role `writer`.
2. Spawn teammate `bob` with role `reviewer`.
3. Ask `alice` to create `__RELEASE_NOTES_PATH__` with this exact initial content:

```text
# Release Notes
- Added team inbox workflow
- Added persistent teammates
- Added reviewer handoff
```

4. After writing the draft, `alice` must send lead this exact message:
`draft-ready::__RELEASE_NOTES_PATH__`

5. After lead receives that message, ask `bob` to review `__RELEASE_NOTES_PATH__`.
6. `bob` must read the file. If the file is missing a rollback section, `bob` must send lead this exact feedback:
`review-feedback::add rollback section`

7. Lead must forward that feedback to `alice`.
8. `alice` must edit the same file and add this exact section at the end:

```text
## Rollback
- Restore previous version
```

9. After updating the file, `alice` must send lead this exact message:
`draft-updated::__RELEASE_NOTES_PATH__`

10. Lead must ask `bob` to review the updated file again.
11. If the file now contains the rollback section, `bob` must:
- write `__REVIEW_RESULT_PATH__` with the exact text `review=passed`
- send lead this exact message:
`review-ok::__RELEASE_NOTES_PATH__`

Final reply requirements:
- mention `draft-ready`
- mention `review-feedback`
- mention `draft-updated`
- mention `review-ok`
- mention that the rollback section was added
