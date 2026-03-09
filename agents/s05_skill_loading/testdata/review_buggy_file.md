You must use the `load_skill` tool first.

Load the `code-review` skill, then inspect this file with the available file tools:

`{{TARGET_FILE}}`

Return a short code review in the skill's markdown format.

Requirements:
- Include a `Critical Issues` section.
- Mention the SQL injection risk in `fetch_user`.
- Mention the command injection risk in `run_list`.
- Keep the review concise.
