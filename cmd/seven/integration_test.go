package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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

	cmdUp := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-console")
	cmdUp.Dir = repo
	cmdUp.Stdout = os.Stdout
	cmdUp.Stderr = os.Stderr
	if err := cmdUp.Run(); err != nil {
		t.Fatalf("seven up failed: %v", err)
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

	if !spriteListed(name) {
		defer destroySprite(t, repo)
		t.Fatalf("sprite not found in list: %s", name)
	}

	destroySprite(t, repo)

	if spriteListed(name) {
		t.Fatalf("sprite still present after destroy: %s", name)
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
	if err := cmdDestroy.Run(); err != nil {
		t.Fatalf("seven destroy failed: %v", err)
	}
}
