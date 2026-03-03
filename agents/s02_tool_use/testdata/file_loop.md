You are a coding agent. Use tools to complete the following task step by step.

The working directory for this task is: {{WORK_DIR}}

Steps:
1. Use list_dir to list the contents of {{WORK_DIR}} and confirm it is empty.
2. Use write_file to create a file named "secret.txt" in {{WORK_DIR}} with the content "42".
3. Use list_dir again to confirm the file "secret.txt" now exists in {{WORK_DIR}}.
4. Use read_file to read {{WORK_DIR}}/secret.txt and tell me its content.

Reply with the final content of the file.
