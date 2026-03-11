## s06 Demo Notes

### Run

```bash
go run ./agents/s06_context_compact/
```

### Manual compact path

Feed the content of `manual_compact.md` into the REPL.

Expected signals:

- The model should inspect a few files first.
- The model should call the `compact` tool explicitly.
- After compaction, the conversation should continue without losing the inspected-file context.

### Auto compact path

Feed the content of `auto_compact_many_reads.md` into the REPL.

Expected signals:

- Multiple large `read_file` results should inflate the context.
- The loop should auto-compact once the approximate token threshold is exceeded.
- Full pre-compact history should be written under `.transcripts/`.

### What to verify

- `.transcripts/` contains new JSONL transcript files.
- The final answer still remembers the main files and conclusions after compaction.
- Older tool outputs no longer need to stay in active context verbatim.
- The threshold is a tutorial-style rough estimate, not a provider tokenizer result.
