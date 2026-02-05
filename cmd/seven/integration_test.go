package main

import (
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

	token, ok := loadSpriteToken()
	if !ok {
		t.Skip("SPRITE_TOKEN not set (must be a spr_ API token)")
	}
	restoreEnv := setEnv("SPRITE_TOKEN", token)
	defer restoreEnv()

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

	cmdUp := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-console")
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

	token, ok := loadSpriteToken()
	if !ok {
		t.Skip("SPRITE_TOKEN not set (must be a spr_ API token)")
	}
	restoreEnv := setEnv("SPRITE_TOKEN", token)
	defer restoreEnv()

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

	destroySprite(t, repo)
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

func loadSpriteToken() (string, bool) {
	token := strings.TrimSpace(os.Getenv("SPRITE_TOKEN"))
	if token == "" {
		return "", false
	}
	return token, true
}

func setEnv(key, value string) func() {
	prev, had := os.LookupEnv(key)
	_ = os.Setenv(key, value)
	return func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}
