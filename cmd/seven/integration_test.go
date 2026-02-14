package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIntegrationUpDestroy(t *testing.T) {
	if os.Getenv("SEVEN_INTEGRATION") != "1" {
		t.Skip("set SEVEN_INTEGRATION=1 to run integration tests")
	}

	if _, err := exec.LookPath("sprite"); err != nil {
		t.Skip("sprite CLI not found in PATH")
	}
	if err := exec.Command("sprite", "list").Run(); err != nil {
		t.Skip("sprite list failed; ensure you are logged in")
	}

	repo := t.TempDir()
	name := uniqueSpriteName()
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}

	cmdInit := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmdInit.Dir = repo
	cmdInit.Stdout = os.Stdout
	cmdInit.Stderr = os.Stderr
	cmdInit.Env = os.Environ()
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("seven init failed: %v", err)
	}

	cmdUp := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-console", "--no-tui")
	cmdUp.Dir = repo
	cmdUp.Stdout = os.Stdout
	cmdUp.Stderr = os.Stderr
	cmdUp.Env = os.Environ()
	if err := cmdUp.Run(); err != nil {
		t.Fatalf("seven up failed: %v", err)
	}

	spriteFile := filepath.Join(repo, ".sprite")
	data, err := os.ReadFile(spriteFile)
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	name = strings.TrimSpace(string(data))
	if name == "" {
		t.Fatalf(".sprite should contain a name")
	}

	if !spriteListed(name) {
		defer destroySprite(t, repo)
		t.Fatalf("sprite not found in list: %s", name)
	}

	destroySprite(t, repo)

	if spriteListed(name) {
		t.Fatalf("sprite still present after destroy: %s", name)
	}
}

func TestIntegrationInitWithGitRemote(t *testing.T) {
	if os.Getenv("SEVEN_INTEGRATION") != "1" {
		t.Skip("set SEVEN_INTEGRATION=1 to run integration tests")
	}

	if _, err := exec.LookPath("sprite"); err != nil {
		t.Skip("sprite CLI not found in PATH")
	}
	if err := exec.Command("sprite", "list").Run(); err != nil {
		t.Skip("sprite list failed; ensure you are logged in")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	if err := initGitRepo(repo, "https://github.com/1to10partners/seven.git"); err != nil {
		t.Fatalf("git setup failed: %v", err)
	}

	name := uniqueSpriteName()
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}

	cmdInit := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmdInit.Dir = repo
	cmdInit.Stdout = os.Stdout
	cmdInit.Stderr = os.Stderr
	cmdInit.Env = os.Environ()
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("seven init failed: %v", err)
	}

	// Ensure the repo was cloned inside the sprite.
	check := exec.Command("sprite", "exec", "-s", name, "test", "-d", name)
	check.Stdout = os.Stdout
	check.Stderr = os.Stderr
	if err := check.Run(); err != nil {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected repo directory in sprite: %v", err)
	}

	hookCheck := exec.Command("sprite", "exec", "-s", name, "sh", "-lc", "test -f \"$HOME/.seven-console-hook.sh\"")
	hookCheck.Stdout = os.Stdout
	hookCheck.Stderr = os.Stderr
	if err := hookCheck.Run(); err != nil {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected console hook file in sprite: %v", err)
	}

	bashRcCheck := exec.Command("sprite", "exec", "-s", name, "sh", "-lc", "grep -Fq '[ -f \"$HOME/.seven-console-hook.sh\" ] && . \"$HOME/.seven-console-hook.sh\"' \"$HOME/.bashrc\"")
	bashRcCheck.Stdout = os.Stdout
	bashRcCheck.Stderr = os.Stderr
	if err := bashRcCheck.Run(); err != nil {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected console hook source line in .bashrc: %v", err)
	}

	zshRcCheck := exec.Command("sprite", "exec", "-s", name, "sh", "-lc", "grep -Fq '[ -f \"$HOME/.seven-console-hook.sh\" ] && . \"$HOME/.seven-console-hook.sh\"' \"$HOME/.zshrc\"")
	zshRcCheck.Stdout = os.Stdout
	zshRcCheck.Stderr = os.Stderr
	if err := zshRcCheck.Run(); err != nil {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected console hook source line in .zshrc: %v", err)
	}

	markerOut, err := exec.Command("sprite", "exec", "-s", name, "sh", "-lc", "cat \"$HOME/.seven-console-once\"").CombinedOutput()
	if err != nil {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected one-shot marker file in sprite: %v\n%s", err, markerOut)
	}
	markerLines := strings.Split(strings.TrimSpace(string(markerOut)), "\n")
	if len(markerLines) < 2 {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected marker to have repo path and assistant, got: %q", markerOut)
	}
	if !strings.HasSuffix(markerLines[0], "/"+name) {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected marker repo path to end with /%s, got: %q", name, markerLines[0])
	}
	if markerLines[1] != "codex" {
		_ = exec.Command("sprite", "destroy", "--force", name).Run()
		t.Fatalf("expected marker assistant to be codex, got: %q", markerLines[1])
	}

	destroySprite(t, repo)
}

func TestIntegrationConsoleBootstrapRunsCodexInRepo(t *testing.T) {
	if os.Getenv("SEVEN_INTEGRATION") != "1" {
		t.Skip("set SEVEN_INTEGRATION=1 to run integration tests")
	}

	if _, err := exec.LookPath("sprite"); err != nil {
		t.Skip("sprite CLI not found in PATH")
	}
	if err := exec.Command("sprite", "list").Run(); err != nil {
		t.Skip("sprite list failed; ensure you are logged in")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	if err := initGitRepo(repo, "https://github.com/1to10partners/seven.git"); err != nil {
		t.Fatalf("git setup failed: %v", err)
	}

	name := uniqueSpriteName()
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}

	cmdInit := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmdInit.Dir = repo
	cmdInit.Stdout = os.Stdout
	cmdInit.Stderr = os.Stderr
	cmdInit.Env = os.Environ()
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("seven init failed: %v", err)
	}

	defer destroySprite(t, repo)

	setupStub := exec.Command("sprite", "exec", "-s", name, "sh", "-lc", `set -e
install -d "$HOME/.seven-test-bin"
cat > "$HOME/.seven-test-bin/codex" <<'EOF'
#!/bin/sh
pwd > "$HOME/.seven-codex-cwd"
exit 0
EOF
chmod +x "$HOME/.seven-test-bin/codex"
rm -f "$HOME/.seven-codex-cwd"
for rc in "$HOME/.bash_profile" "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.zprofile"; do
  tmp="$rc.tmp.$$"
  {
    printf '%s\n' 'export PATH="$HOME/.seven-test-bin:$PATH"'
    cat "$rc"
  } > "$tmp"
  mv "$tmp" "$rc"
done
`)
	setupStub.Stdout = os.Stdout
	setupStub.Stderr = os.Stderr
	if err := setupStub.Run(); err != nil {
		t.Fatalf("failed to set up codex stub in sprite: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	console := exec.CommandContext(ctx, "sprite", "console", "-s", name)
	console.Stdout = os.Stdout
	console.Stderr = os.Stderr
	console.Env = append(os.Environ(), "SHELL=/bin/bash")
	if err := console.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("sprite console timed out waiting for bootstrap codex to execute")
		}
		t.Fatalf("sprite console failed: %v", err)
	}

	cwdOut, err := exec.Command("sprite", "exec", "-s", name, "sh", "-lc", "cat \"$HOME/.seven-codex-cwd\"").CombinedOutput()
	if err != nil {
		t.Fatalf("expected codex stub to write cwd file: %v\n%s", err, cwdOut)
	}
	cwd := strings.TrimSpace(string(cwdOut))
	if !strings.HasSuffix(cwd, "/"+name) {
		t.Fatalf("expected codex to run from repo dir /%s, got: %q", name, cwd)
	}

	markerCheck := exec.Command("sprite", "exec", "-s", name, "sh", "-lc", "test ! -f \"$HOME/.seven-console-once\"")
	markerCheck.Stdout = os.Stdout
	markerCheck.Stderr = os.Stderr
	if err := markerCheck.Run(); err != nil {
		t.Fatalf("expected one-shot marker to be consumed after first console: %v", err)
	}
}

func TestIntegrationInitNormalizesForbiddenDirName(t *testing.T) {
	if os.Getenv("SEVEN_INTEGRATION") != "1" {
		t.Skip("set SEVEN_INTEGRATION=1 to run integration tests")
	}

	if _, err := exec.LookPath("sprite"); err != nil {
		t.Skip("sprite CLI not found in PATH")
	}
	if err := exec.Command("sprite", "list").Run(); err != nil {
		t.Skip("sprite list failed; ensure you are logged in")
	}

	parent := t.TempDir()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	dirName := "seven.it-" + suffix
	expectedSprite := "seven-it-" + suffix
	repo := filepath.Join(parent, dirName)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	cmdInit := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmdInit.Dir = repo
	cmdInit.Stdout = os.Stdout
	cmdInit.Stderr = os.Stderr
	cmdInit.Env = os.Environ()
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("seven init failed: %v", err)
	}

	spriteFile := filepath.Join(repo, ".sprite")
	data, err := os.ReadFile(spriteFile)
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	name := strings.TrimSpace(string(data))
	if name != expectedSprite {
		t.Fatalf("expected normalized sprite name %q, got %q", expectedSprite, name)
	}

	defer destroySprite(t, repo)

	if !spriteListed(name) {
		t.Fatalf("sprite not found in list: %s", name)
	}
}

func TestIntegrationGhAuthPersistsInSprite(t *testing.T) {
	if os.Getenv("SEVEN_INTEGRATION") != "1" {
		t.Skip("set SEVEN_INTEGRATION=1 to run integration tests")
	}

	if _, err := exec.LookPath("sprite"); err != nil {
		t.Skip("sprite CLI not found in PATH")
	}
	if err := exec.Command("sprite", "list").Run(); err != nil {
		t.Skip("sprite list failed; ensure you are logged in")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh CLI not found in PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tokenOut, err := exec.Command("gh", "auth", "token").CombinedOutput()
	if err != nil || strings.TrimSpace(string(tokenOut)) == "" {
		t.Skip("gh auth token not available on host")
	}

	repo := t.TempDir()
	if err := initGitRepo(repo, "https://github.com/1to10partners/seven.git"); err != nil {
		t.Fatalf("git setup failed: %v", err)
	}

	cmdInit := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmdInit.Dir = repo
	cmdInit.Stdout = os.Stdout
	cmdInit.Stderr = os.Stderr
	cmdInit.Env = os.Environ()
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("seven init failed: %v", err)
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

	defer destroySprite(t, repo)

	if err := exec.Command("sprite", "exec", "-s", name, "gh", "--version").Run(); err != nil {
		t.Skip("gh not available in sprite")
	}

	auth := exec.Command("sprite", "exec", "-s", name, "gh", "auth", "status", "-h", "github.com")
	auth.Stdout = os.Stdout
	auth.Stderr = os.Stderr
	if err := auth.Run(); err != nil {
		t.Fatalf("expected gh auth status to succeed in sprite: %v", err)
	}
}

func TestIntegrationCodexChatGPTAuthPersistsInSprite(t *testing.T) {
	if os.Getenv("SEVEN_INTEGRATION") != "1" {
		t.Skip("set SEVEN_INTEGRATION=1 to run integration tests")
	}

	if _, err := exec.LookPath("sprite"); err != nil {
		t.Skip("sprite CLI not found in PATH")
	}
	if err := exec.Command("sprite", "list").Run(); err != nil {
		t.Skip("sprite list failed; ensure you are logged in")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex CLI not found in PATH")
	}

	statusOut, err := exec.Command("codex", "login", "status").CombinedOutput()
	if err != nil || !strings.Contains(string(statusOut), "Logged in using ChatGPT") {
		t.Skip("host codex is not logged in using ChatGPT")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("could not resolve home directory")
	}
	hostAuthPath := filepath.Join(home, ".codex", "auth.json")
	if _, err := os.Stat(hostAuthPath); err != nil {
		t.Skip("host codex auth file not found")
	}

	repo := t.TempDir()
	name := uniqueSpriteName()
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}

	cmdInit := exec.Command(testSevenBin, "init", "--assume-logged-in")
	cmdInit.Dir = repo
	cmdInit.Stdout = os.Stdout
	cmdInit.Stderr = os.Stderr
	cmdInit.Env = os.Environ()
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("seven init failed: %v", err)
	}

	defer destroySprite(t, repo)

	if err := exec.Command("sprite", "exec", "-s", name, "codex", "--version").Run(); err != nil {
		t.Skip("codex not available in sprite")
	}

	status := exec.Command("sprite", "exec", "-s", name, "codex", "login", "status")
	out, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("expected codex login status to succeed in sprite: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Logged in using ChatGPT") {
		t.Fatalf("expected codex ChatGPT auth in sprite, got: %s", out)
	}
}

func spriteListed(name string) bool {
	out, err := exec.Command("sprite", "list").CombinedOutput()
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	return re.Match(out)
}

func destroySprite(t *testing.T, repo string) {
	cmdDestroy := exec.Command(testSevenBin, "destroy")
	cmdDestroy.Dir = repo
	cmdDestroy.Stdout = os.Stdout
	cmdDestroy.Stderr = os.Stderr
	cmdDestroy.Env = os.Environ()
	if err := cmdDestroy.Run(); err != nil {
		t.Fatalf("seven destroy failed: %v", err)
	}
}

func uniqueSpriteName() string {
	suffix := time.Now().UnixNano()
	return "seven-it-" + strconv.FormatInt(suffix, 10)
}

func initGitRepo(dir, remote string) error {
	if err := runCmdInDir(dir, "git", "init"); err != nil {
		return err
	}
	if remote != "" {
		if err := runCmdInDir(dir, "git", "remote", "add", "origin", remote); err != nil {
			return err
		}
	}
	return nil
}

func runCmdInDir(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
