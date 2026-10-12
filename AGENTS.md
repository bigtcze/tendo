# Tendo development

Tendo is a self-hosted household organization tool for persistent obligations.

## Specifications
Specifications live in `docs/specs/`. Always read `docs/specs/GUARDRAILS.md`, `docs/specs/CHARTER.md`, `docs/specs/PRODUCT.md`, `docs/specs/ARCHITECTURE.md`, `docs/specs/TESTING.md`, and `docs/specs/DEFINITION_OF_DONE.md`. For frontend work, read `docs/specs/UX.md`; for HTTP/client work, read `docs/specs/API.md` and `api/openapi.yaml`; for documentation, read `docs/specs/DOCUMENTATION.md`; for future seams, read `docs/specs/ROADMAP.md`. The `docs/specs/*` files are board-controlled and may change only when a task explicitly authorizes it.

Follow `docs/development/workflow.md` for engineering workflow.

## Tooling and verification
Run tools through `scripts/dev-exec.sh` to use the pinned `toolchain.json` versions. The mandatory gate is `bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` and requires `TENDO_TEST_POSTGRES_ADMIN_URL`.

## Repository and task policy
`main` is protected. Use focused pull requests; do not commit or push directly to `main`, force-push, rewrite shared history, bypass required checks, or merge known-broken changes. Task state, plans, and learnings live in the Paperclip project tracker. The repository holds no live agent memory files.

## V1 completion
V1 is complete when every item in `docs/specs/DEFINITION_OF_DONE.md` is verified on `main`, the required CI check is green on `main`, no known release-blocking defect remains, and Head marks the Tendo V1 milestone/project status complete in Paperclip. Completion needs no marker file.
