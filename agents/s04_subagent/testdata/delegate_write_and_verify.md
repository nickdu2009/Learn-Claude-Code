Use the `task` tool to delegate this work to a subagent:

1. The subagent must create the file at the exact absolute path `{{TARGET_FILE}}`.
2. The file content must be exactly `subagent-success`.
3. The subagent should use tools to complete the write and summarize what it did.

After the subtask returns, verify the result from the parent agent by reading the exact same absolute path yourself.
In your final answer, mention:
- that you delegated the work with `task`
- the exact file content
- whether verification succeeded
