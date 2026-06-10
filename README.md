# seven 🥤

_`seven up` is `vagrant up` but made of `sprite` for safe agentic development_

`seven` is a modern, developer-friendly replacement for the classic [vagrant](https://developer.hashicorp.com/vagrant/docs/)-based workflow, specifically engineered for the era of AI coding assistants. It runs your repo inside [Fly.io Sprites](https://docs.sprites.dev/): persistent, hardware‑isolated Linux microVMs you can spin up on demand. Sprites start in about a second or two, keep their filesystem between runs, and go idle when unused — so you get VM‑grade isolation without VM overhead.

## Design Goals
- Make seamless and fast isolation the default for high‑autonomy agentic development.
- Provide a familiar and memorable command: `seven up` mirrors the old‑school `vagrant up` workflow, but is _"made of sprite"_.
- Run a full OS so the agent can "close the loop" (run the dev stack, containers, and a browser).
- Enable a safe path to high‑permission workflows without forcing heavy local VM overhead.

## Rationale
High‑autonomy agentic coding works best when the assistant can "close the loop": i.e. read and write files, run tests, install dependencies, make HTTP calls, control a browser. If you keep it on a tight leash with constant approvals, throughput collapses, and you inevitably develop a pavlovian response to always accept anyway. So the real question is not whether to grant high permissions, but how to make that safe.

Simon Willison’s [“lethal trifecta”](https://simonwillison.net/2025/Aug/9/bay-area-ai/) captures the main risk: untrusted input plus access to private data plus the ability to communicate externally. That combination makes prompt injection genuinely dangerous. While isolation doesn’t fully solve prompt injection, it shrinks the blast radius to local secrets, and makes recovery cheaper.

Besides adversarial scenarios, agents still make serious mistakes. These include mis‑typed `rm -fr` (aka "delete France"), aggressive global installs, lost git commits. With proper disk isolation, and not just git worktree shenanigans, these mistakes become merely annoying rather than devastating, and are _much_ easier to recover from thanks to disk snapshots.

Containers aren't a realistic option as the agent needs to run the, typically-containerized, dev stack as well as a browser and Docker‑in‑Docker (DinD) remains a nightmare. Returning to local VMs (e.g. via Vagrant) is the [obvious answer](https://news.ycombinator.com/item?id=46690907), but nowadays local virtualization adds too much friction (especially on ARM CPUs). There are various other approaches (e.g. [firejail](https://softwareengineeringstandard.com/2025/12/15/ai-agents-firejail-sandbox/)) with their own tradeoffs. In my (admitedly short) experience, sprites give you the best of all worlds — hardware‑isolated microVMs with fast startup, persistent disks, and checkpoint/restore — so you can keep a Vagrant‑style workflow without the VM overhead.

Finally, rich Cloud Development Enrivonments (CDEs) have historically been a mixed bag. While making it easier to run full development stacks without worrying about local resource constraints, they could be brittle, costly, and hard to integrate with the local toolchain. However, they provide a clear edge with agentic assistants: they run much closer to inference APIs, making development significantly snappier. And sprites can expose ports locally in cases where a PR needs manual finishing touches. 

## Adoption recipe
If you’re moving from "chat‑only" to fully autonomous coding, the practical path looks something like:
1. **Move execution off your laptop.** `seven up` and run the agent inside the Sprite.
2. **Engineer the harness.** Init `AGENTS.md`, add relevant skills, and require the agent to write and run tests.
3. **Give real permissions.** Let it edit files, run commands, install deps, and control the browser — inside the sandbox.
4. **Use checkpoints + git as your safety net.** Snapshot after pushes, and rollback fast when a run goes sideways.
5. **Promote proven workflows.** When a task pattern works, turn it into a repeatable skill or tweak AGENTS.md.

...and don't hesitate to code locally in parallel, albeit with reduced permissions.

## Get Started
### Install

```sh
curl -fsSL https://raw.githubusercontent.com/1to10partners/seven/main/scripts/install.sh | sh
```

### Run
```sh
cd /path/to/your/repo
seven up
```

On first run, `seven up` will prompt you to run `sprite login`, then create the sprite, and finally clone your repo and handle basic git setup. If host `codex` is logged in using ChatGPT, `seven init` also copies `~/.codex/auth.json` into the sprite so Codex is authenticated there. After clone, `seven init` configures one-shot console bootstrap for both Bash and Zsh so the first `sprite console` opens in the cloned repo and starts `codex`. On each `seven up`, the host `sprite` CLI is checked for updates and auto-upgraded when a newer version is available. Subsequent runs skip `init` and are therefore instant.

Once inside the sprite, cd into your folder and start your favorite assistant. The following come pre-installed: `claude`, `codex`, `cursor-agent`, and `gemini-cli`.

### Running multiple sprites
Each sprite is a fully isolated microVM, so running one assistant session per sprite is a clean alternative to git worktrees. `seven up` opens the main sprite; siblings are numbered:

```sh
seven up          # main sprite (#1)
seven up --new    # create the next sibling (#2, #3, …) and select it
seven up 2        # reopen sibling #2
seven list        # list this repo's sprite family and which one is selected (alias: ls)
```

Siblings are numbered consistently: the main sprite is **#1**, and `seven up --new` / `seven up N` / `seven list` all agree (the first sibling is `<repo>-02`). The repo is always cloned into a directory named after the project (e.g. `~/soclimmo`), regardless of which sibling sprite you're in.

To avoid confusion when switching between consoles, each sprite gets a **color-coded shell prompt** (bash, zsh, and fish) plus a one-line banner naming it on entry. The color is derived from the sprite name, so a given sprite always shows the same color and siblings stay visually distinct. Each sprite also defines a **`cc` alias** for `claude --dangerously-skip-permissions` — sprites are disposable sandboxes, so running Claude with full permissions (no per-tool prompts, no folder-trust dialog) is the convenient default.

### Assistant authentication
On every `seven up`, seven re-syncs your host assistant credentials into the sprite so a sprite created days ago keeps working after your host login refreshes:

- **Claude Code:** seven syncs the real OAuth credential store, not just `~/.claude.json`. On Linux that's `~/.claude/.credentials.json`; on macOS the tokens live in the login Keychain (service `claude-code` / `Claude Code-credentials`), which seven extracts and writes into the sprite. (The Keychain read may show a one-time access prompt.) `~/.claude/settings.json` and `~/.claude.json` are still deep-merged so sprite-only keys are preserved.
- **Codex:** `~/.codex/auth.json` is synced as before.

Because the freshest host token is copied in on each `up`, an expired token inside the sprite is simply replaced. If the synced credentials still don't validate (e.g. a revoked/rotated refresh token), seven prints a warning and the exact command to re-authenticate from inside the sprite — run `claude` (or `codex login`) there and retry. Note that the host and a sprite share one refresh token, so a refresh on one side can occasionally invalidate the other ("token has already been used"); the fix is the same in-sprite re-login.

### Uninstall
Remove the installed binary (defaults to `~/.local/bin`):

```sh
rm -f "$(command -v seven)"
```

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
SEVEN_INTEGRATION=1 go test -v ./cmd/seven
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

### Install gstack
[gstack](https://github.com/garrytan/gstack) is a Claude Code skill toolkit that lives outside the repo (in `~/.claude/skills/`), so each fresh sprite needs its own copy. Pass `--gstack` to install it during bootstrap:

```sh
seven up --gstack
```

This ensures `bun` (its dependency) is present in the sprite, then runs gstack's documented `git clone … && ./setup`. The skills only run inside Claude Code, so seven warns if Claude isn't the resolved assistant, but installs regardless.

## Features
- **Core CLI:** `seven init`, `seven up`, `seven destroy`, `seven status`, `seven list`.
- **gstack:** optional `--gstack` install of the gstack skill toolkit into the sprite.
- **Multiple sprites:** `seven up --new` / `seven up N` to run one assistant session per isolated sprite, with a color-coded prompt per sprite.
- **Bootstrap:** resolve sprite name, create/reuse sprite, clone repo when possible, setup git.
- **TUI:** minimal Bubbletea UX.
- **Packaging:** GitHub Releases + curl installer (primary). No package managers yet.

## Roadmap
### 1) IDE connection (VS Code and others)
Goal: Handle cases where you want to manually review code or edit files directly inside the sprite.

Planned approach:
- **In-sprite editor:** default to terminal editors (vim/nano) inside the sprite for quick edits.
- **Full IDE in the sprite:** add a `seven ide` flow that starts a browser‑based IDE (e.g. code‑server/openvscode‑server) inside the sprite and forwards a port to the local machine or opens in the browser.

### 2) Feedback loop + port forwarding
Goal: run the app inside the sprite and access it locally as if it were running on your machine, to make debugging easier.

Planned approach:
- **Assistant detection:** on the host, detect which assistant to use (priority order: Claude, Codex, Cursor, Gemini) based on env vars being set.
- **Guided init:** after cloning, write a prompt for the assistant to review the codebase and attempt to start the local dev stack inside the sprite. The prompt should ask it to identify which endpoints/ports need to be exposed.
- **Forwarding:** use `sprite proxy` to forward local ports to the sprite, based on the assistant’s findings (or a user override).
- **Command surface:** add `seven dev` to start the app in the sprite and wire up ports in one step, including any required forwarding.

### 3) Browser skill for agents
Goal: ensure the agent can close the loop for webapps by testing in a real browser context.

Planned approach:
- **Optional install:** during `seven init`, detect if the repo is likely a webapp and offer to install a browser skill for the selected assistant.
- **Assistant‑aware:** pick the right browser skill per agent (Claude/Codex/Cursor/Gemini) and configure it in the sprite.
- **Safe defaults:** keep it opt‑in unless a webapp is detected, and document how to enable/disable later.

## References
- Fly.io Sprite docs: https://docs.sprites.dev/
- Fly.io Sprite announcement: https://fly.io/blog/code-and-let-live/
- Fly.io Sprite technical explainer: https://fly.io/blog/design-and-implementation/
- The “lethal trifecta” (Simon Willison): https://simonwillison.net/2025/Aug/9/bay-area-ai/
- My AI adoption journey (Mitchell Hashimoto): https://mitchellh.com/writing/my-ai-adoption-journey
- Running Claude Code dangerously safely (Emil Burzo): https://blog.emilburzo.com/2026/01/running-claude-code-dangerously-safely/
- Ask HN: How are you sandboxing coding agents? https://news.ycombinator.com/item?id=46400129
- HN: Running Claude Code dangerously safely: https://news.ycombinator.com/item?id=46690907
- Run Your Agent in Firejail and Stay Safe: https://softwareengineeringstandard.com/2025/12/15/ai-agents-firejail-sandbox/
