Prepare a small release bundle in the current working directory.

1. Start exactly one background task with `background_run` using the exact shell command provided below.
2. While it runs, create these files with `write_file` using the exact absolute paths and exact contents provided below:

`release.env`
```text
APP_ENV=production
PORT=8080
```

`feature_flags.json`
```json
{"enableBackgroundJobs": true, "enableMetrics": false}
```

`DEPLOY.md`
```text
# Deploy
## Steps
1. Run smoke test
2. Deploy service
## Rollback
1. Restore previous image
```

3. After you receive the background completion notification, call `check_background` for that task id and use the full output in your final summary.
4. In the final reply, mention:
- all three files were created
- RESULT
- FILES
- PORT
- ROLLBACK_SECTION
- BACKGROUND_JOBS

Use these exact absolute paths:
- `release.env`: `__RELEASE_ENV_PATH__`
- `feature_flags.json`: `__FEATURE_FLAGS_PATH__`
- `DEPLOY.md`: `__DEPLOY_DOC_PATH__`

Use this exact shell command for `background_run`:
`__BACKGROUND_COMMAND__`
