# Contributing to Tendo

Thank you for helping. Tendo is a small project with one maintainer, so focused, well-tested changes are the easiest to accept. Everyone who takes part follows the [code of conduct](CODE_OF_CONDUCT.md). How decisions are made is in [governance](GOVERNANCE.md).

## Report a problem or suggest a change

- **Security vulnerabilities:** never in a public issue. Follow the [security policy](.github/SECURITY.md).
- **Bugs:** open a [GitHub issue](https://github.com/bigtcze/tendo/issues) with the Tendo version, how you run it, the steps to reproduce, what you expected and what happened. Leave out passwords, tokens and real household data.
- **Features:** open an issue before you write code. Tendo deliberately stays a household backlog; check what it will not do in the [charter](.autonomous/CHARTER.md) and [product contract](.autonomous/PRODUCT.md) ("Explicitly deferred or excluded"). A pull request for an unagreed feature may be declined even if it works.

## Requirements for an acceptable contribution

A pull request is merged only when all of these hold:

1. **One focused change.** One fix or feature per pull request, on a branch from current `main`. `main` is protected and changes only through pull requests.
2. **Tests come with the change.** New functionality gets automated tests in the same pull request, at the layer where it can break: Go unit tests, real-PostgreSQL integration tests, frontend tests, Playwright end-to-end tests, API contract checks or deployment smokes. A bug fix gets a regression test that fails before the fix and passes after it; say in the pull request what failed first. Tests must check behavior and stored state, not only that a request returned `200`. Never weaken, skip or delete a meaningful test to get green CI. The full test policy is [.autonomous/TESTING.md](.autonomous/TESTING.md).
3. **Coding standards.**
   - Go follows [Effective Go](https://go.dev/doc/effective_go), [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) and the [Google Go Style Guide](https://google.github.io/styleguide/go/guide.html). `gofmt` and `golangci-lint` (`backend/.golangci.yml`: `go vet`, `staticcheck`, `errcheck` and complexity limits) enforce it.
   - TypeScript and React follow the [Rules of React](https://react.dev/reference/rules) with strict TypeScript. ESLint (`frontend/eslint.config.js`, zero warnings) and `tsc` enforce it.
   - Project conventions for readability, boundaries and size are in [.autonomous/CODE_QUALITY.md](.autonomous/CODE_QUALITY.md); module boundaries are in [.autonomous/ARCHITECTURE.md](.autonomous/ARCHITECTURE.md).
4. **Contracts stay in step.** API changes update `api/openapi.yaml` and the generated code ([API conventions](docs/development/api.md)). Database changes are forward-only migrations with an upgrade test. Configuration changes update `.env.example`, `compose.yaml` and the [configuration reference](docs/admin/configuration.md). Behavior changes update the user or admin guide that describes them ([release rules](docs/development/release.md#compatibility-rules-for-every-change)).
5. **Security rules hold.** Authorization is checked on the server for every request; household data never leaks across households; no secrets, tokens or personal data in code, logs, fixtures or screenshots; no telemetry. See [.autonomous/SECURITY.md](.autonomous/SECURITY.md).
6. **Checks pass.** The complete local gate passes, the `required` GitHub check is green on the latest commit, the branch is current with `main`, and review conversations are resolved.
7. **Review.** Every change is reviewed by someone other than its author before merge.

## Set up and run the checks

The [development and tests guide](docs/development/testing.md) sets up the pinned toolchain (Go, Node.js and sqlc through [mise](https://mise.jdx.dev), checksum-verified) and a disposable PostgreSQL 18 test service. Then run the complete gate:

```sh
bash scripts/dev-exec.sh -- bash scripts/verify-local.sh
```

For a quicker loop while you work, run only the relevant check, for example `bash scripts/dev-exec.sh -- bash -c 'cd backend && go test ./internal/item/...'` or `npm --prefix frontend run -s test -- src/path/to/file.test.tsx`. The [runtime](docs/development/runtime.md) and [frontend](docs/development/frontend.md) guides show how to run Tendo locally.

## Pull request description

Say what changed for users or self-hosters, which tests protect it (and, for a fix, which test failed before), and which checks you ran. Link the issue. Write commit messages in the existing style, for example `fix(ui): ...`, `feat(admin): ...`, `test(e2e): ...`, `docs: ...`.

## License of contributions

Tendo is licensed under the [Apache License 2.0](LICENSE). Unless you state otherwise, a contribution you submit is licensed under the same terms (section 5 of the license). Submit only work you have the right to contribute.
