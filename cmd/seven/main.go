package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
}

type spriteNameInfo struct {
	Name       string
	FromFile   bool
	Original   string
	Normalized bool
}

var spritePath string
var spriteNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

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
	fmt.Println("  seven init [--assume-logged-in]")
	fmt.Println("  seven up [--assume-logged-in] [--no-console] [--no-tui]")
	fmt.Println("  seven destroy")
	fmt.Println("  seven status")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  version  Show version")
	fmt.Println("  init     One-time setup (login, create sprite, clone repo)")
	fmt.Println("  up       Create or reuse a sprite, bootstrap repo, open console")
	fmt.Println("  destroy  Destroy the current sprite and remove .sprite file")
	fmt.Println("  status   Show sprite status for this repo")
}

var version = "dev"

func printVersion() {
	fmt.Println(version)
}

func cmdUp(args []string) {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	noTUI := fs.Bool("no-tui", false, "disable TUI output")
	assumeLoggedIn := fs.Bool("assume-logged-in", false, "skip sprite login")
	noConsole := fs.Bool("no-console", false, "do not open sprite console after up")
	_ = fs.Parse(args)

	shouldUseTUI := !*noTUI
	styleEnabled = shouldUseTUI
	if shouldUseTUI {
		res, err := runUpWithTUI(*assumeLoggedIn, !*noConsole)
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

	res, err := runUp(upOptions{
		Logger:         func(msg string) { fmt.Println(msg) },
		QuietExternal:  false,
		AssumeLoggedIn: *assumeLoggedIn,
		OpenConsole:    !*noConsole,
	})
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
	_ = fs.Parse(args)

	_, err := runInit(upOptions{
		Logger:         func(msg string) { fmt.Println(msg) },
		QuietExternal:  false,
		AssumeLoggedIn: *assumeLoggedIn,
		OpenConsole:    false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seven init failed: %v\n", err)
		os.Exit(1)
	}
}

func cmdDestroy(args []string) {
	fs := flag.NewFlagSet("destroy", flag.ExitOnError)
	_ = fs.Parse(args)

	info, err := resolveSpriteName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve sprite name: %v\n", err)
		os.Exit(1)
	}
	name := info.Name

	if err := ensureSpriteCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if _, err := spriteList(); err != nil {
		fmt.Fprintf(os.Stderr, "sprite list failed: %v\n", err)
		os.Exit(1)
	}

	if err := removeSpriteFile(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to remove .sprite: %v\n", err)
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
		fmt.Printf("destroyed sprite: %s\n", name)
		return
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

func runUp(opts upOptions) (upResult, error) {
	if opts.Logger == nil {
		opts.Logger = func(string) {}
	}

	if err := ensureSpriteCLI(); err != nil {
		return upResult{}, err
	}

	info, err := resolveSpriteName()
	if err != nil {
		return upResult{}, err
	}
	if info.Normalized && !info.FromFile {
		opts.Logger(fmt.Sprintf("[seven up] normalized sprite name from %q to %q (set .sprite to override)", info.Original, info.Name))
	}
	name := info.Name
	fromFile := info.FromFile

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
		if !fromFile {
			if err := writeSpriteFile(name); err != nil {
				return upResult{}, err
			}
		}
		return upResult{Name: name, OpenConsole: opts.OpenConsole, SpriteExists: true}, nil
	}

	res, err := runInit(opts)
	if err != nil {
		return upResult{}, err
	}
	res.OpenConsole = opts.OpenConsole
	return res, nil
}

func runInit(opts upOptions) (upResult, error) {
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
	if info.Normalized && !info.FromFile {
		opts.Logger(fmt.Sprintf("[seven init] normalized sprite name from %q to %q (set .sprite to override)", info.Original, info.Name))
	}
	name := info.Name
	fromFile := info.FromFile

	opts.Logger(fmt.Sprintf("[seven init] using sprite name: %s", name))

	exists, err := spriteExists(name)
	if err != nil {
		return upResult{}, err
	}
	if exists {
		opts.Logger("[seven init] sprite exists")
		if !fromFile {
			if err := writeSpriteFile(name); err != nil {
				return upResult{}, err
			}
		}
		if err := syncGitIdentity(name, opts); err != nil {
			return upResult{}, err
		}
		return upResult{Name: name, OpenConsole: false, SpriteExists: true}, nil
	}

	opts.Logger("[seven init] creating sprite")
	if opts.QuietExternal {
		if err := runCmdQuiet(spriteBin(), nil, "create", "--skip-console", name); err != nil {
			return upResult{}, err
		}
	} else if err := runCmdDevNull(spriteBin(), nil, "create", "--skip-console", name); err != nil {
		return upResult{}, err
	}

	opts.Logger("[seven init] writing .sprite")
	if err := writeSpriteFile(name); err != nil {
		return upResult{}, err
	}

	if err := syncGitIdentity(name, opts); err != nil {
		return upResult{}, err
	}

	repoURL, repoSlug, ghToken, err := detectRepoInfo(name, opts)
	if err != nil {
		return upResult{}, err
	}
	if err := ensureGhAuthInSprite(name, ghToken, opts); err != nil {
		opts.Logger(fmt.Sprintf("[seven init] gh auth setup failed: %v", err))
	}

	if repoURL == "" {
		opts.Logger("[seven init] no repo url found, skipping clone")
		return upResult{Name: name, OpenConsole: false, SpriteExists: false}, nil
	}

	if repoSlug != "" {
		if ghToken != "" {
			opts.Logger(fmt.Sprintf("[seven init] cloning via gh repo clone: %s", repoSlug))
			if err := spriteExec(name, []string{"GH_TOKEN=" + ghToken}, opts.QuietExternal, "gh", "repo", "clone", repoSlug, name); err != nil {
				return upResult{}, err
			}
		} else {
			opts.Logger(fmt.Sprintf("[seven init] cloning via gh repo clone (no token): %s", repoSlug))
			if err := spriteExec(name, nil, opts.QuietExternal, "gh", "repo", "clone", repoSlug, name); err != nil {
				return upResult{}, err
			}
		}
		return upResult{Name: name, OpenConsole: false, SpriteExists: false}, nil
	}

	opts.Logger(fmt.Sprintf("[seven init] cloning via git clone: %s", repoURL))
	if err := spriteExec(name, nil, opts.QuietExternal, "git", "clone", repoURL, name); err != nil {
		return upResult{}, err
	}

	return upResult{Name: name, OpenConsole: false, SpriteExists: false}, nil
}

func runUpWithTUI(assumeLoggedIn bool, openConsole bool) (upResult, error) {
	if !assumeLoggedIn {
		if err := ensureSpriteCLI(); err != nil {
			return upResult{}, err
		}
		if _, err := spriteList(); err != nil {
			fmt.Println(formatStyledBulletLog("[seven init] logging in to sprite"))
			if err := runCmd(spriteBin(), nil, "login"); err != nil {
				return upResult{}, err
			}
		}
		assumeLoggedIn = true
	}

	m := newUpModel()
	p := tea.NewProgram(m)

	go func() {
		res, err := runUp(upOptions{
			Logger: func(msg string) { p.Send(logMsg(msg)) },
			// TUI mode captures output for cleaner display.
			QuietExternal:  true,
			AssumeLoggedIn: assumeLoggedIn,
			OpenConsole:    openConsole,
		})
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
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	return re.MatchString(out), nil
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
	slug := repoURL
	if strings.HasPrefix(slug, "git@github.com:") {
		slug = strings.TrimPrefix(slug, "git@github.com:")
	} else if strings.HasPrefix(slug, "https://github.com/") {
		slug = strings.TrimPrefix(slug, "https://github.com/")
	} else if strings.HasPrefix(slug, "http://github.com/") {
		slug = strings.TrimPrefix(slug, "http://github.com/")
	}
	if strings.HasSuffix(slug, ".git") {
		slug = strings.TrimSuffix(slug, ".git")
	}
	if strings.Contains(slug, "/") {
		return slug
	}
	return ""
}

func spriteExec(spriteName string, env []string, quiet bool, args ...string) error {
	if spriteName == "" {
		return errors.New("sprite name is empty")
	}
	cmdArgs := []string{"exec", "-s", spriteName}
	for _, kv := range env {
		cmdArgs = append(cmdArgs, "-env", kv)
	}
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
