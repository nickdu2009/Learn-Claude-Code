Use the `task` tool to delegate this work to a subagent:

1. In directory `{{WORK_DIR}}`, create a file named `delegated.txt`.
2. The file content must be exactly `subagent-success`.
3. The subagent should complete the write and summarize what it did.

After the subtask returns, verify the result from the parent agent by reading the file yourself.
In your final answer, mention:
- that you delegated the work with `task`
- the exact file content
- whether verification succeeded
