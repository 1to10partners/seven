# seven

`seven up` is the trusted `vagrant up` workflow based on fly.io's fantastic "sprite" backend for safe agentic development

`seven` is a modern, developer-friendly replacement for the classic `vagrant` workflow, built on Fly.io's Sprite backend. The core idea is familiar: `seven up` starts an isolated dev environment and drops you into a console — but instead of local VMs, it uses Sprites for fast, remote, purpose-built isolation. This is especially valuable for agentic development, where strong isolation reduces risk (the lethal trifecta) and improves network proximity to inference APIs.

## Goal
- Provide a single, memorable CLI (`seven`) that mirrors the classic `vagrant` ergonomics (`seven up`, `seven destroy`) while using Sprites as the runtime.
- Make isolation the default for local dev: secure, fast to boot, and convenient for day-to-day workflows.

## Get Started
### Install
POC: a simple curl-based installer for macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/1to10partners/seven/main/scripts/install.sh | sh
```

### Uninstall
Remove the installed binary (defaults to `~/.local/bin`):

```sh
rm -f "$(command -v seven)"
```

### Use it in an existing repo
```sh
cd /path/to/your/repo
seven up
```

### Start your agent
```sh
codex
# or
claude
```


## Rationale
- **Isolation without VM overhead.** Sprites provide a fit-for-purpose environment for code execution without the cost and complexity of local virtual machines.
- **Agentic safety.** Strong isolation reduces risk when running tools that can read/write files, make network calls, or execute code.
- **Network performance.** Remote execution near inference APIs can be faster and more reliable than local VM networking.

## Contributing
### Integration tests (interactive)
Integration tests require an interactive Sprite login and will create/destroy a temporary sprite.

```sh
sprite login
SEVEN_INTEGRATION=1 go test -run TestIntegrationUpDestroy -v ./cmd/seven
```

### Releases
We ship binaries via GitHub Releases using GoReleaser. Tag a release to trigger the workflow:

```sh
git tag v0.1.0
git push origin v0.1.0
```

To test the release process locally without publishing (requires `goreleaser`):

```sh
goreleaser check
goreleaser release --snapshot --clean
```

## Implementation Plan
The current fish scripts (`sprite_up`/`sprite_destroy`) define the baseline behavior. The plan below maps that flow into a robust, cross-platform CLI built with Bubble Tea.

### Phase 1: CLI foundations
- Initialize a Go CLI with a single `seven` binary.
- Implement commands:
  - `seven up` (create or reuse a sprite, open console)
  - `seven destroy` (delete sprite, remove local marker file)
  - `seven status` (show sprite existence + health)
- Add a config/marker file (`.sprite`) to pin the sprite name for the repo.

### Phase 2: Sprite lifecycle logic
- Name resolution:
  - Default to `basename(pwd)` unless `.sprite` overrides it.
  - Validate names and provide helpful errors.
- Create/reuse logic:
  - If sprite exists, reuse and open console.
  - If not, create with `sprite create --skip-console`.
- Destroy logic:
  - Remove local `.sprite` file.
  - Destroy sprite if it exists.

### Phase 3: Repo bootstrap
- Detect git repo and `origin` remote.
- If GitHub remote:
  - Use `gh auth token` if available for faster clone.
  - `gh repo clone <owner/repo> <sprite-name>`.
- Else fallback to `git clone <repo-url> <sprite-name>`.

### Phase 4: TUI experience (Bubble Tea)
- Add a guided flow for `seven up`:
  - status spinner, name confirm/override, progress logs.
- Provide success/failure summary with next actions.
- Make it easy to cancel safely (Ctrl+C).

### Phase 5: polish + docs
- Structured logging and clear error messages.
- Extend README with examples, troubleshooting, and FAQ.
- Add tests for name resolution, `.sprite` handling, and command invocation.
- Document installation:
  - **Primary:** prebuilt binaries per OS/arch (GitHub Releases).
  - **Convenience:** package managers (Homebrew/Scoop/Winget) as optional wrappers.
  - **POC focus:** a simple curl-based installer that fetches the correct release asset.

## References
- Fly.io Sprite docs: https://docs.sprites.dev/
- Design background: https://fly.io/blog/code-and-let-live/ and https://fly.io/blog/design-and-implementation/
