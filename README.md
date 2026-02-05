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

On first run, `seven up` will prompt you to log in and then create/clone the sprite.
Subsequent runs should feel instant.

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
- **Better defaults for agents.** Instead of running Claude or Codex directly on your laptop, `seven` encourages a safer, reproducible workflow without losing day‑to‑day convenience.

## Contributing
### Commands (dev workflow)
```sh
make build
./build/seven init
./build/seven up --no-tui
./build/seven status
./build/seven destroy
```

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

## Implementation Plan (short)
- **Core CLI:** `seven init`, `seven up`, `seven destroy`, `seven status`.
- **Bootstrap:** resolve sprite name, create/reuse sprite, clone repo when possible.
- **TUI:** minimal Bubble Tea flow with progress and clean status output.
- **Packaging:** GitHub Releases + curl installer (primary), package managers later.

## Upcoming: IDE + Networking Experience
### 1) IDE connection (VS Code and others)
Goal: edit and run code directly inside the sprite, without relying on SSH.

Planned approach:
- **Session-first UX:** `seven up` opens a shell via `sprite console`, while `seven exec` (and the CLI’s `sprite exec`) can run commands in the sprite with full TTY support.
- **In-sprite editor:** default to terminal editors (vim/nano) inside the sprite for quick edits.
- **Full IDE in the sprite:** add a `seven ide` flow that starts a browser‑based IDE (e.g. code‑server/openvscode‑server) inside the sprite and forwards a port to the local machine.

### 2) Feedback loop + port forwarding
Goal: run the app inside the sprite and access it locally as if it were running on your machine.

Planned approach:
- **Assistant detection:** on the host, detect which assistant to use (priority order: Claude, Codex, Cursor, Gemini) based on env vars being set.
- **Guided init:** after cloning, write a prompt for the assistant to review the codebase and attempt to start the local dev stack inside the sprite. The prompt should ask it to identify which endpoints/ports need to be exposed.
- **Forwarding:** use `sprite proxy` to forward local ports to the sprite, based on the assistant’s findings (or a user override).
- **Command surface:** add `seven forward` (and/or `seven dev`) to start the app in the sprite and wire up ports in one step, including any required forwarding.

### 3) Browser skill for agents
Goal: ensure the agent can close the loop for webapps by testing in a real browser context.

Planned approach:
- **Optional install:** during `seven init`, detect if the repo is likely a webapp and offer to install a browser skill for the selected assistant.
- **Assistant‑aware:** pick the right browser tool per agent (Claude/Codex/Cursor/Gemini) and configure it in the sprite.
- **Safe defaults:** keep it opt‑in unless a webapp is detected, and document how to enable/disable later.

## References
- Fly.io Sprite docs: https://docs.sprites.dev/
- Design background: https://fly.io/blog/code-and-let-live/ and https://fly.io/blog/design-and-implementation/
