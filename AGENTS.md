# Repository Guidelines

## Project Structure & Module Organization
- `cmd/seven/` contains the Go CLI source and unit tests (`main.go`, `*_test.go`).
- `cmd/seven/integration_test.go` holds integration tests that exercise the Sprite CLI.
- `scripts/` includes installer tooling; `dist/` and `build/` are for release and local binaries.
- `.github/` stores CI/workflow configuration; `README.md` documents user-facing usage.

## Build, Test, and Development Commands
- `make build`: compile the CLI into `./build/seven`.
- `./build/seven up --no-tui`: run locally without TUI for debug output.
- `go test ./cmd/seven`: run unit tests for the CLI package.
- `SEVEN_INTEGRATION=1 go test -run TestIntegration -v ./cmd/seven`: run integration tests (requires `sprite login`).

## Coding Style & Naming Conventions
- Go code follows standard Go formatting; run `gofmt` on modified files.
- Indentation: tabs in Go (per `gofmt`), 2 spaces in shell snippets where applicable.
- Naming: use clear, action-oriented function names (e.g., `resolveSpriteName`, `syncGitIdentity`).

## Testing Guidelines
- Unit tests live next to sources as `*_test.go`.
- Integration tests are gated by `SEVEN_INTEGRATION=1` and require a logged-in Sprite CLI.
- Prefer targeted test names describing behavior, e.g., `TestResolveSpriteNameNormalizesDirName`.

## Commit & Pull Request Guidelines
- Commit messages are short and imperative (e.g., “Fix sprite name normalization”).
- Include a brief problem/solution summary in PR descriptions and link any related issues.
- If behavior changes, mention new/updated tests and how to run them.

## Agent & Environment Notes
- This repo runs inside a Sprite; work with maximum permissions enabled, and call it out if that is not the case.
- Start feature work on a feature branch (e.g., `feat/normalize-sprite-name`).
- Write tests for all changes; add integration tests when behavior touches Sprite or external workflows.
- Run tests before every commit.

## Release Notes
- For bugfix releases, push commit and tags together: `git push origin main --tags`.
