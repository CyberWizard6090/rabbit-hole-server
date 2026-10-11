# Contributing

## Commit Message Convention

All commit messages must follow this format:

`type(scope): description`

Allowed types:

- `feat` — new functionality
- `fix` — bug fixes
- `refactor` — internal code restructuring without behavior changes
- `test` — test changes
- `chore` — maintenance tasks
- `ci` — CI/CD configuration
- `docs` — documentation
- `build` — build system and dependencies

The scope is required and must identify the affected area, for example `auth`, `db`, `task`, `user`, `workspace`, `config`, `api`, `github`, or `docker`.

Examples:

- `feat(auth): add refresh token rotation`
- `fix(task): reject invalid status transitions`
- `refactor(db): isolate transaction handling`
- `test(auth): cover expired token scenarios`
- `ci(github): add race detector`
- `docs(api): document cursor pagination`

Breaking changes must be explicitly marked with `!` and documented using `BREAKING CHANGE:`.

## Commit Granularity

Each commit must represent one logical change.

- Keep unrelated changes in separate commits.
- Include relevant tests with the implementation when practical.
- Separate independent refactoring from behavior changes.
- Do not use vague messages such as `fix`, `update`, `final`, or `changes`.
- Each commit should be understandable when reviewed independently.

## Pull Requests

- Keep PRs focused on one task or a closely related set of changes.
- Describe the purpose and expected behavior.
- Include test results and database migration details when applicable.
- Do not merge while required CI checks are failing.
- Verify migration rollback when a migration changes database schema.

## Required Checks

- gofmt
- golangci-lint
- go vet
- unit tests
- race detector
- integration tests when applicable
- vulnerability scanning
- Go build
- Docker build
