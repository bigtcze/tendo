## Edit tool call hygiene

- For OpenCode v2 `edit`, use the declared schema: `path`, `oldString`, `newString`, and optional `replaceAll`. Never invent argument names.
- Never call `edit` when `oldString` and `newString` are identical. Either prepare an actual change or skip the edit if the desired state already exists.
- When `oldString` is not found, inspect only the relevant current file section, construct a new unique anchor, and retry with corrected arguments.
- Never repeat an identical failed edit.
- On invalid arguments, correct the missing or malformed fields before retrying.
- Prefer narrow edits. Avoid unnecessary full-file reads and unchecked shell-based rewrites.
