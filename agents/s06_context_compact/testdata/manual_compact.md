You are testing the `compact` tool in the s06 context-compact session.

Work in this exact order:

1. Use `list_dir` on `pkg/loop`.
2. Use `read_file` on `pkg/loop/agent.go`.
3. Use `read_file` on `pkg/loop/todo_nag.go`.
4. Briefly explain, in one short sentence, what changed from the basic loop to the todo nag loop.
5. Then call the `compact` tool with a focus that preserves:
   - the key difference between `Run` and `RunWithTodoNag`
   - which files were inspected
   - what to do next
6. After compaction, answer with:
   - which files you inspected
   - the preserved difference in one short sentence
   - a short note that you are continuing from compressed context

Keep the final answer concise.
