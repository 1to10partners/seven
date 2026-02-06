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

func TestSevenInitSetsUpSpriteWithoutConsole(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
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
	if !strings.Contains(log, "gh auth login --with-token") {
		t.Fatalf("expected gh auth login in sprite, got: %s", log)
	}
	if !strings.Contains(log, "gh auth setup-git") {
		t.Fatalf("expected gh auth setup-git in sprite, got: %s", log)
	}
	if !strings.Contains(log, "GH_TOKEN="+token) {
		t.Fatalf("expected GH_TOKEN to be passed into sprite, got: %s", log)
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
    logit "exec $*"
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
