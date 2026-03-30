package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testSevenBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "seven-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	bin := filepath.Join(tmp, "seven")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = filepath.Join(projectRoot(), "cmd", "seven")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	testSevenBin = bin
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

func TestSevenUpCreatesSpriteAndWritesFile(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	spriteFile := filepath.Join(repo, ".sprite")
	data, err := os.ReadFile(spriteFile)
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		t.Fatalf(".sprite should contain a name")
	}

	stateData, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("expected sprite state: %v", err)
	}
	if !strings.Contains(string(stateData), name) {
		t.Fatalf("sprite state missing name %q", name)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "create "+name) {
		t.Fatalf("expected create log, got: %s", log)
	}
	if !strings.Contains(log, "console -s "+name) {
		t.Fatalf("expected console log, got: %s", log)
	}
	if !strings.Contains(log, "gh repo clone") {
		t.Fatalf("expected clone exec log, got: %s", log)
	}
}

func TestSevenUpSkipsLoginWhenSpriteExists(t *testing.T) {
	repo := t.TempDir()
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	spriteName := "existing-sprite"
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "up", "--no-tui")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	if strings.Contains(string(logData), "login") {
		t.Fatalf("expected no login when sprite exists, got: %s", logData)
	}
}

func TestSevenUpSyncsGitIdentityWithExecSeparator(t *testing.T) {
	repo := t.TempDir()
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	spriteName := "existing-sprite"
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	fakeHome := t.TempDir()
	configPath := filepath.Join(fakeHome, ".gitconfig")
	config := "[user]\n\tname = Test User\n\temail = test@example.com\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("failed to write fake gitconfig: %v", err)
	}

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"GIT_CONFIG_NOSYSTEM=1",
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_EXEC_REQUIRE_SEPARATOR=1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "exec -s "+spriteName+" -- git config --global user.name Test User") {
		t.Fatalf("expected git identity sync to use exec separator, got: %s", log)
	}
	if !strings.Contains(log, "exec -s "+spriteName+" -- git config --global user.email test@example.com") {
		t.Fatalf("expected git email sync to use exec separator, got: %s", log)
	}
}

func TestSevenUpExistingSpriteRefreshesConsoleBootstrap(t *testing.T) {
	repo := t.TempDir()
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	spriteName := "existing-sprite"
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	fakeHome := t.TempDir()
	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"GIT_CONFIG_NOSYSTEM=1",
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, ".seven-console-hook.sh") || !strings.Contains(log, ".seven-console-once") {
		t.Fatalf("expected existing sprite up to refresh console bootstrap, got: %s", log)
	}
	if !strings.Contains(log, "-env SEVEN_REPO_DIR="+spriteName+",SEVEN_ASSISTANT=codex") {
		t.Fatalf("expected existing sprite up to refresh bootstrap with default assistant, got: %s", log)
	}
	if strings.Contains(log, "exec \"$assistant_cmd\"") {
		t.Fatalf("expected console bootstrap to stop exec-replacing the shell, got: %s", log)
	}
	if !strings.Contains(log, "Run %s when you want.") {
		t.Fatalf("expected console bootstrap to print an assistant hint instead of exec, got: %s", log)
	}
}

func TestSevenUpLogsInWhenSpriteListFails(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--no-tui")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_FAIL_LIST=once",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	if !strings.Contains(string(logData), "login") {
		t.Fatalf("expected login when list fails, got: %s", logData)
	}
}

func TestSevenUpAutoUpgradesSpriteCLIWhenOutdated(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-console", "--no-tui")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_UPGRADE_CHECK_LATEST=v0.0.2",
		"SPRITE_UPGRADE_CHECK_CURRENT=v0.0.1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "upgrade --check") {
		t.Fatalf("expected upgrade check log, got: %s", log)
	}
	if strings.Count(log, "upgrade ") < 2 {
		t.Fatalf("expected upgrade execution log after check, got: %s", log)
	}
}

func TestSevenUpAutoUpgradesSpriteCLIWithNonInteractiveConfirmation(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-console", "--no-tui")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader("")
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_UPGRADE_CHECK_LATEST=v0.0.2",
		"SPRITE_UPGRADE_CHECK_CURRENT=v0.0.1",
		"SPRITE_UPGRADE_CONFIRM_REQUIRED=1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	if strings.Contains(string(logData), "upgrade (confirm failed)") {
		t.Fatalf("expected upgrade confirmation to be provided, got: %s", logData)
	}
}

func TestSevenUpSkipsSpriteUpgradeCheckWhenEnvSet(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-console", "--no-tui")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_UPGRADE_CHECK_LATEST=v0.0.2",
		"SPRITE_UPGRADE_CHECK_CURRENT=v0.0.1",
		"SEVEN_SKIP_SPRITE_UPGRADE=1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if strings.Contains(log, "upgrade --check") {
		t.Fatalf("expected sprite upgrade check to be skipped, got: %s", log)
	}
	if !bytes.Contains(output, []byte("[seven up] skipping sprite CLI update check")) {
		t.Fatalf("expected skip log in output, got: %s", output)
	}
}

func TestSevenInitSetsUpSpriteWithoutConsole(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	fakeHome := t.TempDir()
	cmd := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven init failed: %v\n%s", err, output)
	}

	spriteFile := filepath.Join(repo, ".sprite")
	data, err := os.ReadFile(spriteFile)
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		t.Fatalf(".sprite should contain a name")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "create "+name) {
		t.Fatalf("expected create log, got: %s", log)
	}
	if !strings.Contains(log, "gh repo clone") {
		t.Fatalf("expected clone exec log, got: %s", log)
	}
	if !strings.Contains(log, ".seven-console-hook.sh") {
		t.Fatalf("expected console hook setup in sprite, got: %s", log)
	}
	if !strings.Contains(log, ".seven-console-once") {
		t.Fatalf("expected one-shot marker setup in sprite, got: %s", log)
	}
	if !strings.Contains(log, "-env SEVEN_REPO_DIR="+name+",SEVEN_ASSISTANT=codex") {
		t.Fatalf("expected env vars to be comma-joined for sprite exec, got: %s", log)
	}
	if strings.Contains(log, "exec \"$assistant_cmd\"") {
		t.Fatalf("expected console bootstrap to keep the shell usable, got: %s", log)
	}
	if !strings.Contains(log, "Run %s when you want.") {
		t.Fatalf("expected console bootstrap to print an assistant hint, got: %s", log)
	}
	if !strings.Contains(log, ".bashrc") || !strings.Contains(log, ".zshrc") {
		t.Fatalf("expected shell rc setup for bash and zsh, got: %s", log)
	}
	if strings.Contains(log, "console -s "+name) {
		t.Fatalf("did not expect console log, got: %s", log)
	}
}

func TestSevenStatus(t *testing.T) {
	repo := t.TempDir()
	state, _, cleanup := createFakeSprite(t)
	defer cleanup()

	spriteName := "status-sprite"
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "status")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven status failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("status: exists")) {
		t.Fatalf("expected status exists, got: %s", output)
	}
}

func TestSevenDestroy(t *testing.T) {
	repo := t.TempDir()
	state, _, cleanup := createFakeSprite(t)
	defer cleanup()

	spriteName := "destroy-sprite"
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "destroy")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven destroy failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(filepath.Join(repo, ".sprite")); !os.IsNotExist(err) {
		t.Fatalf("expected .sprite to be removed")
	}

	stateData, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	if strings.Contains(string(stateData), spriteName) {
		t.Fatalf("expected sprite to be removed from state")
	}
}

func TestSevenDestroyKeepsSpriteFileOnDestroyFailure(t *testing.T) {
	repo := t.TempDir()
	state, _, cleanup := createFakeSprite(t)
	defer cleanup()

	spriteName := "destroy-fails"
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "destroy")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_FAIL_DESTROY=1",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected seven destroy to fail\n%s", output)
	}

	data, readErr := os.ReadFile(filepath.Join(repo, ".sprite"))
	if readErr != nil {
		t.Fatalf("expected .sprite to remain after destroy failure: %v", readErr)
	}
	if strings.TrimSpace(string(data)) != spriteName {
		t.Fatalf("expected .sprite to keep sprite name %q, got %q", spriteName, strings.TrimSpace(string(data)))
	}
}

func TestSevenInitConfiguresGhAuthInSprite(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()
	ghBin := t.TempDir()
	token := "gh-test-token"
	ghScript := `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "token" ]; then
  echo "` + token + `"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(ghBin, "gh"), []byte(ghScript), 0o755); err != nil {
		t.Fatalf("failed to write fake gh: %v", err)
	}

	cmd := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+ghBin+string(os.PathListSeparator)+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven init failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "gh auth login --with-token -h github.com") {
		t.Fatalf("expected gh auth login in sprite, got: %s", log)
	}
	if !strings.Contains(log, "gh auth setup-git") {
		t.Fatalf("expected gh auth setup-git in sprite, got: %s", log)
	}
	if !strings.Contains(log, "GH_TOKEN="+token) {
		t.Fatalf("expected GH_TOKEN to be passed into sprite, got: %s", log)
	}
}

func TestSevenInitSyncsCodexChatGPTAuthInSprite(t *testing.T) {
	repo := t.TempDir()
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	fakeHome := t.TempDir()
	codexDir := filepath.Join(fakeHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("failed to create fake codex dir: %v", err)
	}
	authPath := filepath.Join(codexDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"provider":"chatgpt"}`), 0o600); err != nil {
		t.Fatalf("failed to write fake codex auth file: %v", err)
	}

	codexBin := t.TempDir()
	codexScript := `#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  echo "Logged in using ChatGPT"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(codexBin, "codex"), []byte(codexScript), 0o755); err != nil {
		t.Fatalf("failed to write fake codex: %v", err)
	}

	cmd := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+codexBin+string(os.PathListSeparator)+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+fakeHome,
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven init failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	wantFileSpec := "-file " + authPath + ":/tmp/host-codex-auth.json"
	if !strings.Contains(log, wantFileSpec) {
		t.Fatalf("expected codex auth file upload in sprite exec, got: %s", log)
	}
}

func TestSevenInitSyncsCodexConfigInSprite(t *testing.T) {
	repo := t.TempDir()
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	fakeHome := t.TempDir()
	codexDir := filepath.Join(fakeHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("failed to create fake codex dir: %v", err)
	}
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("approval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n"), 0o600); err != nil {
		t.Fatalf("failed to write fake codex config file: %v", err)
	}

	cmd := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+fakeHome,
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven init failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	wantFileSpec := "-file " + configPath + ":/tmp/host-codex-config.toml"
	if !strings.Contains(log, wantFileSpec) {
		t.Fatalf("expected codex config file upload in sprite exec, got: %s", log)
	}
}

func TestSevenInitSyncsClaudeAuthAndConfigInSprite(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	fakeHome := t.TempDir()
	claudeDir := filepath.Join(fakeHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("failed to create fake claude dir: %v", err)
	}
	authPath := filepath.Join(fakeHome, ".claude.json")
	if err := os.WriteFile(authPath, []byte(`{"oauthAccount":{"emailAddress":"test@example.com"}}`), 0o600); err != nil {
		t.Fatalf("failed to write fake claude auth file: %v", err)
	}
	configPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(configPath, []byte(`{"theme":"dark-ansi"}`), 0o600); err != nil {
		t.Fatalf("failed to write fake claude config file: %v", err)
	}

	claudeBin := t.TempDir()
	claudeScript := `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ] && [ "$3" = "--json" ]; then
  echo '{"loggedIn":true,"authMethod":"oauth","apiProvider":"firstParty"}'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(claudeBin, "claude"), []byte(claudeScript), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}

	cmd := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"PATH="+claudeBin+string(os.PathListSeparator)+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven init failed: %v\n%s", err, output)
	}

	spriteData, err := os.ReadFile(filepath.Join(repo, ".sprite"))
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	spriteName := strings.TrimSpace(string(spriteData))

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	wantAuthFileSpec := "-file " + authPath + ":/tmp/host-claude-auth.json"
	if !strings.Contains(log, wantAuthFileSpec) {
		t.Fatalf("expected claude auth file upload in sprite exec, got: %s", log)
	}
	wantConfigFileSpec := "-file " + configPath + ":/tmp/host-claude-settings.json"
	if !strings.Contains(log, wantConfigFileSpec) {
		t.Fatalf("expected claude config file upload in sprite exec, got: %s", log)
	}
	if !strings.Contains(log, "-env SEVEN_REPO_DIR="+spriteName+",SEVEN_ASSISTANT=claude") {
		t.Fatalf("expected claude to be selected for console hint, got: %s", log)
	}
}

func TestNormalizeSpriteName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "trems.al", want: "trems-al"},
		{in: "My_Repo", want: "my-repo"},
		{in: "--bad--", want: "bad"},
		{in: "a..b", want: "a-b"},
		{in: "UPPER", want: "upper"},
	}

	for _, tc := range cases {
		if got := normalizeSpriteName(tc.in); got != tc.want {
			t.Fatalf("normalizeSpriteName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSpriteListedInOutput(t *testing.T) {
	out := "NAME STATUS\nfoo-bar-baz running\n"
	if spriteListedInOutput(out, "foo-bar") {
		t.Fatalf("did not expect partial sprite name match")
	}
	if !spriteListedInOutput(out, "foo-bar-baz") {
		t.Fatalf("expected exact sprite name match")
	}
}

func TestGithubRepoSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "git@github.com:octo/hello.git", want: "octo/hello"},
		{in: "https://github.com/octo/hello.git", want: "octo/hello"},
		{in: "ssh://git@github.com/octo/hello.git", want: "octo/hello"},
		{in: "ssh://git@github.com/octo/hello/extra.git", want: ""},
		{in: "https://gitlab.com/octo/hello.git", want: ""},
	}

	for _, tc := range cases {
		if got := githubRepoSlug(tc.in); got != tc.want {
			t.Fatalf("githubRepoSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseSpriteUpgradeCheckOutput(t *testing.T) {
	out := "Checking for updates...\nLatest version: v0.0.2\nCurrent version: v0.0.1\n"
	latest, current, ok := parseSpriteUpgradeCheckOutput(out)
	if !ok {
		t.Fatalf("expected parseSpriteUpgradeCheckOutput to succeed")
	}
	if latest != "v0.0.2" || current != "v0.0.1" {
		t.Fatalf("unexpected parse result latest=%q current=%q", latest, current)
	}
}

func TestSevenUpNewCreatesNextSiblingAndUpdatesSelection(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte("seven-01\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte("seven\nseven-01\nseven-02\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console", "--new")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up --new failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(filepath.Join(repo, ".sprite"))
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "seven-03" {
		t.Fatalf("expected .sprite to select seven-03, got %q", got)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "create seven-03") {
		t.Fatalf("expected create log for seven-03, got: %s", log)
	}
}

func TestSevenUpSpriteUsesExplicitNameAndSelectsIt(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console", "--sprite", "review-sprite")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up --sprite failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(filepath.Join(repo, ".sprite"))
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "review-sprite" {
		t.Fatalf("expected .sprite to select explicit sprite, got %q", got)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	if !strings.Contains(string(logData), "create review-sprite") {
		t.Fatalf("expected explicit create log, got: %s", logData)
	}
}

func TestSevenDestroySpriteKeepsDifferentSelection(t *testing.T) {
	repo := t.TempDir()
	state, _, cleanup := createFakeSprite(t)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte("current-sprite\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte("current-sprite\nother-sprite\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "destroy", "--sprite", "other-sprite")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven destroy --sprite failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(filepath.Join(repo, ".sprite"))
	if err != nil {
		t.Fatalf("expected .sprite to remain: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "current-sprite" {
		t.Fatalf("expected .sprite to keep current selection, got %q", got)
	}

	stateData, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	if strings.Contains(string(stateData), "other-sprite") {
		t.Fatalf("expected other-sprite to be removed from state")
	}
	if !strings.Contains(string(stateData), "current-sprite") {
		t.Fatalf("expected current-sprite to remain in state")
	}
}

func TestSevenDestroyRequiresSelectedSpriteWithoutFlag(t *testing.T) {
	repo := t.TempDir()
	state, _, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "destroy")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected seven destroy to fail without a selected sprite\n%s", output)
	}
	if !bytes.Contains(output, []byte("no selected sprite")) {
		t.Fatalf("expected missing selection error, got: %s", output)
	}
}

func TestParseSpriteUpgradeCheckOutputSupportsCurrentSpriteFormat(t *testing.T) {
	out := "Checking for updates...\nMigrating configuration from version 1 to 1...\n\x1b[32mLatest client version:\x1b[0m v0.0.2\nCurrent client version: v0.0.1\n"
	latest, current, ok := parseSpriteUpgradeCheckOutput(out)
	if !ok {
		t.Fatalf("expected parseSpriteUpgradeCheckOutput to succeed for current sprite format")
	}
	if latest != "v0.0.2" || current != "v0.0.1" {
		t.Fatalf("unexpected parse result latest=%q current=%q", latest, current)
	}
}

func TestParseSpriteUpgradeCheckOutputInvalid(t *testing.T) {
	if _, _, ok := parseSpriteUpgradeCheckOutput("no version lines"); ok {
		t.Fatalf("expected parseSpriteUpgradeCheckOutput to fail")
	}
}

func TestResolveSpriteNameNormalizesDirName(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "trems.al")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("failed to make repo dir: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	info, err := resolveSpriteName()
	if err != nil {
		t.Fatalf("resolveSpriteName failed: %v", err)
	}
	if info.Name != "trems-al" {
		t.Fatalf("expected normalized name trems-al, got %q", info.Name)
	}
	if !info.Normalized {
		t.Fatalf("expected normalization to be true")
	}
	if info.FromFile {
		t.Fatalf("expected fromFile=false")
	}
}

func TestResolveSpriteNameRejectsInvalidFromFile(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte("bad.name\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	if _, err := resolveSpriteName(); err == nil {
		t.Fatalf("expected resolveSpriteName to fail for invalid .sprite")
	}
}

func createTempRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()

	init := exec.Command("git", "init")
	init.Dir = repo
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	remote := exec.Command("git", "remote", "add", "origin", "https://github.com/octo/hello.git")
	remote.Dir = repo
	if out, err := remote.CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v\n%s", err, out)
	}
	return repo
}

func createFakeSprite(t *testing.T) (statePath, logPath string, cleanup func()) {
	t.Helper()
	binDir := t.TempDir()
	statePath = filepath.Join(binDir, "sprite_state")
	logPath = filepath.Join(binDir, "sprite_log")

	scriptPath := filepath.Join(binDir, "sprite")
	script := `#!/bin/sh
set -e
state="${SPRITE_STATE:-` + statePath + `}"
log="${SPRITE_LOG:-` + logPath + `}"
cmd="$1"
shift || true
logit() {
  if [ -n "$log" ]; then
    echo "$*" >> "$log"
  fi
}
case "$cmd" in
  login)
    logit "login"
    exit 0
    ;;
  upgrade)
    if [ "$1" = "--check" ]; then
      logit "upgrade --check"
      if [ "${SPRITE_UPGRADE_CHECK_FAIL:-}" = "1" ]; then
        exit 1
      fi
      if [ -n "${SPRITE_UPGRADE_CHECK_OUTPUT:-}" ]; then
        printf '%s\n' "$SPRITE_UPGRADE_CHECK_OUTPUT"
      else
        latest="${SPRITE_UPGRADE_CHECK_LATEST:-v0.0.1}"
        current="${SPRITE_UPGRADE_CHECK_CURRENT:-$latest}"
        printf 'Latest version: %s\nCurrent version: %s\n' "$latest" "$current"
      fi
      exit 0
    fi
    if [ "${SPRITE_UPGRADE_CONFIRM_REQUIRED:-}" = "1" ]; then
      answer=""
      IFS= read -r answer || answer=""
      case "$answer" in
        y|Y|yes|YES|Yes)
          ;;
        *)
          logit "upgrade (confirm failed)"
          exit 1
          ;;
      esac
    fi
    logit "upgrade $*"
    if [ "${SPRITE_UPGRADE_FAIL:-}" = "1" ]; then
      exit 1
    fi
    exit 0
    ;;
  list)
    if [ "${SPRITE_FAIL_LIST:-}" = "1" ]; then
      logit "list (fail)"
      exit 1
    fi
    if [ "${SPRITE_FAIL_LIST:-}" = "once" ]; then
      failflag="${state}.fail_once"
      if [ ! -f "$failflag" ]; then
        logit "list (fail)"
        echo 1 > "$failflag"
        exit 1
      fi
    fi
    logit "list"
    if [ -f "$state" ]; then
      cat "$state"
    fi
    exit 0
    ;;
  create)
    if [ "$1" = "--skip-console" ]; then
      shift
    fi
    name="$1"
    logit "create $name"
    if [ -n "$name" ]; then
      if [ -f "$state" ]; then
        if ! grep -q "^$name$" "$state"; then
          echo "$name" >> "$state"
        fi
      else
        echo "$name" > "$state"
      fi
    fi
    exit 0
    ;;
  destroy)
    if [ "$1" = "--force" ]; then
      shift
    fi
    name="$1"
    if [ "${SPRITE_FAIL_DESTROY:-}" = "1" ]; then
      logit "destroy $name (fail)"
      exit 1
    fi
    logit "destroy $name"
    if [ -f "$state" ]; then
      grep -v "^$name$" "$state" > "$state.tmp" || true
      mv "$state.tmp" "$state"
    fi
    exit 0
    ;;
  console)
    logit "console $*"
    exit 0
    ;;
  exec)
    exec_args="$*"
    if [ "${SPRITE_EXEC_REQUIRE_SEPARATOR:-}" = "1" ]; then
      found_separator=0
      while [ "$#" -gt 0 ]; do
        case "$1" in
          --)
            found_separator=1
            break
            ;;
          -s|-env|-file)
            shift
            shift || true
            ;;
          *)
            break
            ;;
        esac
      done
      if [ "$found_separator" != "1" ]; then
        logit "exec (missing separator) $exec_args"
        exit 2
      fi
    fi
    logit "exec $exec_args"
    exit 0
    ;;
  upgrade)
    if [ "$1" = "--check" ]; then
      logit "upgrade --check"
      latest="${SPRITE_UPGRADE_CHECK_LATEST:-v0.0.1}"
      current="${SPRITE_UPGRADE_CHECK_CURRENT:-$latest}"
      echo "Latest version: $latest"
      echo "Current version: $current"
      exit 0
    fi
    if [ "${SPRITE_UPGRADE_CONFIRM_REQUIRED:-}" = "1" ]; then
      answer=""
      IFS= read -r answer || answer=""
      case "$answer" in
        y|Y|yes|YES|Yes)
          ;;
        *)
          logit "upgrade (confirm failed)"
          exit 1
          ;;
      esac
    fi
    logit "upgrade $*"
    exit 0
    ;;
  *)
    logit "unknown $cmd $*"
    exit 0
    ;;
esac
`

	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake sprite: %v", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(binDir)
	}
	return statePath, logPath, cleanup
}

func projectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			return cwd
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "."
		}
		cwd = parent
	}
}
