package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type upResult struct {
	Name         string
	OpenConsole  bool
	SpriteExists bool
}

type upOptions struct {
	Logger         func(string)
	QuietExternal  bool
	AssumeLoggedIn bool
	OpenConsole    bool
	Assistant      string
	SpriteName     string
	NewSprite      bool
	ResolvedName   string
	InstallGstack  bool
	SiblingOrdinal int
}

type spriteNameInfo struct {
	Name       string
	FromFile   bool
	Original   string
	Normalized bool
}

type hostAssistantState struct {
	PreferredAssistant string
	CodexConfigPath    string
	CodexAuthPath      string
	ClaudeConfigPath   string
	ClaudeAuthPath     string
	ClaudeCredentials  claudeCredentialsSource
}

// claudeCredentialsSource describes where the host's Claude Code OAuth
// credentials (access + refresh tokens) live. On Linux they are a file; on
// macOS they live in the login Keychain and must be extracted.
type claudeCredentialsSource struct {
	FilePath string // non-empty: copy this file into the sprite
	Keychain bool   // true: extract from the macOS login Keychain
}

func (c claudeCredentialsSource) present() bool {
	return c.FilePath != "" || c.Keychain
}

// claudeKeychainServices are the macOS Keychain service names Claude Code has
// used for its credentials item, newest first.
var claudeKeychainServices = []string{"Claude Code-credentials", "claude-code"}

var spritePath string
var spriteNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var githubSlugPartPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var spriteLatestVersionPattern = regexp.MustCompile(`(?im)^Latest(?:\s+client)?\s+version:\s*(\S+)\s*$`)
var spriteCurrentVersionPattern = regexp.MustCompile(`(?im)^Current(?:\s+client)?\s+version:\s*(\S+)\s*$`)
var spriteSuffixRe = regexp.MustCompile(`^(.*)-([0-9]{2})$`)

const (
	sevenConsoleHookPath    = "$HOME/.seven-console-hook.sh"
	sevenConsoleMarkerPath  = "$HOME/.seven-console-once"
	sevenSpriteIdentityPath = "$HOME/.seven-sprite-id.sh"
	sevenDefaultAssistant   = "codex"
	gstackRepoURL           = "https://github.com/garrytan/gstack.git"
	gstackSkillDir          = "$HOME/.claude/skills/gstack"
	// Keep the default used by the explicit --gstack flag immutable. Projects
	// may request another immutable revision in their tooling manifest.
	gstackDefaultRevision = "a3259400a366593e0c909dd9ac3e59752efd2488"
	// Playwright in the pinned gstack revision recognizes Ubuntu through 24.04.
	// Sprite images currently report Ubuntu 26.04, whose Chromium build is ABI-
	// compatible with Playwright's Ubuntu 24.04 payload but has no registry key.
	// Apply Playwright's supported host override only for Ubuntu 26+ and only
	// during gstack setup; leave every recognized platform untouched.
	gstackPlaywrightPlatformCmd = `if [ "$(uname -s)" = "Linux" ] && [ -z "${PLAYWRIGHT_HOST_PLATFORM_OVERRIDE:-}" ] && [ -r /etc/os-release ]; then
  . /etc/os-release
  ubuntu_major="${VERSION_ID%%.*}"
  if [ "${ID:-}" = "ubuntu" ] && case "$ubuntu_major" in ''|*[!0-9]*) false ;; *) [ "$ubuntu_major" -ge 26 ] ;; esac; then
    case "$(uname -m)" in
      x86_64|amd64) PLAYWRIGHT_HOST_PLATFORM_OVERRIDE="ubuntu24.04-x64" ;;
      aarch64|arm64) PLAYWRIGHT_HOST_PLATFORM_OVERRIDE="ubuntu24.04-arm64" ;;
    esac
    if [ -n "${PLAYWRIGHT_HOST_PLATFORM_OVERRIDE:-}" ]; then
      export PLAYWRIGHT_HOST_PLATFORM_OVERRIDE
      echo "[seven] using Playwright $PLAYWRIGHT_HOST_PLATFORM_OVERRIDE compatibility build for Ubuntu $VERSION_ID"
    fi
  fi
fi`
	// gstackChromiumDepsCmd installs the OS shared libraries Chromium links
	// against (libglib-2.0, libnss3, libgbm, …). gstack's ./setup downloads the
	// browser *binary* but not these system libs, so on a minimal sprite image
	// Chromium exits 127 ("error while loading shared libraries: libglib-2.0.so.0")
	// at launch and every browse command fails. We run this before ./setup so its
	// own launch self-check passes. Linux + sudo gated; failures propagate so a
	// Sprite is never presented with a known-broken browser runtime.
	// The executable comes from the revision's frozen lockfile, not a mutable
	// registry resolution.
	gstackChromiumDepsCmd = `if [ "$(uname -s)" = "Linux" ] && command -v sudo >/dev/null 2>&1; then
  echo "[seven] installing Chromium system dependencies (libglib-2.0, libnss3, …)"
  ./node_modules/.bin/playwright install-deps chromium
fi`
)

// spriteIdentityPalette holds distinct 256-color codes used to tint each
// sprite's prompt/banner. The color is chosen by hashing the sprite name so a
// given sprite always renders in the same color across sessions.
var spriteIdentityPalette = []int{39, 208, 170, 118, 213, 154, 220, 45, 201, 99}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "--version", "-v", "version":
		printVersion()
		return
	case "init":
		cmdInit(os.Args[2:])
	case "up":
		cmdUp(os.Args[2:])
	case "destroy":
		cmdDestroy(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "list", "ls":
		cmdList(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("seven - vagrant-style workflow backed by fly.io sprites")
	fmt.Printf("version: %s\n", version)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  seven init [--assume-logged-in] [--new] [--sprite name] [--assistant codex|claude] [--gstack]")
	fmt.Println("  seven up [N] [--assume-logged-in] [--new] [--sprite name] [--assistant codex|claude] [--no-console] [--no-tui] [--gstack]")
	fmt.Println("  seven destroy [name] [--sprite name]")
	fmt.Println("  seven status")
	fmt.Println("  seven list")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  version  Show version")
	fmt.Println("  init     One-time setup (login, create sprite, clone repo)")
	fmt.Println("  up       Create or reuse a sprite. Pass N to open sibling #N (1 = main), or --new for the next one")
	fmt.Println("  destroy  Destroy the selected sprite, or a specific sprite by name (positional or --sprite)")
	fmt.Println("  status   Show sprite status for this repo")
	fmt.Println("  list     List this repo's sprite family and which one is selected (alias: ls)")
}

var version = "dev"

func printVersion() {
	fmt.Println(version)
}

func normalizeAssistant(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "codex", "claude":
		return value, nil
	default:
		return "", fmt.Errorf("unsupported assistant %q (use codex or claude)", value)
	}
}

func cmdUp(args []string) {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	noTUI := fs.Bool("no-tui", false, "disable TUI output")
	assumeLoggedIn := fs.Bool("assume-logged-in", false, "skip sprite login")
	noConsole := fs.Bool("no-console", false, "do not open sprite console after up")
	newSprite := fs.Bool("new", false, "create and select a new sibling sprite")
	spriteName := fs.String("sprite", "", "use a specific sprite name")
	assistant := fs.String("assistant", "", "preferred assistant: codex or claude")
	gstack := fs.Bool("gstack", false, "install gstack (github.com/garrytan/gstack) into the sprite")

	// An optional leading number (e.g. "seven up 2") selects sibling #N. It must
	// come first so it is never confused with a flag value like "--sprite 2".
	ordinal := 0
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil {
			if n < 1 {
				fmt.Fprintf(os.Stderr, "seven up failed: sprite number must be a positive integer, got %q\n", args[0])
				os.Exit(1)
			}
			ordinal = n
			args = args[1:]
		}
	}

	_ = fs.Parse(args)
	if *newSprite && strings.TrimSpace(*spriteName) != "" {
		fmt.Fprintln(os.Stderr, "seven up failed: --new and --sprite cannot be used together")
		os.Exit(1)
	}
	if ordinal > 0 && (*newSprite || strings.TrimSpace(*spriteName) != "") {
		fmt.Fprintln(os.Stderr, "seven up failed: sprite number cannot be combined with --new or --sprite")
		os.Exit(1)
	}
	preferredAssistant, err := normalizeAssistant(*assistant)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seven up failed: %v\n", err)
		os.Exit(1)
	}

	shouldUseTUI := !*noTUI
	styleEnabled = shouldUseTUI
	opts := upOptions{
		Logger:         func(msg string) { fmt.Println(msg) },
		QuietExternal:  false,
		AssumeLoggedIn: *assumeLoggedIn,
		OpenConsole:    !*noConsole,
		Assistant:      preferredAssistant,
		SpriteName:     strings.TrimSpace(*spriteName),
		NewSprite:      *newSprite,
		InstallGstack:  *gstack,
		SiblingOrdinal: ordinal,
	}
	if shouldUseTUI {
		res, err := runUpWithTUI(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "seven up failed: %v\n", err)
			os.Exit(1)
		}
		if res.OpenConsole {
			if err := runConsole(res.Name); err != nil {
				fmt.Fprintf(os.Stderr, "failed to open console: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	res, err := runUp(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seven up failed: %v\n", err)
		os.Exit(1)
	}
	if res.OpenConsole {
		if err := runConsole(res.Name); err != nil {
			fmt.Fprintf(os.Stderr, "failed to open console: %v\n", err)
			os.Exit(1)
		}
	}
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	assumeLoggedIn := fs.Bool("assume-logged-in", false, "skip sprite login")
	newSprite := fs.Bool("new", false, "create and select a new sibling sprite")
	spriteName := fs.String("sprite", "", "use a specific sprite name")
	assistant := fs.String("assistant", "", "preferred assistant: codex or claude")
	gstack := fs.Bool("gstack", false, "install gstack (github.com/garrytan/gstack) into the sprite")
	_ = fs.Parse(args)
	if *newSprite && strings.TrimSpace(*spriteName) != "" {
		fmt.Fprintln(os.Stderr, "seven init failed: --new and --sprite cannot be used together")
		os.Exit(1)
	}
	preferredAssistant, err := normalizeAssistant(*assistant)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seven init failed: %v\n", err)
		os.Exit(1)
	}

	_, err = runInit(upOptions{
		Logger:         func(msg string) { fmt.Println(msg) },
		QuietExternal:  false,
		AssumeLoggedIn: *assumeLoggedIn,
		OpenConsole:    false,
		Assistant:      preferredAssistant,
		SpriteName:     strings.TrimSpace(*spriteName),
		NewSprite:      *newSprite,
		InstallGstack:  *gstack,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seven init failed: %v\n", err)
		os.Exit(1)
	}
}

func cmdDestroy(args []string) {
	fs := flag.NewFlagSet("destroy", flag.ExitOnError)
	spriteName := fs.String("sprite", "", "destroy a specific sprite name")
	_ = fs.Parse(args)

	name := strings.TrimSpace(*spriteName)

	// Accept the sprite name as a positional argument too (e.g.
	// "seven destroy soclimmo-03"), and refuse to run when extra arguments
	// are present. Previously a positional name was silently ignored and
	// destroy fell back to the selected sprite — destroying the wrong one.
	if rest := fs.Args(); len(rest) > 0 {
		if len(rest) > 1 {
			fmt.Fprintf(os.Stderr, "seven destroy failed: too many arguments: %s\n", strings.Join(rest, " "))
			os.Exit(1)
		}
		positional := strings.TrimSpace(rest[0])
		if name != "" && name != positional {
			fmt.Fprintf(os.Stderr, "seven destroy failed: conflicting sprite names %q (--sprite) and %q\n", name, positional)
			os.Exit(1)
		}
		name = positional
	}

	info, err := resolveSpriteName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve sprite name: %v\n", err)
		os.Exit(1)
	}
	clearSelection := false
	if name == "" {
		if !info.FromFile {
			fmt.Fprintln(os.Stderr, "failed to resolve sprite name: no selected sprite; use seven up or pass --sprite")
			os.Exit(1)
		}
		name = info.Name
		clearSelection = true
	} else {
		if err := validateSpriteName(name); err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve sprite name: %v\n", err)
			os.Exit(1)
		}
		if info.FromFile && info.Name == name {
			clearSelection = true
		}
	}

	if err := ensureSpriteCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if _, err := spriteList(); err != nil {
		fmt.Fprintf(os.Stderr, "sprite list failed: %v\n", err)
		os.Exit(1)
	}

	exists, err := spriteExists(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sprite list failed: %v\n", err)
		os.Exit(1)
	}
	if exists {
		if err := runCmd(spriteBin(), nil, "destroy", "--force", name); err != nil {
			fmt.Fprintf(os.Stderr, "sprite destroy failed: %v\n", err)
			os.Exit(1)
		}
		if clearSelection {
			if err := removeSpriteFile(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to remove .sprite: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("destroyed sprite: %s\n", name)
		return
	}

	if clearSelection {
		if err := removeSpriteFile(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to remove .sprite: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Printf("sprite not found: %s\n", name)
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	_ = fs.Parse(args)

	info, err := resolveSpriteName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve sprite name: %v\n", err)
		os.Exit(1)
	}
	name := info.Name
	fromFile := info.FromFile

	if err := ensureSpriteCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	exists, err := spriteExists(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sprite list failed: %v\n", err)
		os.Exit(1)
	}

	origin := "cwd"
	if fromFile {
		origin = ".sprite"
	}

	if exists {
		fmt.Printf("sprite: %s (from %s)\n", name, origin)
		fmt.Println("status: exists")
		return
	}

	fmt.Printf("sprite: %s (from %s)\n", name, origin)
	fmt.Println("status: missing")
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	_ = fs.Parse(args)

	info, err := resolveSpriteName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve sprite name: %v\n", err)
		os.Exit(1)
	}
	if err := ensureSpriteCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	listOut, err := spriteList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sprite list failed: %v\n", err)
		os.Exit(1)
	}

	base := info.Name
	if info.FromFile {
		base = spriteFamilyBase(info.Name)
	}
	selected := info.Name

	members := spriteFamilyMembers(base, listOut)
	fmt.Printf("sprite family for %s:\n", base)
	if len(members) == 0 {
		fmt.Println("  (none yet — run 'seven up' to create the main sprite)")
		return
	}
	for _, name := range members {
		number, _ := spriteFamilyOrdinal(base, name)
		marker := " "
		if name == selected {
			marker = "*"
		}
		label := name
		if name == base {
			label = name + " (main)"
		}
		styled := lipgloss.NewStyle().Foreground(lipgloss.Color(spriteColor(name))).Render(label)
		fmt.Printf("  %s %d  %s\n", marker, number, styled)
	}
	fmt.Println()
	fmt.Println("open:  seven up <number>    new:  seven up --new")
}

func runUp(opts upOptions) (upResult, error) {
	if opts.Logger == nil {
		opts.Logger = func(string) {}
	}

	if err := ensureSpriteCLI(); err != nil {
		return upResult{}, err
	}
	maybeUpgradeSpriteCLI(opts)

	name, err := resolveTargetSpriteName(opts)
	if err != nil {
		return upResult{}, err
	}
	info, err := resolveSpriteName()
	if err != nil {
		return upResult{}, err
	}
	if opts.SpriteName == "" && opts.ResolvedName == "" && info.Normalized && !info.FromFile {
		opts.Logger(fmt.Sprintf("[seven up] normalized sprite name from %q to %q (set .sprite to override)", info.Original, info.Name))
	}

	opts.Logger(fmt.Sprintf("[seven up] using sprite name: %s", name))

	exists, err := spriteExists(name)
	if err != nil {
		opts.Logger("[seven up] sprite list failed; running init")
		res, initErr := runInit(opts)
		if initErr != nil {
			return upResult{}, initErr
		}
		res.OpenConsole = opts.OpenConsole
		return res, nil
	}
	if exists {
		opts.Logger("[seven up] sprite exists")
		if err := writeSpriteFile(name); err != nil {
			return upResult{}, err
		}
		// Git identity and assistant credentials (claude/codex, plus gh) are all
		// set up once at creation. Re-syncing on every up was slow (per-call
		// sprite-exec overhead adds up across config + auth for both assistants)
		// and unnecessary for an established sprite. If a host token has rotated
		// and the sprite copy is stale, run `claude` or `codex login` inside the
		// sprite to re-auth — same recovery path as for gh.
		assistantState := detectHostAssistantState(opts)
		assistantState.PreferredAssistant = resolvePreferredAssistantInSprite(name, assistantState, "[seven up]", opts)
		if err := configureConsoleBootstrapInSprite(name, spriteFamilyBase(name), assistantState.PreferredAssistant, opts); err != nil {
			opts.Logger(fmt.Sprintf("[seven up] console bootstrap setup failed: %v", err))
		}
		if err := reconcileProjectEnvironment(name, spriteFamilyBase(name), assistantState.PreferredAssistant, opts); err != nil {
			return upResult{}, err
		}
		return upResult{Name: name, OpenConsole: opts.OpenConsole, SpriteExists: true}, nil
	}

	initOpts := opts
	initOpts.ResolvedName = name
	res, err := runInit(initOpts)
	if err != nil {
		return upResult{}, err
	}
	res.OpenConsole = opts.OpenConsole
	return res, nil
}

func runInit(opts upOptions) (result upResult, returnErr error) {
	if opts.Logger == nil {
		opts.Logger = func(string) {}
	}

	if err := ensureSpriteCLI(); err != nil {
		return upResult{}, err
	}

	if !opts.AssumeLoggedIn {
		opts.Logger("[seven init] logging in to sprite")
		if err := runCmd(spriteBin(), nil, "login"); err != nil {
			return upResult{}, err
		}
	}

	info, err := resolveSpriteName()
	if err != nil {
		return upResult{}, err
	}
	name := opts.ResolvedName
	if name == "" {
		name, err = resolveTargetSpriteName(opts)
		if err != nil {
			return upResult{}, err
		}
	}
	if opts.SpriteName == "" && opts.ResolvedName == "" && info.Normalized && !info.FromFile {
		opts.Logger(fmt.Sprintf("[seven init] normalized sprite name from %q to %q (set .sprite to override)", info.Original, info.Name))
	}

	opts.Logger(fmt.Sprintf("[seven init] using sprite name: %s", name))

	exists, err := spriteExists(name)
	if err != nil {
		return upResult{}, err
	}
	if exists {
		opts.Logger("[seven init] sprite exists")
		if err := writeSpriteFile(name); err != nil {
			return upResult{}, err
		}
		// Git identity and assistant credentials (claude/codex, plus gh) are all
		// set up once at creation. Re-syncing on every up was slow (per-call
		// sprite-exec overhead adds up across config + auth for both assistants)
		// and unnecessary for an established sprite. If a host token has rotated
		// and the sprite copy is stale, run `claude` or `codex login` inside the
		// sprite to re-auth — same recovery path as for gh.
		assistantState := detectHostAssistantState(opts)
		assistantState.PreferredAssistant = resolvePreferredAssistantInSprite(name, assistantState, "[seven init]", opts)
		if err := configureConsoleBootstrapInSprite(name, spriteFamilyBase(name), assistantState.PreferredAssistant, opts); err != nil {
			opts.Logger(fmt.Sprintf("[seven init] console bootstrap setup failed: %v", err))
		}
		if err := reconcileProjectEnvironment(name, spriteFamilyBase(name), assistantState.PreferredAssistant, opts); err != nil {
			return upResult{}, err
		}
		return upResult{Name: name, OpenConsole: false, SpriteExists: true}, nil
	}

	// Resolve and preflight the host checkout before creating any external
	// resource. A dirty/detached checkout must not leave an empty Sprite behind.
	repoURL, repoSlug, ghToken, err := detectRepoInfo(name, opts)
	if err != nil {
		return upResult{}, err
	}
	repoBranch, repoHead := "", ""
	if repoURL != "" {
		repoBranch, repoHead, err = detectRepoCheckout(opts)
		if err != nil {
			return upResult{}, err
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return upResult{}, err
	}
	spriteSelectionPath := filepath.Join(cwd, ".sprite")
	previousSelection, selectionErr := os.ReadFile(spriteSelectionPath)
	hadPreviousSelection := selectionErr == nil
	if selectionErr != nil && !os.IsNotExist(selectionErr) {
		return upResult{}, selectionErr
	}

	opts.Logger("[seven init] creating sprite")
	if opts.QuietExternal {
		if err := runCmdQuiet(spriteBin(), nil, "create", "--skip-console", name); err != nil {
			return upResult{}, err
		}
	} else if err := runCmdDevNull(spriteBin(), nil, "create", "--skip-console", name); err != nil {
		return upResult{}, err
	}
	defer func() {
		if returnErr == nil {
			return
		}
		opts.Logger(fmt.Sprintf("[seven init] initialization failed; destroying incomplete sprite: %s", name))
		if cleanupErr := runCmd(spriteBin(), nil, "destroy", "--force", name); cleanupErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("destroy incomplete sprite %s: %w", name, cleanupErr))
		}
		if hadPreviousSelection {
			if restoreErr := os.WriteFile(spriteSelectionPath, previousSelection, 0o644); restoreErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("restore .sprite selection: %w", restoreErr))
			}
		} else if removeErr := os.Remove(spriteSelectionPath); removeErr != nil && !os.IsNotExist(removeErr) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove failed .sprite selection: %w", removeErr))
		}
	}()

	opts.Logger("[seven init] writing .sprite")
	if err := writeSpriteFile(name); err != nil {
		return upResult{}, err
	}

	if err := syncGitIdentity(name, opts); err != nil {
		return upResult{}, err
	}

	assistantState := detectHostAssistantState(opts)
	if err := ensureGhAuthInSprite(name, ghToken, opts); err != nil {
		opts.Logger(fmt.Sprintf("[seven init] gh auth setup failed: %v", err))
	}
	assistantState = syncHostAssistantState(name, assistantState, "[seven init]", opts)

	if repoURL == "" {
		if err := maybeInstallGstack(name, assistantState.PreferredAssistant, gstackDefaultRevision, opts); err != nil {
			return upResult{}, fmt.Errorf("required gstack provisioning failed: %w", err)
		}
		opts.Logger("[seven init] no repo url found, skipping clone")
		return upResult{Name: name, OpenConsole: false, SpriteExists: false}, nil
	}
	// Clone into a directory named after the repo (the sprite family base), not
	// the sprite name, so sibling sprites get "soclimmo" rather than "soclimmo-02".
	repoDir := spriteFamilyBase(name)

	if repoSlug != "" {
		cloneArgs := []string{"repo", "clone", repoSlug, repoDir}
		if repoBranch != "" {
			cloneArgs = append(cloneArgs, "--", "--branch", repoBranch)
			opts.Logger(fmt.Sprintf("[seven init] cloning current host branch: %s", repoBranch))
		}
		if ghToken != "" {
			opts.Logger(fmt.Sprintf("[seven init] cloning via gh repo clone: %s", repoSlug))
			commandArgs := append([]string{"gh"}, cloneArgs...)
			if err := spriteExec(name, []string{"GH_TOKEN=" + ghToken}, opts.QuietExternal, commandArgs...); err != nil {
				return upResult{}, err
			}
		} else {
			opts.Logger(fmt.Sprintf("[seven init] cloning via gh repo clone (no token): %s", repoSlug))
			commandArgs := append([]string{"gh"}, cloneArgs...)
			if err := spriteExec(name, nil, opts.QuietExternal, commandArgs...); err != nil {
				return upResult{}, err
			}
		}
		if err := verifyClonedRepoHead(name, repoDir, repoHead); err != nil {
			return upResult{}, err
		}
		if err := configureConsoleBootstrapInSprite(name, repoDir, assistantState.PreferredAssistant, opts); err != nil {
			opts.Logger(fmt.Sprintf("[seven init] console bootstrap setup failed: %v", err))
		}
		if err := reconcileProjectEnvironment(name, repoDir, assistantState.PreferredAssistant, opts); err != nil {
			return upResult{}, err
		}
		return upResult{Name: name, OpenConsole: false, SpriteExists: false}, nil
	}

	opts.Logger(fmt.Sprintf("[seven init] cloning via git clone: %s", repoURL))
	cloneArgs := []string{"clone"}
	if repoBranch != "" {
		cloneArgs = append(cloneArgs, "--branch", repoBranch)
		opts.Logger(fmt.Sprintf("[seven init] cloning current host branch: %s", repoBranch))
	}
	cloneArgs = append(cloneArgs, repoURL, repoDir)
	commandArgs := append([]string{"git"}, cloneArgs...)
	if err := spriteExec(name, nil, opts.QuietExternal, commandArgs...); err != nil {
		return upResult{}, err
	}
	if err := verifyClonedRepoHead(name, repoDir, repoHead); err != nil {
		return upResult{}, err
	}
	if err := configureConsoleBootstrapInSprite(name, name, assistantState.PreferredAssistant, opts); err != nil {
		opts.Logger(fmt.Sprintf("[seven init] console bootstrap setup failed: %v", err))
	}
	if err := reconcileProjectEnvironment(name, repoDir, assistantState.PreferredAssistant, opts); err != nil {
		return upResult{}, err
	}

	return upResult{Name: name, OpenConsole: false, SpriteExists: false}, nil
}

func runUpWithTUI(opts upOptions) (upResult, error) {
	if !opts.AssumeLoggedIn {
		if err := ensureSpriteCLI(); err != nil {
			return upResult{}, err
		}
		if _, err := spriteList(); err != nil {
			fmt.Println(formatStyledBulletLog("[seven init] logging in to sprite"))
			if err := runCmd(spriteBin(), nil, "login"); err != nil {
				return upResult{}, err
			}
		}
		opts.AssumeLoggedIn = true
	}

	m := newUpModel()
	p := tea.NewProgram(m)

	go func() {
		runOpts := opts
		runOpts.Logger = func(msg string) { p.Send(logMsg(msg)) }
		// TUI mode captures output for cleaner display.
		runOpts.QuietExternal = true
		res, err := runUp(runOpts)
		p.Send(doneMsg{res: res, err: err})
	}()

	final, err := p.Run()
	if err != nil {
		return upResult{}, err
	}
	fm, ok := final.(upModel)
	if !ok {
		return upResult{}, errors.New("unexpected TUI model")
	}
	if fm.err != nil {
		return upResult{}, fm.err
	}
	return fm.res, nil
}

func runConsole(name string) error {
	fmt.Println(formatStyledBulletLog(fmt.Sprintf("[seven up] opening console: %s", name)))
	return runCmd(spriteBin(), nil, "console", "-s", name)
}

func resolveSpriteName() (spriteNameInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return spriteNameInfo{}, err
	}
	name := filepath.Base(cwd)
	fromFile := false
	path := filepath.Join(cwd, ".sprite")
	if data, err := os.ReadFile(path); err == nil {
		trimmed := strings.TrimSpace(string(data))
		if trimmed != "" {
			name = trimmed
			fromFile = true
		}
	}

	info := spriteNameInfo{
		Name:     name,
		FromFile: fromFile,
		Original: name,
	}
	if fromFile {
		if err := validateSpriteName(info.Name); err != nil {
			return spriteNameInfo{}, fmt.Errorf("invalid sprite name in .sprite: %w", err)
		}
		return info, nil
	}

	normalized := normalizeSpriteName(info.Name)
	info.Name = normalized
	info.Normalized = normalized != info.Original
	if err := validateSpriteName(info.Name); err != nil {
		return spriteNameInfo{}, fmt.Errorf("invalid sprite name derived from directory %q: %w (set a valid name in .sprite to override)", info.Original, err)
	}
	return info, nil
}

func resolveTargetSpriteName(opts upOptions) (string, error) {
	if opts.ResolvedName != "" {
		return opts.ResolvedName, nil
	}
	if opts.SpriteName != "" {
		if err := validateSpriteName(opts.SpriteName); err != nil {
			return "", err
		}
		return opts.SpriteName, nil
	}

	info, err := resolveSpriteName()
	if err != nil {
		return "", err
	}

	if opts.SiblingOrdinal > 0 {
		base := info.Name
		if info.FromFile {
			base = spriteFamilyBase(info.Name)
		}
		name := siblingSpriteNameForOrdinal(base, opts.SiblingOrdinal)
		if err := validateSpriteName(name); err != nil {
			return "", err
		}
		return name, nil
	}

	if !opts.NewSprite {
		return info.Name, nil
	}

	base := info.Name
	if info.FromFile {
		base = spriteFamilyBase(info.Name)
	}
	listOut, err := spriteList()
	if err != nil {
		return "", err
	}
	return nextSiblingSpriteName(base, listOut), nil
}

// siblingSpriteNameForOrdinal maps a 1-based family ordinal to a sprite name:
// 1 is the main sprite (the bare base), N>=2 is "<base>-0N" to match the
// sibling naming produced by nextSiblingSpriteName.
func siblingSpriteNameForOrdinal(base string, n int) string {
	if n <= 1 {
		return base
	}
	return fmt.Sprintf("%s-%02d", base, n)
}

// spriteColor returns a stable 256-color code (as a string) for a sprite name,
// chosen by hashing the name into spriteIdentityPalette.
func spriteColor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return strconv.Itoa(spriteIdentityPalette[int(h.Sum32())%len(spriteIdentityPalette)])
}

// spriteFamilyMembers returns the names of sprites in the family rooted at base
// that appear in listOut, sorted by ordinal (main first).
func spriteFamilyMembers(base, listOut string) []string {
	seen := map[string]int{}
	scanner := bufio.NewScanner(strings.NewReader(listOut))
	for scanner.Scan() {
		for _, field := range strings.Fields(scanner.Text()) {
			if _, ok := seen[field]; ok {
				continue
			}
			if ordinal, ok := spriteFamilyOrdinal(base, field); ok {
				seen[field] = ordinal
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return seen[names[i]] < seen[names[j]] })
	return names
}

func spriteFamilyBase(name string) string {
	match := spriteSuffixRe.FindStringSubmatch(name)
	if len(match) != 3 {
		return name
	}
	return match[1]
}

func nextSiblingSpriteName(base string, listOut string) string {
	// Reuse the same field-based family scan that `seven list` uses so the two
	// agree on which sprites exist. An earlier regex approach consumed the
	// newline between adjacent names as a match boundary, so a family of just
	// "<base>" + "<base>-02" only matched the main sprite and `--new` collided
	// with the existing -02 sibling instead of allocating -03.
	//
	// Start at 1 (the main sprite) so the first sibling is always -02, keeping
	// `--new` consistent with `seven up N` and `seven list`.
	maxOrdinal := 1
	for _, name := range spriteFamilyMembers(base, listOut) {
		if ordinal, ok := spriteFamilyOrdinal(base, name); ok && ordinal > maxOrdinal {
			maxOrdinal = ordinal
		}
	}
	return siblingSpriteNameForOrdinal(base, maxOrdinal+1)
}

// spriteFamilyOrdinal returns the 1-based family number for name: the main
// sprite (bare base) is 1, and "<base>-NN" is NN. This matches the numbering
// shown by `seven list` and accepted by `seven up N`.
func spriteFamilyOrdinal(base, name string) (int, bool) {
	if name == base {
		return 1, true
	}
	match := spriteSuffixRe.FindStringSubmatch(name)
	if len(match) != 3 || match[1] != base {
		return 0, false
	}
	value, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, false
	}
	return value, true
}

func writeSpriteFile(name string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path := filepath.Join(cwd, ".sprite")
	return os.WriteFile(path, []byte(name+"\n"), 0o644)
}

func removeSpriteFile() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path := filepath.Join(cwd, ".sprite")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Remove(path)
}

func normalizeSpriteName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	lastHyphen := false

	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if r == '-' {
			if b.Len() > 0 && !lastHyphen {
				b.WriteRune('-')
				lastHyphen = true
			}
			continue
		}
		if b.Len() > 0 && !lastHyphen {
			b.WriteRune('-')
			lastHyphen = true
		}
	}

	return strings.Trim(b.String(), "-")
}

func validateSpriteName(name string) error {
	if name == "" {
		return errors.New("sprite name is empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("sprite name %q is too long (max 63 characters)", name)
	}
	if !spriteNamePattern.MatchString(name) {
		return fmt.Errorf("sprite name %q is invalid (use lowercase letters, numbers, hyphens, start/end with a letter or number)", name)
	}
	return nil
}

func ensureSpriteCLI() error {
	if path, err := exec.LookPath("sprite"); err == nil {
		spritePath = path
		return nil
	}

	if err := installSpriteCLI(); err != nil {
		return err
	}

	if path, err := exec.LookPath("sprite"); err == nil {
		spritePath = path
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return errors.New("sprite CLI installed but could not resolve home directory for ~/.local/bin")
	}
	fallback := filepath.Join(home, ".local", "bin", "sprite")
	if _, err := os.Stat(fallback); err == nil {
		spritePath = fallback
		return nil
	}

	return errors.New("sprite CLI not found after install; ensure ~/.local/bin is on PATH")
}

func maybeUpgradeSpriteCLI(opts upOptions) {
	if os.Getenv("SEVEN_SKIP_SPRITE_UPGRADE") == "1" {
		opts.Logger("[seven up] skipping sprite CLI update check")
		return
	}

	opts.Logger("[seven up] checking sprite CLI updates")
	out, err := runCmdOutput(spriteBin(), nil, "upgrade", "--check")
	if err != nil {
		opts.Logger(fmt.Sprintf("[seven up] sprite upgrade check failed: %v", err))
		return
	}

	latest, current, ok := parseSpriteUpgradeCheckOutput(out)
	if !ok {
		opts.Logger("[seven up] could not parse sprite upgrade check output; skipping auto-upgrade")
		return
	}
	if spriteVersionsEqual(latest, current) {
		opts.Logger(fmt.Sprintf("[seven up] sprite CLI is up to date (%s)", current))
		return
	}

	opts.Logger(fmt.Sprintf("[seven up] upgrading sprite CLI from %s to %s", current, latest))
	if err := runCmdWithInput(spriteBin(), nil, "y\n", "upgrade"); err != nil {
		opts.Logger(fmt.Sprintf("[seven up] sprite CLI upgrade failed: %v", err))
		return
	}

	if err := ensureSpriteCLI(); err != nil {
		opts.Logger(fmt.Sprintf("[seven up] sprite CLI upgraded but refresh failed: %v", err))
		return
	}
	opts.Logger(fmt.Sprintf("[seven up] sprite CLI upgraded to %s", latest))
}

func spriteVersionsEqual(left, right string) bool {
	return strings.TrimPrefix(left, "v") == strings.TrimPrefix(right, "v")
}

func parseSpriteUpgradeCheckOutput(out string) (latest, current string, ok bool) {
	cleaned := ansiEscapeRe.ReplaceAllString(out, "")
	latestMatch := spriteLatestVersionPattern.FindStringSubmatch(cleaned)
	currentMatch := spriteCurrentVersionPattern.FindStringSubmatch(cleaned)
	if len(latestMatch) < 2 || len(currentMatch) < 2 {
		return "", "", false
	}
	return strings.TrimSpace(latestMatch[1]), strings.TrimSpace(currentMatch[1]), true
}

func configureConsoleBootstrapInSprite(spriteName, repoDir, assistant string, opts upOptions) error {
	if err := configureSpriteIdentity(spriteName, opts); err != nil {
		opts.Logger(fmt.Sprintf("[seven init] sprite identity setup failed: %v", err))
	}

	if repoDir == "" || assistant == "" {
		return nil
	}

	opts.Logger(fmt.Sprintf("[seven init] configuring first console launch: cd %s (assistant: %s)", repoDir, assistant))
	env := []string{"SEVEN_REPO_DIR=" + repoDir + ",SEVEN_ASSISTANT=" + assistant}
	cmd := `set -e
cat > "` + sevenConsoleHookPath + `" <<'EOF'
# seven one-shot console bootstrap
case "$-" in
  *i*) ;;
  *) return 0 ;;
esac

if [ -n "${SEVEN_CONSOLE_BOOTSTRAP_RUNNING:-}" ]; then
  return 0
fi

marker="` + sevenConsoleMarkerPath + `"
if [ ! -f "$marker" ]; then
  return 0
fi

repo_path="$(sed -n '1p' "$marker")"
assistant_cmd="$(sed -n '2p' "$marker")"
rm -f "$marker"

if [ -n "$repo_path" ] && [ -d "$repo_path" ]; then
  cd "$repo_path" || true
fi

if [ -n "$assistant_cmd" ] && command -v "$assistant_cmd" >/dev/null 2>&1; then
  printf '\nseven: ready in %s. Run %s when you want.\n\n' "$repo_path" "$assistant_cmd"
fi
EOF
chmod 600 "` + sevenConsoleHookPath + `"

install -d -m 700 "$HOME/.config/fish/conf.d"
cat > "$HOME/.config/fish/conf.d/seven-console.fish" <<'EOF'
# seven one-shot console bootstrap for fish
status is-interactive; or exit 0

if set -q SEVEN_CONSOLE_BOOTSTRAP_RUNNING
  exit 0
end

set marker "$HOME/.seven-console-once"
if not test -f "$marker"
  exit 0
end

set repo_path (sed -n '1p' "$marker")
set assistant_cmd (sed -n '2p' "$marker")
rm -f "$marker"

if test -n "$repo_path"; and test -d "$repo_path"
  cd "$repo_path"
end

if test -n "$assistant_cmd"
  if command -q "$assistant_cmd"
    printf '\nseven: ready in %s. Run %s when you want.\n\n' "$repo_path" "$assistant_cmd"
  end
end
EOF
chmod 600 "$HOME/.config/fish/conf.d/seven-console.fish"

for rc in "$HOME/.bash_profile" "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.zprofile"; do
  touch "$rc"
  grep -Fqx '[ -f "` + sevenConsoleHookPath + `" ] && . "` + sevenConsoleHookPath + `"' "$rc" || printf '\n%s\n' '[ -f "` + sevenConsoleHookPath + `" ] && . "` + sevenConsoleHookPath + `"' >> "$rc"
done

printf '%s\n%s\n' "$HOME/$SEVEN_REPO_DIR" "$SEVEN_ASSISTANT" > "` + sevenConsoleMarkerPath + `"
chmod 600 "` + sevenConsoleMarkerPath + `"
`
	return spriteExec(spriteName, env, opts.QuietExternal, "sh", "-lc", cmd)
}

// maybeInstallGstack installs gstack inside the sprite when explicitly requested
// or declared by the repository tooling manifest. Setup targets every supported
// assistant present in the sprite, so Claude Code and Codex share one checkout.
func maybeInstallGstack(spriteName, assistant, revision string, opts upOptions) error {
	if !opts.InstallGstack {
		return nil
	}
	_ = assistant
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) {
		return fmt.Errorf("gstack revision must be a full commit SHA, got %q", revision)
	}

	if err := spriteExec(spriteName, nil, true, "sh", "-lc", "command -v bun >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("bun is required for gstack but is absent from the Sprite image")
	}

	// Fetch and verify an immutable revision in a fresh staging repository. We
	// never run Git or tool code through an existing checkout: even .git/config,
	// the index, ignored dependencies, and generated binaries are untrusted.
	opts.Logger("[seven init] installing gstack into sprite (includes a browser download; can take a few minutes)")
	install := `set -e
export PATH="$HOME/.bun/bin:$HOME/.local/bin:$PATH"
gstack_parent="$(dirname "` + gstackSkillDir + `")"
mkdir -p "$gstack_parent"
gstack_staging="$(mktemp -d "$gstack_parent/.gstack-seven.XXXXXX")"
gstack_backup="$gstack_parent/.gstack-seven-old"
cleanup_gstack_staging() { [ -z "$gstack_staging" ] || rm -rf "$gstack_staging"; }
trap cleanup_gstack_staging EXIT
git -c core.hooksPath=/dev/null -C "$gstack_staging" init
git -c core.hooksPath=/dev/null -C "$gstack_staging" remote add origin "` + gstackRepoURL + `"
git -c core.hooksPath=/dev/null -C "$gstack_staging" fetch --depth 1 origin "` + revision + `"
[ "$(git -c core.hooksPath=/dev/null -C "$gstack_staging" rev-parse FETCH_HEAD)" = "` + revision + `" ]
git -c core.hooksPath=/dev/null -C "$gstack_staging" checkout --detach "` + revision + `"
[ "$(git -c core.hooksPath=/dev/null -C "$gstack_staging" rev-parse HEAD)" = "` + revision + `" ]
rm -rf "$gstack_backup"
if [ -e "` + gstackSkillDir + `" ] || [ -L "` + gstackSkillDir + `" ]; then mv "` + gstackSkillDir + `" "$gstack_backup"; fi
if ! mv "$gstack_staging" "` + gstackSkillDir + `"; then
  if [ -e "$gstack_backup" ] || [ -L "$gstack_backup" ]; then mv "$gstack_backup" "` + gstackSkillDir + `"; fi
  exit 1
fi
gstack_staging=""
rm -rf "$gstack_backup"
cd "` + gstackSkillDir + `"
bun install --frozen-lockfile
` + gstackPlaywrightPlatformCmd + `
` + gstackChromiumDepsCmd + `
./setup --host auto --no-team`
	if out, err := spriteExecOutput(spriteName, nil, "sh", "-lc", install); err != nil {
		return fmt.Errorf("gstack setup failed: %w%s", err, gstackOutputTail(out))
	}
	opts.Logger("[seven init] gstack installed")
	return nil
}

// gstackOutputTail formats the last few lines of captured command output for
// inclusion in an error, so failures (e.g. the gstack browser download) are
// diagnosable instead of surfacing only "exit status 1".
func gstackOutputTail(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return "\n  " + strings.Join(lines, "\n  ")
}

// projectToolingManifestRelPath is the conventional path, within a cloned repo, of a project's
// declarative tooling manifest. A project opts into auto-install simply by shipping this file —
// one tool per line: "kind name npm-spec(pinned) verify-command". This keeps seven entirely
// project-agnostic: it hardcodes no project's dependencies, it just honors whatever the pulled
// repo declares.
const projectToolingManifestRelPath = "scripts/sprite-tooling.manifest"

var toolingNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var pythonModulePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var toolingVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var forbiddenToolNames = map[string]bool{
	"alias": true, "break": true, "cd": true, "command": true, "continue": true,
	"echo": true, "eval": true, "exec": true, "export": true, "false": true,
	"hash": true, "printf": true, "pwd": true, "read": true, "readonly": true,
	"return": true, "set": true, "shift": true, "source": true, "test": true,
	"times": true, "trap": true, "true": true, "type": true, "ulimit": true,
	"umask": true, "unalias": true, "unset": true, "wait": true,
}

type validatedToolingManifest struct {
	gstackRevision string
	rows           []string
}

func (manifest validatedToolingManifest) normalized() string {
	return strings.Join(manifest.rows, "\n")
}

// readProjectToolingManifest reads and validates the complete manifest before
// any install mechanism runs. Parsing in Go avoids shell-evaluation hazards and
// catches duplicate, malformed, unknown, and non-newline-terminated rows.
func readProjectToolingManifest(spriteName, repoDir string) (validatedToolingManifest, bool, error) {
	manifestPath := "$HOME/" + repoDir + "/" + projectToolingManifestRelPath
	presenceCmd := `if [ -f "` + manifestPath + `" ]; then printf 'present'; elif [ -e "` + manifestPath + `" ]; then exit 2; else printf 'absent'; fi`
	presence, err := spriteExecOutput(spriteName, nil, "sh", "-lc", presenceCmd)
	if err != nil {
		return validatedToolingManifest{}, false, fmt.Errorf("probe project tooling manifest: %w", err)
	}
	switch strings.TrimSpace(presence) {
	case "absent":
		return validatedToolingManifest{}, false, nil
	case "present":
	default:
		return validatedToolingManifest{}, false, fmt.Errorf("probe project tooling manifest: unexpected response %q", strings.TrimSpace(presence))
	}
	out, err := spriteExecOutput(spriteName, nil, "sh", "-lc", `cat "`+manifestPath+`"`)
	if err != nil {
		return validatedToolingManifest{}, true, fmt.Errorf("read project tooling manifest: %w", err)
	}
	manifest, err := parseProjectToolingManifest(out)
	if err != nil {
		return validatedToolingManifest{}, true, err
	}
	return manifest, true, nil
}

func validateProjectToolingManifest(contents string) (string, error) {
	manifest, err := parseProjectToolingManifest(contents)
	return manifest.gstackRevision, err
}

func parseProjectToolingManifest(contents string) (validatedToolingManifest, error) {
	manifest := validatedToolingManifest{}
	reservedNames := map[string]bool{}
	reserve := func(name string, line int) error {
		if reservedNames[name] {
			return fmt.Errorf("invalid project tooling manifest line %d: duplicate tool name or alias %q", line, name)
		}
		reservedNames[name] = true
		return nil
	}

	for index, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		kind := fields[0]
		if kind == "gstack" {
			if len(fields) != 4 || fields[1] != "gstack" || fields[3] != "-" ||
				!regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(fields[2]) {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: malformed gstack row", index+1)
			}
			if err := reserve("gstack", index+1); err != nil {
				return validatedToolingManifest{}, err
			}
			manifest.gstackRevision = fields[2]
			manifest.rows = append(manifest.rows, strings.Join(fields, " "))
			continue
		}
		if len(fields) != 5 {
			return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: expected five fields", index+1)
		}
		name, packageSpec, verifyName, verifyArg := fields[1], fields[2], fields[3], fields[4]
		if !toolingNamePattern.MatchString(name) || forbiddenToolNames[name] {
			return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: unsafe name", index+1)
		}
		if err := reserve(name, index+1); err != nil {
			return validatedToolingManifest{}, err
		}
		switch kind {
		case "npm":
			if verifyName != name || (verifyArg != "--version" && verifyArg != "version") {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: unsafe verifier", index+1)
			}
			prefix := name + "@"
			if !strings.HasPrefix(packageSpec, prefix) || !toolingVersionPattern.MatchString(strings.TrimPrefix(packageSpec, prefix)) {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: npm spec must be an exact name@version pin", index+1)
			}
		case "pip":
			if verifyName != name || (verifyArg != "--version" && verifyArg != "version") {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: unsafe verifier", index+1)
			}
			prefix := name + "=="
			if !strings.HasPrefix(packageSpec, prefix) || !toolingVersionPattern.MatchString(strings.TrimPrefix(packageSpec, prefix)) {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: pip spec must be an exact name==version pin", index+1)
			}
		case "pip-module":
			prefix := name + "=="
			version := strings.TrimPrefix(packageSpec, prefix)
			if !strings.HasPrefix(packageSpec, prefix) || !toolingVersionPattern.MatchString(version) ||
				!pythonModulePattern.MatchString(verifyName) || verifyArg != version {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: pip-module requires exact package, module, and matching version", index+1)
			}
		case "archive":
			if verifyName != name || (verifyArg != "--version" && verifyArg != "version") {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: unsafe verifier", index+1)
			}
			parts := strings.Split(packageSpec, "|")
			if len(parts) != 6 || !toolingVersionPattern.MatchString(parts[0]) ||
				!strings.HasPrefix(parts[1], "https://") || strings.Count(parts[1], "{arch}") != 1 ||
				!sha256Pattern.MatchString(parts[2]) || !sha256Pattern.MatchString(parts[3]) ||
				!toolingNamePattern.MatchString(parts[4]) {
				return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: malformed archive spec", index+1)
			}
			for _, alias := range strings.Split(parts[5], ",") {
				if alias != "-" && !toolingNamePattern.MatchString(alias) {
					return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: unsafe archive alias", index+1)
				}
				if alias != "-" {
					if err := reserve(alias, index+1); err != nil {
						return validatedToolingManifest{}, err
					}
				}
			}
		default:
			return validatedToolingManifest{}, fmt.Errorf("invalid project tooling manifest line %d: unsupported kind %q", index+1, kind)
		}
		manifest.rows = append(manifest.rows, strings.Join(fields, " "))
	}
	return manifest, nil
}

// reconcileProjectEnvironment is the single provisioning path for new and
// existing sprites. Every seven up repairs missing gstack/browser artifacts and
// reruns Seven's typed pinned-tool reconciler.
func reconcileProjectEnvironment(spriteName, repoDir, assistant string, opts upOptions) error {
	manifest, manifestPresent, err := readProjectToolingManifest(spriteName, repoDir)
	if err != nil {
		return err
	}
	revision := manifest.gstackRevision
	required := revision != ""
	reconcileOpts := opts
	reconcileOpts.InstallGstack = opts.InstallGstack || required
	if !required {
		revision = gstackDefaultRevision
	}
	if err := maybeInstallGstack(spriteName, assistant, revision, reconcileOpts); err != nil {
		return fmt.Errorf("required gstack provisioning failed: %w", err)
	}
	if err := maybeInstallProjectTooling(spriteName, manifest, manifestPresent, opts); err != nil {
		return fmt.Errorf("required project tooling provisioning failed: %w", err)
	}
	return nil
}

// projectToolingInstallScript builds Seven's typed manifest interpreter. It
// deliberately supports a small fixed set of install mechanisms and never evals
// repository text. All declared rows are required: drift or install failure
// returns non-zero and prevents entry into a partially provisioned Sprite.
func projectToolingInstallScript(manifestContents string) string {
	return `set -u
PATH="$HOME/.local/bin:$PATH"
export PATH
if command -v npm >/dev/null 2>&1; then
  NPM_BIN="$(npm prefix -g 2>/dev/null)/bin"
  case ":$PATH:" in *":$NPM_BIN:"*) ;; *) PATH="$NPM_BIN:$PATH"; export PATH ;; esac
fi
present="" installed="" failed=""

verify_pinned() {
  verify_name="$1" expected="$2" verify="$3"
  case "$verify" in
    "$verify_name --version") verify_arg="--version" ;;
    "$verify_name version") verify_arg="version" ;;
    *) return 1 ;;
  esac
  verify_path="$(command -v "$verify_name" 2>/dev/null)" || return 1
  case "$verify_path" in /*) ;; *) return 1 ;; esac
  [ -f "$verify_path" ] && [ -x "$verify_path" ] || return 1
  verify_output="$("$verify_path" "$verify_arg" 2>&1)" || return 1
  set -f
  for verify_word in $verify_output; do
    case "$verify_word" in "$expected"|"v$expected") set +f; return 0 ;; esac
  done
  set +f
  return 1
}

verify_python_module() {
  module_dist="$1" module_name="$2" expected="$3"
  command -v python3 >/dev/null 2>&1 || return 1
  python3 -c 'import importlib.metadata as m, importlib.util, pathlib, sys; d=m.distribution(sys.argv[1]); s=importlib.util.find_spec(sys.argv[2]); owned={pathlib.Path(d.locate_file(f)).resolve() for f in (d.files or [])}; origin=pathlib.Path(s.origin).resolve() if s and s.origin else None; providers=[p.lower() for p in m.packages_distributions().get(sys.argv[2], [])]; raise SystemExit(d.version != sys.argv[3] or d.metadata["Name"].lower() not in providers or origin not in owned)' "$module_dist" "$module_name" "$expected" >/dev/null 2>&1
}

install_archive() {
  archive_name="$1" archive_spec="$2"
  old_ifs="$IFS"; IFS='|'; set -f; set -- $archive_spec; set +f; IFS="$old_ifs"
  [ "$#" -eq 6 ] || return 1
  archive_version="$1" url_template="$2" sha_x86="$3" sha_arm="$4" archive_binary="$5" archive_aliases="$6"
  case "$url_template" in https://*) ;; *) return 1 ;; esac
  case "$sha_x86$sha_arm" in *[!0-9a-f]*) return 1 ;; esac
  [ "${#sha_x86}" -eq 64 ] && [ "${#sha_arm}" -eq 64 ] || return 1
  case "$archive_binary$archive_aliases" in *[!A-Za-z0-9._,-]*) return 1 ;; esac
  case "$(uname -m)" in
    x86_64|amd64) archive_arch="x86_64"; archive_sha="$sha_x86" ;;
    aarch64|arm64) archive_arch="arm64"; archive_sha="$sha_arm" ;;
    *) return 1 ;;
  esac
  archive_url="$(printf '%s' "$url_template" | sed "s/{arch}/$archive_arch/g")"
  archive_tmp="$(mktemp -d)" || return 1
  if ! curl -fsSL "$archive_url" -o "$archive_tmp/archive.tgz" ||
     ! printf '%s  %s\n' "$archive_sha" "$archive_tmp/archive.tgz" | sha256sum -c - >/dev/null 2>&1 ||
     ! tar -xzf "$archive_tmp/archive.tgz" -C "$archive_tmp" "$archive_binary" >/dev/null 2>&1; then
    rm -rf "$archive_tmp"; return 1
  fi
  mkdir -p "$HOME/.local/bin" || { rm -rf "$archive_tmp"; return 1; }
  install -m 0755 "$archive_tmp/$archive_binary" "$HOME/.local/bin/$archive_name" || {
    rm -rf "$archive_tmp"; return 1;
  }
  old_ifs="$IFS"; IFS=','; set -f; set -- $archive_aliases; set +f; IFS="$old_ifs"
  for archive_alias in "$@"; do
    [ "$archive_alias" = "-" ] || ln -sf "$archive_name" "$HOME/.local/bin/$archive_alias" || {
      rm -rf "$archive_tmp"; return 1;
    }
  done
  rm -rf "$archive_tmp"
  return 0
}

while read -r kind name spec verify || [ -n "$kind$name$spec$verify" ]; do
  case "$kind" in ''|\#*) continue ;; esac
  [ "$kind" = gstack ] && continue
  expected=""
  case "$kind" in
    npm)
      case "$spec" in "$name"@[0-9]*.[0-9]*.[0-9]*) expected="${spec##*@}" ;; *) failed="$failed $name"; continue ;; esac
      case "$expected" in *[!0-9.]*|.*|*.|*.*.*.*) failed="$failed $name"; continue ;; esac
      if verify_pinned "$name" "$expected" "$verify"; then present="$present $name"
      elif command -v npm >/dev/null 2>&1 && npm i -g -- "$spec" >/dev/null 2>&1 && verify_pinned "$name" "$expected" "$verify"; then installed="$installed $name"
      else failed="$failed $name"; fi
      ;;
    pip)
      case "$spec" in "$name"==[0-9]*.[0-9]*.[0-9]*) expected="${spec##*==}" ;; *) failed="$failed $name"; continue ;; esac
      case "$expected" in *[!0-9.]*|.*|*.|*.*.*.*) failed="$failed $name"; continue ;; esac
      if verify_pinned "$name" "$expected" "$verify"; then present="$present $name"
      elif command -v python3 >/dev/null 2>&1 && python3 -m pip install --user -- "$spec" >/dev/null 2>&1 && verify_pinned "$name" "$expected" "$verify"; then installed="$installed $name"
      else failed="$failed $name"; fi
      ;;
    pip-module)
      expected="${spec##*==}"
      old_ifs="$IFS"; IFS=' '; set -f; set -- $verify; set +f; IFS="$old_ifs"
      module_name="$1"
      if verify_python_module "$name" "$module_name" "$expected"; then present="$present $name"
      elif command -v python3 >/dev/null 2>&1 && python3 -m pip install --user -- "$spec" >/dev/null 2>&1 && verify_python_module "$name" "$module_name" "$expected"; then installed="$installed $name"
      else failed="$failed $name"; fi
      ;;
    archive)
      expected="${spec%%|*}"
      if verify_pinned "$name" "$expected" "$verify"; then present="$present $name"
      elif install_archive "$name" "$spec" && verify_pinned "$name" "$expected" "$verify"; then installed="$installed $name"
      else failed="$failed $name"; fi
      ;;
    *) failed="$failed $name" ;;
  esac
done <<'SEVEN_TOOLING_MANIFEST'
` + manifestContents + `
SEVEN_TOOLING_MANIFEST
echo "[project-tooling] present:${present:- none} | installed:${installed:- none} | failed:${failed:- none}"
[ -z "$failed" ]`
}

// maybeInstallProjectTooling reconciles a repo's declared CLI/MCP tooling using
// Seven's typed interpreter. A missing manifest is the common no-op; a present
// manifest is a required contract and failures propagate to the caller.
func maybeInstallProjectTooling(spriteName string, manifest validatedToolingManifest, manifestPresent bool, opts upOptions) error {
	if !manifestPresent {
		return nil
	}
	opts.Logger("[seven up] project tooling manifest found — reconciling pinned tools")
	out, err := spriteExecOutput(spriteName, nil, "sh", "-lc", projectToolingInstallScript(manifest.normalized()))
	if err != nil {
		return fmt.Errorf("project tooling install failed: %w%s", err, gstackOutputTail(out))
	}
	if s := strings.TrimSpace(out); s != "" {
		opts.Logger("[seven up] " + s)
	}
	return nil
}

// configureSpriteIdentity installs a persistent, color-coded shell prompt and a
// one-line banner inside the sprite so it is obvious which sprite a console
// belongs to. The color is derived from the sprite name (see spriteColor) and is
// stable across sessions, so sibling sprites stay visually distinct. It also
// defines assistant aliases for full-permission Claude and Codex sessions (safe
// in a disposable sandbox). Snippets are written for bash, zsh, and fish and
// sourced from the usual rc files.
func configureSpriteIdentity(spriteName string, opts upOptions) error {
	if spriteName == "" {
		return nil
	}
	color := spriteColor(spriteName)
	opts.Logger(fmt.Sprintf("[seven init] configuring sprite identity prompt: %s", spriteName))
	env := []string{"SEVEN_SPRITE_NAME=" + spriteName + ",SEVEN_SPRITE_COLOR=" + color}
	idPath := sevenSpriteIdentityPath
	cmd := `set -e
{
  printf 'SEVEN_SPRITE_NAME=%s\n' "$SEVEN_SPRITE_NAME"
  printf 'SEVEN_SPRITE_COLOR=%s\n' "$SEVEN_SPRITE_COLOR"
  cat <<'EOF'
# seven sprite identity: colored prompt + banner + assistant aliases
case "$-" in
  *i*)
    # Sprites are disposable sandboxes, so run assistants with full permissions.
    alias c='claude --dangerously-skip-permissions'
    alias c2='codex --dangerously-bypass-approvals-and-sandbox'
    if [ -z "${SEVEN_SPRITE_PROMPT_SET:-}" ]; then
      SEVEN_SPRITE_PROMPT_SET=1
      __seven_c="${SEVEN_SPRITE_COLOR:-7}"
      __seven_n="${SEVEN_SPRITE_NAME:-sprite}"
      if [ -n "${ZSH_VERSION:-}" ]; then
        PROMPT="%{$(printf '\033[1;38;5;%sm' "$__seven_c")%}[$__seven_n]%{$(printf '\033[0m')%} $PROMPT"
      elif [ -n "${BASH_VERSION:-}" ]; then
        PS1="\[$(printf '\033[1;38;5;%sm' "$__seven_c")\][$__seven_n]\[$(printf '\033[0m')\] $PS1"
      fi
      printf '\033[1;38;5;%sm# sprite: %s\033[0m\n' "$__seven_c" "$__seven_n"
    fi
    ;;
esac
EOF
} > "` + idPath + `"
chmod 600 "` + idPath + `"

install -d -m 700 "$HOME/.config/fish/conf.d"
{
  printf 'set -g __seven_sprite_name %s\n' "$SEVEN_SPRITE_NAME"
  printf 'set -g __seven_sprite_color %s\n' "$SEVEN_SPRITE_COLOR"
  cat <<'EOF'
status is-interactive; or exit 0
# Sprites are disposable sandboxes, so run assistants with full permissions.
alias c 'claude --dangerously-skip-permissions'
alias c2 'codex --dangerously-bypass-approvals-and-sandbox'
if not functions -q __seven_orig_fish_prompt
  if functions -q fish_prompt
    functions -c fish_prompt __seven_orig_fish_prompt
  end
end
function fish_prompt
  printf '\033[1;38;5;%sm[%s]\033[0m ' $__seven_sprite_color $__seven_sprite_name
  if functions -q __seven_orig_fish_prompt
    __seven_orig_fish_prompt
  end
end
if not set -q __seven_sprite_banner_shown
  set -g __seven_sprite_banner_shown 1
  printf '\033[1;38;5;%sm# sprite: %s\033[0m\n' $__seven_sprite_color $__seven_sprite_name
end
EOF
} > "$HOME/.config/fish/conf.d/seven-sprite-id.fish"
chmod 600 "$HOME/.config/fish/conf.d/seven-sprite-id.fish"

for rc in "$HOME/.bash_profile" "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.zprofile"; do
  touch "$rc"
  grep -Fqx '[ -f "` + idPath + `" ] && . "` + idPath + `"' "$rc" || printf '\n%s\n' '[ -f "` + idPath + `" ] && . "` + idPath + `"' >> "$rc"
done
`
	return spriteExec(spriteName, env, opts.QuietExternal, "sh", "-lc", cmd)
}

func spriteList() (string, error) {
	out, err := runCmdOutput(spriteBin(), nil, "list")
	if err != nil {
		return "", err
	}
	return out, nil
}

func spriteExists(name string) (bool, error) {
	out, err := spriteList()
	if err != nil {
		return false, err
	}
	if name == "" {
		return false, nil
	}
	return spriteListedInOutput(out, name), nil
}

func spriteListedInOutput(out, name string) bool {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == name {
			return true
		}
		for _, field := range strings.Fields(line) {
			if field == name {
				return true
			}
		}
	}
	return false
}

// detectRepoCheckout returns a clean branch + commit identity. A dirty or
// detached checkout cannot be reproduced by cloning and therefore must never
// be used as evidence from a supposedly clean Sprite.
func detectRepoCheckout(opts upOptions) (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	dirty, err := runCmdOutput("git", nil, "-C", cwd, "status", "--porcelain", "--untracked-files=normal", "--", ".", ":(exclude).sprite")
	if err != nil {
		return "", "", fmt.Errorf("inspect host checkout: %w", err)
	}
	if strings.TrimSpace(dirty) != "" {
		return "", "", fmt.Errorf("host checkout is dirty; commit and push it before creating a reproducible Sprite")
	}
	branch, err := runCmdOutput("git", nil, "-C", cwd, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("host checkout is detached; use a pushed branch before creating a reproducible Sprite")
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", "", fmt.Errorf("host checkout has no branch")
	}
	if _, err := runCmdOutput("git", nil, "check-ref-format", "--branch", branch); err != nil {
		opts.Logger(fmt.Sprintf("[seven init] ignoring invalid host branch %q", branch))
		return "", "", fmt.Errorf("invalid host branch %q", branch)
	}
	head, err := runCmdOutput("git", nil, "-C", cwd, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("resolve host HEAD: %w", err)
	}
	head = strings.TrimSpace(head)
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(head) {
		return "", "", fmt.Errorf("invalid host HEAD %q", head)
	}
	return branch, head, nil
}

func verifyClonedRepoHead(spriteName, repoDir, expectedHead string) error {
	if expectedHead == "" {
		return nil
	}
	cmd := `[ "$(git -C "$HOME/` + repoDir + `" rev-parse HEAD)" = "` + expectedHead + `" ]`
	if err := spriteExec(spriteName, nil, true, "sh", "-lc", cmd); err != nil {
		return fmt.Errorf("cloned Sprite HEAD does not match host HEAD %s; push the branch and retry", expectedHead)
	}
	return nil
}

func detectRepoInfo(spriteName string, opts upOptions) (string, string, string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		opts.Logger("[seven init] git not found")
		return "", "", "", nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", err
	}

	inside, err := runCmdOutput("git", nil, "-C", cwd, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		opts.Logger("[seven init] not inside a git repo")
		return "", "", "", nil
	}

	remotes, err := runCmdOutput("git", nil, "-C", cwd, "remote")
	if err != nil {
		return "", "", "", err
	}
	remotes = strings.TrimSpace(remotes)
	if remotes != "" {
		opts.Logger(fmt.Sprintf("[seven init] git remotes: %s", remotes))
	}

	if !hasOriginRemote(remotes) {
		return "", "", "", nil
	}

	repoURL, err := runCmdOutput("git", nil, "-C", cwd, "remote", "get-url", "origin")
	if err != nil {
		return "", "", "", err
	}
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", "", "", nil
	}

	opts.Logger(fmt.Sprintf("[seven init] repo url: %s", repoURL))

	repoSlug := githubRepoSlug(repoURL)
	ghToken := ""
	if _, err := exec.LookPath("gh"); err == nil {
		token, err := runCmdOutput("gh", nil, "auth", "token")
		if err == nil {
			ghToken = strings.TrimSpace(token)
		}
	}

	if ghToken != "" {
		opts.Logger("[seven init] detected gh token on host")
	}

	return repoURL, repoSlug, ghToken, nil
}

func ensureGhAuthInSprite(spriteName, ghToken string, opts upOptions) error {
	if ghToken == "" {
		return nil
	}

	opts.Logger("[seven init] configuring gh auth inside sprite")
	env := []string{"GH_TOKEN=" + ghToken}
	if err := spriteExec(spriteName, env, opts.QuietExternal, "sh", "-lc", "command -v gh >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("gh not found in sprite: %w", err)
	}
	loginCmd := "token=\"$GH_TOKEN\"; unset GH_TOKEN; printf '%s' \"$token\" | gh auth login --with-token -h github.com"
	if out, err := spriteExecOutput(spriteName, env, "sh", "-lc", loginCmd); err != nil {
		msg := strings.TrimSpace(out)
		if msg != "" {
			return fmt.Errorf("gh auth login failed: %w (%s)", err, msg)
		}
		return fmt.Errorf("gh auth login failed: %w", err)
	}
	if out, err := spriteExecOutput(spriteName, env, "gh", "auth", "setup-git"); err != nil {
		msg := strings.TrimSpace(out)
		if msg != "" {
			return fmt.Errorf("gh auth setup-git failed: %w (%s)", err, msg)
		}
		return fmt.Errorf("gh auth setup-git failed: %w", err)
	}
	return nil
}

func detectHostAssistantState(opts upOptions) hostAssistantState {
	state := hostAssistantState{
		PreferredAssistant: sevenDefaultAssistant,
	}
	state.ClaudeAuthPath = detectHostClaudeAuth(opts)
	state.ClaudeConfigPath = detectHostClaudeConfig(opts)
	state.ClaudeCredentials = detectHostClaudeCredentials(opts)
	state.CodexAuthPath = detectHostCodexChatGPTAuth(opts)
	state.CodexConfigPath = detectHostCodexConfig(opts)
	if opts.Assistant != "" {
		state.PreferredAssistant = opts.Assistant
		return state
	}

	switch {
	case state.ClaudeAuthPath != "":
		state.PreferredAssistant = "claude"
	case state.CodexAuthPath != "":
		state.PreferredAssistant = "codex"
	}

	return state
}

func syncHostAssistantState(spriteName string, state hostAssistantState, phase string, opts upOptions) hostAssistantState {
	if err := ensureClaudeConfigInSprite(spriteName, state.ClaudeConfigPath, opts); err != nil {
		opts.Logger(fmt.Sprintf("%s claude config setup failed: %v", phase, err))
	}
	if err := ensureClaudeAuthInSprite(spriteName, state.ClaudeAuthPath, opts); err != nil {
		opts.Logger(fmt.Sprintf("%s claude auth setup failed: %v", phase, err))
	}
	if err := ensureClaudeCredentialsInSprite(spriteName, state.ClaudeCredentials, opts); err != nil {
		opts.Logger(fmt.Sprintf("%s claude credentials setup failed: %v", phase, err))
	}
	if err := ensureCodexConfigInSprite(spriteName, state.CodexConfigPath, opts); err != nil {
		opts.Logger(fmt.Sprintf("%s codex config setup failed: %v", phase, err))
	}
	if err := ensureCodexAuthInSprite(spriteName, state.CodexAuthPath, opts); err != nil {
		opts.Logger(fmt.Sprintf("%s codex auth setup failed: %v", phase, err))
	}

	state.PreferredAssistant = resolvePreferredAssistantInSprite(spriteName, state, phase, opts)
	return state
}

func resolvePreferredAssistantInSprite(spriteName string, state hostAssistantState, phase string, opts upOptions) string {
	if opts.Assistant != "" {
		return opts.Assistant
	}
	if loggedIn, err := spriteClaudeLoggedIn(spriteName); err != nil {
		opts.Logger(fmt.Sprintf("%s claude auth validation failed: %v", phase, err))
	} else if loggedIn {
		return "claude"
	} else if state.ClaudeAuthPath != "" || state.ClaudeCredentials.present() {
		opts.Logger(fmt.Sprintf("%s claude auth is not usable in sprite; run 'claude' inside the sprite to log in (or 'codex login'), then retry. Falling back to %s", phase, sevenDefaultAssistant))
	}

	if state.CodexAuthPath != "" {
		return "codex"
	}

	return sevenDefaultAssistant
}

func spriteClaudeLoggedIn(spriteName string) (bool, error) {
	if err := spriteExec(spriteName, nil, true, "sh", "-lc", "command -v claude >/dev/null 2>&1"); err != nil {
		return false, nil
	}

	status, err := spriteExecOutput(spriteName, nil, "claude", "auth", "status", "--json")
	if loggedIn, ok := parseClaudeAuthStatus(status); ok {
		return loggedIn, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func parseClaudeAuthStatus(status string) (bool, bool) {
	var parsed struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal([]byte(status), &parsed); err != nil {
		return false, false
	}
	return parsed.LoggedIn, true
}

func detectHostClaudeAuth(opts upOptions) string {
	if _, err := exec.LookPath("claude"); err != nil {
		return ""
	}

	status, err := runCmdOutput("claude", nil, "auth", "status", "--json")
	if err != nil && strings.TrimSpace(status) == "" {
		return ""
	}
	loggedIn, ok := parseClaudeAuthStatus(status)
	if !ok || !loggedIn {
		return ""
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	authPath := filepath.Join(home, ".claude.json")
	info, err := os.Stat(authPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}

	opts.Logger("[seven init] detected host Claude Code auth")
	return authPath
}

func detectHostClaudeConfig(opts upOptions) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configPath := filepath.Join(home, ".claude", "settings.json")
	info, err := os.Stat(configPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}

	opts.Logger("[seven init] detected host Claude Code config")
	return configPath
}

// detectHostClaudeCredentials locates the host's Claude Code OAuth credential
// store (access + refresh tokens). On macOS this is the login Keychain; on Linux
// it is ~/.claude/.credentials.json. Syncing this — not just ~/.claude.json —
// is what keeps Claude usable in the sprite after the host token is refreshed.
func detectHostClaudeCredentials(opts upOptions) claudeCredentialsSource {
	if runtime.GOOS == "darwin" {
		if claudeKeychainHasCredentials() {
			opts.Logger("[seven init] detected host Claude Code credentials (macOS keychain)")
			return claudeCredentialsSource{Keychain: true}
		}
		return claudeCredentialsSource{}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return claudeCredentialsSource{}
	}
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	info, err := os.Stat(credPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return claudeCredentialsSource{}
	}
	opts.Logger("[seven init] detected host Claude Code credentials")
	return claudeCredentialsSource{FilePath: credPath}
}

// claudeKeychainHasCredentials reports whether the macOS login Keychain holds a
// Claude Code credentials item. It uses a metadata lookup (no -w), so it does
// not read the secret and does not trigger a Keychain access prompt.
func claudeKeychainHasCredentials() bool {
	for _, svc := range claudeKeychainServices {
		if err := exec.Command("security", "find-generic-password", "-s", svc).Run(); err == nil {
			return true
		}
	}
	return false
}

// extractClaudeKeychainCredentials reads the Claude Code credentials JSON out of
// the macOS login Keychain. This may trigger a one-time Keychain access prompt.
func extractClaudeKeychainCredentials() (string, error) {
	var lastErr error
	for _, svc := range claudeKeychainServices {
		out, err := exec.Command("security", "find-generic-password", "-s", svc, "-w").Output()
		if err != nil {
			lastErr = err
			continue
		}
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return trimmed, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no claude credentials found in keychain")
	}
	return "", lastErr
}

func detectHostCodexChatGPTAuth(opts upOptions) string {
	if _, err := exec.LookPath("codex"); err != nil {
		return ""
	}

	status, err := runCmdOutput("codex", nil, "login", "status")
	if err != nil {
		return ""
	}
	if !strings.Contains(status, "Logged in using ChatGPT") {
		return ""
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	authPath := filepath.Join(home, ".codex", "auth.json")
	info, err := os.Stat(authPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}

	opts.Logger("[seven init] detected host codex ChatGPT auth")
	return authPath
}

func detectHostCodexConfig(opts upOptions) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	info, err := os.Stat(configPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}

	opts.Logger("[seven init] detected host codex config")
	return configPath
}

// deepMergeJSON merges src into dst recursively. For nested maps both sides
// are recursed; for everything else src wins. dst is mutated and returned.
func deepMergeJSON(dst, src map[string]interface{}) map[string]interface{} {
	for k, srcVal := range src {
		if dstVal, exists := dst[k]; exists {
			srcMap, srcIsMap := srcVal.(map[string]interface{})
			dstMap, dstIsMap := dstVal.(map[string]interface{})
			if srcIsMap && dstIsMap {
				dst[k] = deepMergeJSON(dstMap, srcMap)
				continue
			}
		}
		dst[k] = srcVal
	}
	return dst
}

// mergedJSONForSprite reads the host JSON file and the sprite's existing copy
// (via spriteReadCmd), deep-merges host values into the sprite's version so
// that sprite-only keys are preserved, and returns the path to the file that
// should be copied into the sprite. When there is no existing sprite file or
// the merge cannot be performed it returns the original hostPath unchanged.
// If a temporary file is created the returned cleanup func removes it.
func mergedJSONForSprite(spriteName, hostPath, spriteReadCmd string) (path string, cleanup func(), err error) {
	hostData, err := os.ReadFile(hostPath)
	if err != nil {
		return "", nil, fmt.Errorf("reading host config: %w", err)
	}
	var hostJSON map[string]interface{}
	if err := json.Unmarshal(hostData, &hostJSON); err != nil {
		return hostPath, nil, nil
	}

	spriteData, readErr := spriteExecOutput(spriteName, nil, "sh", "-lc", spriteReadCmd)
	if readErr != nil || strings.TrimSpace(spriteData) == "" {
		return hostPath, nil, nil
	}

	var spriteJSON map[string]interface{}
	if err := json.Unmarshal([]byte(spriteData), &spriteJSON); err != nil {
		return hostPath, nil, nil
	}

	merged := deepMergeJSON(spriteJSON, hostJSON)

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return hostPath, nil, nil
	}

	tmpFile, err := os.CreateTemp("", "seven-merged-*.json")
	if err != nil {
		return hostPath, nil, nil
	}
	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return hostPath, nil, nil
	}
	tmpFile.Close()

	return tmpFile.Name(), func() { os.Remove(tmpFile.Name()) }, nil
}

func ensureClaudeConfigInSprite(spriteName, hostConfigPath string, opts upOptions) error {
	if hostConfigPath == "" {
		return nil
	}
	if err := spriteExec(spriteName, nil, opts.QuietExternal, "sh", "-lc", "command -v claude >/dev/null 2>&1"); err != nil {
		opts.Logger("[seven init] claude not found in sprite, skipping claude config sync")
		return nil
	}

	opts.Logger("[seven init] syncing claude config into sprite")

	srcPath, cleanup, err := mergedJSONForSprite(spriteName, hostConfigPath, `cat "$HOME/.claude/settings.json" 2>/dev/null`)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	copySpec := srcPath + ":/tmp/host-claude-settings.json"
	cmdArgs := []string{
		"exec",
		"-s", spriteName,
		"-file", copySpec,
		"--",
		"sh", "-lc", "install -d -m 700 \"$HOME/.claude\" && install -m 600 /tmp/host-claude-settings.json \"$HOME/.claude/settings.json\" && rm -f /tmp/host-claude-settings.json",
	}
	if opts.QuietExternal {
		_, err := runCmdOutput(spriteBin(), nil, cmdArgs...)
		return err
	}
	return runCmd(spriteBin(), nil, cmdArgs...)
}

func ensureClaudeAuthInSprite(spriteName, hostAuthPath string, opts upOptions) error {
	if hostAuthPath == "" {
		return nil
	}
	if err := spriteExec(spriteName, nil, opts.QuietExternal, "sh", "-lc", "command -v claude >/dev/null 2>&1"); err != nil {
		opts.Logger("[seven init] claude not found in sprite, skipping claude auth sync")
		return nil
	}

	opts.Logger("[seven init] syncing claude auth into sprite")

	srcPath, cleanup, err := mergedJSONForSprite(spriteName, hostAuthPath, `cat "$HOME/.claude.json" 2>/dev/null`)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	copySpec := srcPath + ":/tmp/host-claude-auth.json"
	cmdArgs := []string{
		"exec",
		"-s", spriteName,
		"-file", copySpec,
		"--",
		"sh", "-lc", "install -m 600 /tmp/host-claude-auth.json \"$HOME/.claude.json\" && rm -f /tmp/host-claude-auth.json",
	}
	if opts.QuietExternal {
		_, err := runCmdOutput(spriteBin(), nil, cmdArgs...)
		return err
	}
	return runCmd(spriteBin(), nil, cmdArgs...)
}

// ensureClaudeCredentialsInSprite copies the host's Claude Code OAuth credential
// store into the sprite's ~/.claude/.credentials.json. Unlike the config/auth
// syncs this overwrites rather than merges: the host token is the freshest copy,
// and we want it to replace any stale token already in the sprite. The source is
// either a host file (Linux) or extracted from the macOS Keychain.
func ensureClaudeCredentialsInSprite(spriteName string, src claudeCredentialsSource, opts upOptions) error {
	if !src.present() {
		return nil
	}
	if err := spriteExec(spriteName, nil, opts.QuietExternal, "sh", "-lc", "command -v claude >/dev/null 2>&1"); err != nil {
		opts.Logger("[seven init] claude not found in sprite, skipping claude credentials sync")
		return nil
	}

	hostPath := src.FilePath
	if src.Keychain {
		data, err := extractClaudeKeychainCredentials()
		if err != nil {
			return fmt.Errorf("reading claude credentials from keychain: %w", err)
		}
		tmpFile, err := os.CreateTemp("", "seven-claude-credentials-*.json")
		if err != nil {
			return err
		}
		defer os.Remove(tmpFile.Name())
		if _, err := tmpFile.WriteString(data); err != nil {
			tmpFile.Close()
			return err
		}
		if err := tmpFile.Chmod(0o600); err != nil {
			tmpFile.Close()
			return err
		}
		tmpFile.Close()
		hostPath = tmpFile.Name()
	}

	opts.Logger("[seven init] syncing claude credentials into sprite")
	copySpec := hostPath + ":/tmp/host-claude-credentials.json"
	cmdArgs := []string{
		"exec",
		"-s", spriteName,
		"-file", copySpec,
		"--",
		"sh", "-lc", "install -d -m 700 \"$HOME/.claude\" && install -m 600 /tmp/host-claude-credentials.json \"$HOME/.claude/.credentials.json\" && rm -f /tmp/host-claude-credentials.json",
	}
	if opts.QuietExternal {
		_, err := runCmdOutput(spriteBin(), nil, cmdArgs...)
		return err
	}
	return runCmd(spriteBin(), nil, cmdArgs...)
}

func ensureCodexConfigInSprite(spriteName, hostConfigPath string, opts upOptions) error {
	if hostConfigPath == "" {
		return nil
	}
	if err := spriteExec(spriteName, nil, opts.QuietExternal, "sh", "-lc", "command -v codex >/dev/null 2>&1"); err != nil {
		opts.Logger("[seven init] codex not found in sprite, skipping codex config sync")
		return nil
	}

	opts.Logger("[seven init] syncing codex config into sprite")
	copySpec := hostConfigPath + ":/tmp/host-codex-config.toml"
	cmdArgs := []string{
		"exec",
		"-s", spriteName,
		"-file", copySpec,
		"--",
		"sh", "-lc", "install -d -m 700 \"$HOME/.codex\" && install -m 600 /tmp/host-codex-config.toml \"$HOME/.codex/config.toml\" && rm -f /tmp/host-codex-config.toml",
	}
	if opts.QuietExternal {
		_, err := runCmdOutput(spriteBin(), nil, cmdArgs...)
		return err
	}
	return runCmd(spriteBin(), nil, cmdArgs...)
}

func ensureCodexAuthInSprite(spriteName, hostAuthPath string, opts upOptions) error {
	if hostAuthPath == "" {
		return nil
	}
	if err := spriteExec(spriteName, nil, opts.QuietExternal, "sh", "-lc", "command -v codex >/dev/null 2>&1"); err != nil {
		opts.Logger("[seven init] codex not found in sprite, skipping codex auth sync")
		return nil
	}

	opts.Logger("[seven init] syncing codex auth into sprite")
	copySpec := hostAuthPath + ":/tmp/host-codex-auth.json"
	cmdArgs := []string{
		"exec",
		"-s", spriteName,
		"-file", copySpec,
		"--",
		"sh", "-lc", "install -d -m 700 \"$HOME/.codex\" && install -m 600 /tmp/host-codex-auth.json \"$HOME/.codex/auth.json\" && rm -f /tmp/host-codex-auth.json",
	}
	if opts.QuietExternal {
		_, err := runCmdOutput(spriteBin(), nil, cmdArgs...)
		return err
	}
	return runCmd(spriteBin(), nil, cmdArgs...)
}

func syncGitIdentity(spriteName string, opts upOptions) error {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}

	name, _ := readGitConfig("user.name")
	email, _ := readGitConfig("user.email")
	if name == "" && email == "" {
		return nil
	}

	opts.Logger("[seven init] syncing git identity into sprite")
	if name != "" {
		if err := spriteExec(spriteName, nil, opts.QuietExternal, "git", "config", "--global", "user.name", name); err != nil {
			return err
		}
	}
	if email != "" {
		if err := spriteExec(spriteName, nil, opts.QuietExternal, "git", "config", "--global", "user.email", email); err != nil {
			return err
		}
	}
	return nil
}

func readGitConfig(key string) (string, error) {
	val, err := runCmdOutput("git", nil, "config", "--get", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(val), nil
}

func hasOriginRemote(remotes string) bool {
	scanner := bufio.NewScanner(strings.NewReader(remotes))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "origin" {
			return true
		}
	}
	return false
}

func githubRepoSlug(repoURL string) string {
	slug := strings.TrimSpace(repoURL)
	if strings.HasPrefix(slug, "git@github.com:") {
		slug = strings.TrimPrefix(slug, "git@github.com:")
	} else if strings.HasPrefix(slug, "https://github.com/") {
		slug = strings.TrimPrefix(slug, "https://github.com/")
	} else if strings.HasPrefix(slug, "http://github.com/") {
		slug = strings.TrimPrefix(slug, "http://github.com/")
	} else if strings.HasPrefix(slug, "ssh://git@github.com/") {
		slug = strings.TrimPrefix(slug, "ssh://git@github.com/")
	} else {
		return ""
	}

	slug = strings.TrimPrefix(slug, "/")
	slug = strings.TrimSuffix(slug, "/")
	if strings.HasSuffix(slug, ".git") {
		slug = strings.TrimSuffix(slug, ".git")
	}
	parts := strings.Split(slug, "/")
	if len(parts) != 2 {
		return ""
	}
	if !githubSlugPartPattern.MatchString(parts[0]) || !githubSlugPartPattern.MatchString(parts[1]) {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func spriteExec(spriteName string, env []string, quiet bool, args ...string) error {
	if spriteName == "" {
		return errors.New("sprite name is empty")
	}
	cmdArgs := []string{"exec", "-s", spriteName}
	for _, kv := range env {
		cmdArgs = append(cmdArgs, "-env", kv)
	}
	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, args...)

	if quiet {
		_, err := runCmdOutput(spriteBin(), nil, cmdArgs...)
		return err
	}
	return runCmd(spriteBin(), nil, cmdArgs...)
}

func spriteExecOutput(spriteName string, env []string, args ...string) (string, error) {
	if spriteName == "" {
		return "", errors.New("sprite name is empty")
	}
	cmdArgs := []string{"exec", "-s", spriteName}
	for _, kv := range env {
		cmdArgs = append(cmdArgs, "-env", kv)
	}
	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, args...)
	return runCmdOutput(spriteBin(), nil, cmdArgs...)
}

func spriteBin() string {
	if spritePath == "" {
		return "sprite"
	}
	return spritePath
}

func installSpriteCLI() error {
	if _, err := exec.LookPath("curl"); err != nil {
		return errors.New("sprite CLI not found and curl is not available for auto-install")
	}
	cmd := exec.Command("sh", "-c", "curl -fsSL https://sprites.dev/install.sh | sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runCmd(name string, extraEnv []string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	return cmd.Run()
}

func runCmdWithInput(name string, extraEnv []string, stdin string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(stdin)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	return cmd.Run()
}

func runCmdDevNull(name string, extraEnv []string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	return cmd.Run()
}

func runCmdQuiet(name string, extraEnv []string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	return cmd.Run()
}

func runCmdOutput(name string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TUI types

type logMsg string

type doneMsg struct {
	res upResult
	err error
}

type upModel struct {
	spinner spinner.Model
	logs    []string
	res     upResult
	err     error
}

var (
	headerStyle  = lipgloss.NewStyle().Bold(true)
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	bulletStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	prefixStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	styleEnabled = true
)

func newUpModel() upModel {
	sp := spinner.New()
	sp.Spinner = spinner.Line
	return upModel{spinner: sp, logs: []string{}}
}

func (m upModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m upModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case logMsg:
		m.logs = append(m.logs, string(msg))
		return m, nil
	case doneMsg:
		m.res = msg.res
		m.err = msg.err
		return m, tea.Quit
	}
	return m, nil
}

func (m upModel) View() string {
	b := &strings.Builder{}
	title := fmt.Sprintf("%s seven up", m.spinner.View())
	if m.err != nil {
		title = fmt.Sprintf("! seven up")
	}
	fmt.Fprintf(b, "%s\n", headerStyle.Render(title))
	if len(m.logs) > 0 {
		start := 0
		if len(m.logs) > 6 {
			start = len(m.logs) - 6
		}
		for _, line := range m.logs[start:] {
			prefix, rest := splitLogPrefix(line)
			if prefix != "" {
				fmt.Fprintf(b, "%s %s %s\n", bulletStyle.Render("•"), prefixStyle.Render(prefix), subtleStyle.Render(rest))
				continue
			}
			fmt.Fprintf(b, "%s %s\n", bulletStyle.Render("•"), subtleStyle.Render(line))
		}
	}
	if m.err != nil {
		fmt.Fprintf(b, "\n%s %v\n", errorStyle.Render("error:"), m.err)
	}
	return b.String()
}

func splitLogPrefix(line string) (string, string) {
	if !strings.HasPrefix(line, "[") {
		return "", ""
	}
	idx := strings.Index(line, "]")
	if idx == -1 {
		return "", ""
	}
	prefix := line[:idx+1]
	rest := strings.TrimSpace(line[idx+1:])
	return prefix, rest
}

func formatStyledLog(line string) string {
	if !styleEnabled {
		return line
	}
	prefix, rest := splitLogPrefix(line)
	if prefix == "" {
		return line
	}
	return fmt.Sprintf("%s %s", prefixStyle.Render(prefix), subtleStyle.Render(rest))
}

func formatStyledBulletLog(line string) string {
	if !styleEnabled {
		return line
	}
	return fmt.Sprintf("%s %s", bulletStyle.Render("•"), formatStyledLog(line))
}
