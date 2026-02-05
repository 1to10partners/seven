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

If you want to run the one-time setup explicitly (login, create sprite, clone repo), use:

```sh
seven init
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

### Commands (dev workflow)
```sh
make build
./build/seven init
./build/seven up --no-tui
./build/seven status
./build/seven destroy
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

## Implementation Plan (short)
- **Core CLI:** `seven init`, `seven up`, `seven destroy`, `seven status`.
- **Bootstrap:** resolve sprite name, create/reuse sprite, clone repo when possible.
- **TUI:** minimal Bubble Tea flow with progress and clean status output.
- **Packaging:** GitHub Releases + curl installer (primary), package managers later.

## Upcoming: IDE + Networking Experience
### 1) IDE connection (VS Code and others)
Goal: open the sprite workspace in your local IDE without losing native tooling.

Planned approach:
- **Session-first UX:** `seven up` opens a shell via `sprite console`, while `seven exec` (and the CLI’s `sprite exec`) can run commands in the sprite with full TTY support. citeturn1view0
- **IDE adapters:** add a `seven ide` flow that can:
  - detect an SSH-capable endpoint if Sprites exposes one,
  - or fall back to syncing + remote commands (git-based or file sync) when SSH isn’t available.

### 2) Feedback loop + port forwarding
Goal: run the app inside the sprite and access it locally as if it were running on your machine.

Planned approach:
- **Forwarding:** use `sprite proxy` to forward local ports to the sprite. citeturn1view1
- **Auto-discovery:** detect ports by heuristics (common dev ports, package scripts, Docker compose, `.env`, or a user-specified list).
- **Command surface:** add `seven forward` (and/or `seven dev`) to start the app in the sprite and wire up ports in one step.

## References
- Fly.io Sprite docs: https://docs.sprites.dev/
- Design background: https://fly.io/blog/code-and-let-live/ and https://fly.io/blog/design-and-implementation/
