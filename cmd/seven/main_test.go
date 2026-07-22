package main

import (
	"bytes"
	"encoding/json"
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
		"HOME="+t.TempDir(),
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

// runSevenUpForLog runs `seven up` with the given extra flags/env against a fake
// sprite and returns the captured sprite log.
func runSevenUpForLog(t *testing.T, repo, state, logPath string, extraEnv []string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"up", "--assume-logged-in", "--no-tui"}, args...)
	cmd := exec.Command(testSevenBin, cmdArgs...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	return string(logData)
}

func TestSevenUpGstackInstallsWhenFlagSet(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	log := runSevenUpForLog(t, repo, state, logPath, nil, "--gstack")
	if !strings.Contains(log, "garrytan/gstack") {
		t.Fatalf("expected gstack clone in log, got: %s", log)
	}
	if strings.Contains(log, "bun.sh/install") {
		t.Fatalf("expected no bun install when bun present, got: %s", log)
	}
	if !strings.Contains(log, "./setup --host auto --no-team") {
		t.Fatalf("expected gstack setup for every installed assistant host, got: %s", log)
	}
	for _, want := range []string{
		"mktemp -d \"$gstack_parent/.gstack-seven.XXXXXX\"",
		"core.hooksPath=/dev/null",
		"fetch --depth 1 origin \"" + gstackDefaultRevision + "\"",
		"mv \"$gstack_staging\" \"$HOME/.claude/skills/gstack\"",
		"bun install --frozen-lockfile",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("expected pinned gstack guard %q, got: %s", want, log)
		}
	}
}

func TestSevenUpGstackInstallsWhenProjectRequiresIt(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	log := runSevenUpForLog(t, repo, state, logPath, []string{
		"SPRITE_EXEC_PROJECT_MANIFEST=1",
		"SPRITE_EXEC_GSTACK_REQUIRED=1",
	})
	if !strings.Contains(log, "garrytan/gstack") {
		t.Fatalf("expected project-required gstack install without --gstack, got: %s", log)
	}
}

func TestSevenUpRejectsUnpinnedProjectGstackRevision(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_EXEC_PROJECT_MANIFEST=1",
		"SPRITE_EXEC_GSTACK_REQUIRED=1",
		"SPRITE_EXEC_GSTACK_REVISION=main",
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "malformed gstack row") {
		t.Fatalf("expected mutable gstack revision to fail closed, err=%v output=%s", err, out)
	}
}

func TestSevenUpGstackInstallsChromiumSystemDeps(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	// gstack's ./setup downloads the Chromium binary but not the OS shared
	// libraries it links against, so on a minimal sprite image the browser
	// exits 127 at launch. seven must install those deps before ./setup so
	// setup's own Chromium launch self-check passes.
	log := runSevenUpForLog(t, repo, state, logPath, nil, "--gstack")
	deps := strings.Index(log, "playwright install-deps chromium")
	if deps < 0 {
		t.Fatalf("expected Chromium system-deps install in log, got: %s", log)
	}
	setup := strings.Index(log, "./setup")
	if setup < 0 {
		t.Fatalf("expected gstack ./setup in log, got: %s", log)
	}
	if deps > setup {
		t.Fatalf("expected Chromium system-deps install before ./setup, got: %s", log)
	}
}

func TestSevenUpSkipsGstackWithoutFlag(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	log := runSevenUpForLog(t, repo, state, logPath, nil)
	if strings.Contains(log, "garrytan/gstack") {
		t.Fatalf("expected no gstack install without --gstack, got: %s", log)
	}
}

func TestSevenUpGstackFailsClosedWhenBunMissing(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console", "--gstack")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_EXEC_BUN_MISSING=1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "bun is required") {
		t.Fatalf("expected missing Bun to block provisioning, err=%v output=%s", err, out)
	}
}

func TestProjectToolingInstallScript(t *testing.T) {
	s := projectToolingInstallScript("npm tool tool@1.2.3 tool --version")
	for _, want := range []string{
		"SEVEN_TOOLING_MANIFEST",
		"npm tool tool@1.2.3 tool --version",
		"command -v npm",
		"npm i -g",
		"[project-tooling]",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("install script missing %q; got:\n%s", want, s)
		}
	}
}

func TestValidateProjectToolingManifest(t *testing.T) {
	sha := strings.Repeat("a", 40)
	validWithoutFinalNewline := "gstack gstack " + sha + " -\nnpm tool tool@1.2.3 tool --version\npip-module pynacl pynacl==1.6.2 nacl 1.6.2"
	revision, err := validateProjectToolingManifest(validWithoutFinalNewline)
	if err != nil || revision != sha {
		t.Fatalf("expected valid newline-less manifest, revision=%q err=%v", revision, err)
	}
	for name, manifest := range map[string]string{
		"duplicate gstack": "gstack gstack " + sha + " -\ngstack gstack " + sha + " -\n",
		"duplicate tool":   "npm tool tool@1.2.3 tool --version\npip tool tool==1.2.3 tool --version\n",
		"alias collision":  "archive flyctl v0.4.60|https://example.com/fly_{arch}.tgz|" + strings.Repeat("b", 64) + "|" + strings.Repeat("c", 64) + "|flyctl|fly flyctl version\nnpm fly fly@1.2.3 fly --version\n",
		"malformed gstack": "gstack gstack\n",
		"unknown kind":     "script tool ignored tool --version\n",
		"unpinned npm":     "npm tool tool@latest tool --version\n",
		"unsafe verifier":  "npm tool tool@1.2.3 sh -c\n",
		"shell builtin":    "npm eval eval@1.2.3 eval version\n",
		"module mismatch":  "pip-module pynacl pynacl==1.6.2 nacl 1.6.1\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateProjectToolingManifest(manifest); err == nil {
				t.Fatalf("expected invalid manifest to fail: %q", manifest)
			}
		})
	}
}

func TestSevenUpInstallsProjectToolingWhenManifestPresent(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	// SPRITE_EXEC_PROJECT_MANIFEST=1 makes the fake sprite report the manifest present, so
	// seven runs the install (verify-then-`npm i -g` from the manifest).
	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_EXEC_PROJECT_MANIFEST=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(logPath)
	log := string(data)
	if !strings.Contains(log, "scripts/sprite-tooling.manifest") {
		t.Fatalf("expected the project manifest existence check in log, got: %s", log)
	}
	if !strings.Contains(log, "npm i -g") {
		t.Fatalf("expected project tooling install (npm i -g) when manifest present, got: %s", log)
	}
}

func TestSevenUpNeverRunsRepositoryToolingInstaller(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	log := runSevenUpForLog(t, repo, state, logPath, []string{
		"SPRITE_EXEC_PROJECT_MANIFEST=1",
		"SPRITE_EXEC_PROJECT_INSTALLER=1",
	})
	if strings.Contains(log, "sprite-tooling-install.sh") {
		t.Fatalf("Seven must not execute repository scripts, got: %s", log)
	}
	if !strings.Contains(log, "npm i -g") {
		t.Fatalf("expected Seven's typed manifest interpreter, got: %s", log)
	}
}

func TestSevenUpBlocksNewSpriteWhenRequiredToolingFails(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_EXEC_PROJECT_MANIFEST=1",
		"SPRITE_EXEC_PROJECT_TOOLING_FAIL=1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "required project tooling provisioning failed") {
		t.Fatalf("expected required tooling failure to block new Sprite, err=%v output=%s", err, out)
	}
	logData, _ := os.ReadFile(logPath)
	if strings.Contains(string(logData), "console -s") {
		t.Fatalf("console must not open after required provisioning failure: %s", logData)
	}
}

func TestSevenUpBlocksExistingSpriteWhenRequiredToolingFails(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	_ = runSevenUpForLog(t, repo, state, logPath, nil, "--no-console")
	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_EXEC_PROJECT_MANIFEST=1",
		"SPRITE_EXEC_PROJECT_TOOLING_FAIL=1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "required project tooling provisioning failed") {
		t.Fatalf("expected required tooling failure to block existing Sprite, err=%v output=%s", err, out)
	}
	logData, _ := os.ReadFile(logPath)
	if strings.Contains(string(logData), "console -s") {
		t.Fatalf("console must not open after required provisioning failure: %s", logData)
	}
}

func TestSevenUpSkipsProjectToolingWithoutManifest(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	// Default fake sprite reports no manifest → seven must not run any install.
	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(logPath)
	if strings.Contains(string(data), "npm i -g") {
		t.Fatalf("expected no project tooling install without a manifest, got: %s", string(data))
	}
}

func TestSevenUpFailsClosedWhenManifestProbeErrors(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		"SPRITE_EXEC_MANIFEST_PROBE_FAIL=1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "probe project tooling manifest") {
		t.Fatalf("expected manifest probe failure to block Sprite, err=%v output=%s", err, out)
	}
}

// TestProjectToolingInstallScriptBehavior actually *runs* the production install script (the same
// string returned by projectToolingInstallScript) against a real /bin/sh, with fake npm + verify
// binaries on PATH. Unlike the fake-sprite tests above — which only prove seven dispatches the
// command — this exercises the typed interpreter's real logic and exact-version reconciliation.
func TestProjectToolingInstallScriptBehavior(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	t.Run("installs missing npm and pip tools and skips exact versions", func(t *testing.T) {
		dir := t.TempDir()
		// pathDir is the script's entire PATH; gbin is the npm global bin dir, reachable ONLY via
		// `npm prefix -g` (parent = dir, so dir/bin == gbin). present-tool lives in gbin only, so it
		// resolves solely because the script puts the global bin dir on PATH — the regression guard.
		pathDir := filepath.Join(dir, "path")
		gbin := filepath.Join(dir, "bin")
		if err := os.MkdirAll(pathDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(gbin, 0o755); err != nil {
			t.Fatal(err)
		}
		npmLog := filepath.Join(dir, "npm.log")
		missingState := filepath.Join(dir, "missing.installed")
		pipState := filepath.Join(dir, "ruff.installed")

		// Fake npm: report its global prefix as `dir` (so $prefix/bin == gbin), record every
		// "i -g <spec>" call so we can assert exactly which pinned specs were installed, and fail
		// only for the fail-tool spec to exercise the failed branch.
		writeExecutable(t, filepath.Join(pathDir, "npm"), `#!/bin/sh
case "$1" in
  prefix) printf '%s\n' "`+dir+`"; exit 0 ;;
esac
printf '%s\n' "$*" >> "`+npmLog+`"
: > "`+missingState+`"
exit 0
`)
		writeExecutable(t, filepath.Join(pathDir, "python3"), `#!/bin/sh
: > "`+pipState+`"
exit 0
`)
		writeExecutable(t, filepath.Join(gbin, "present-tool"), "#!/bin/sh\nprintf '1.0.0\\n'\n")
		writeExecutable(t, filepath.Join(gbin, "missing-tool"), `#!/bin/sh
[ -f "`+missingState+`" ] || exit 1
printf '2.0.0\n'
`)
		writeExecutable(t, filepath.Join(pathDir, "ruff"), `#!/bin/sh
[ -f "`+pipState+`" ] || exit 1
printf 'ruff 0.15.18\n'
`)

		manifest := filepath.Join(dir, "sprite-tooling.manifest")
		if err := os.WriteFile(manifest, []byte(`# a comment line is skipped

npm     present-tool  present-tool@1.0.0  present-tool --version
npm     missing-tool  missing-tool@2.0.0  missing-tool --version
pip     ruff          ruff==0.15.18       ruff --version
`), 0o644); err != nil {
			t.Fatal(err)
		}

		manifestData, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		out := runInstallScript(t, projectToolingInstallScript(string(manifestData)), pathDir)

		if !strings.Contains(out, "present: present-tool") {
			t.Errorf("expected present-tool reported present (verify resolved via npm global bin), got: %s", out)
		}
		if !strings.Contains(out, "installed: missing-tool") {
			t.Errorf("expected missing-tool reported installed, got: %s", out)
		}
		if !strings.Contains(out, "installed: missing-tool ruff") {
			t.Errorf("expected missing npm and pip tools reported installed, got: %s", out)
		}

		data, err := os.ReadFile(npmLog)
		if err != nil {
			t.Fatalf("reading npm log: %v", err)
		}
		log := string(data)
		if !strings.Contains(log, "i -g -- missing-tool@2.0.0") {
			t.Errorf("expected npm install of pinned missing-tool spec, got npm log: %q", log)
		}
		if strings.Contains(log, "present-tool") {
			t.Errorf("present-tool verify passed; its npm install must be skipped, got npm log: %q", log)
		}
	})

	t.Run("wrong or unavailable required versions fail closed", func(t *testing.T) {
		dir := t.TempDir()
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeExecutable(t, filepath.Join(binDir, "tool"), "#!/bin/sh\nprintf '0.9.0\\n'\n")
		manifest := filepath.Join(dir, "sprite-tooling.manifest")
		if err := os.WriteFile(manifest, []byte("npm tool tool@1.0.0 tool --version\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		manifestData, readErr := os.ReadFile(manifest)
		if readErr != nil {
			t.Fatal(readErr)
		}
		out, err := runInstallScriptResult(t, projectToolingInstallScript(string(manifestData)), binDir)
		if err == nil || !strings.Contains(out, "failed: tool") {
			t.Errorf("expected stale required tool to fail closed, err=%v output=%s", err, out)
		}
	})
}

// writeExecutable writes an executable script to path (0o755) for use as a fake binary on PATH.
func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("writing fake executable %s: %v", path, err)
	}
}

// runInstallScript runs an install script under /bin/sh with an isolated PATH (binDir only), so the
// only external commands it can reach are the fakes placed in binDir. Returns combined output.
func runInstallScript(t *testing.T, script, binDir string) string {
	t.Helper()
	out, err := runInstallScriptResult(t, script, binDir)
	if err != nil {
		t.Fatalf("install script returned error: %v\n%s", err, out)
	}
	return out
}

func runInstallScriptResult(t *testing.T, script, binDir string) (string, error) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{"PATH=" + binDir, "HOME=" + t.TempDir()}
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

func TestSevenUpGstackReinstallsWhenAlreadyPresent(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	// gstack's ./setup is idempotent and is the documented fix for a partial
	// install, so we always (re-)run it rather than skipping when present.
	log := runSevenUpForLog(t, repo, state, logPath, []string{"SPRITE_EXEC_GSTACK_PRESENT=1"}, "--gstack")
	if !strings.Contains(log, "garrytan/gstack") {
		t.Fatalf("expected gstack setup to run even when already installed, got: %s", log)
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

func TestSevenInitSyncsGitIdentityWithExecSeparator(t *testing.T) {
	// Git identity is synced when the sprite is first created. Use a fresh
	// (not-yet-existing) sprite so we hit the creation path.
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	spriteName := "git-id-sprite"
	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte(spriteName+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
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

func TestSevenUpSkipsGitIdentitySyncWhenSpriteExists(t *testing.T) {
	// Re-syncing git identity on an existing sprite is redundant and has hung
	// in the field, so `seven up` must not do it.
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
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	if strings.Contains(string(logData), "git config --global user.name") {
		t.Fatalf("expected no git identity sync for existing sprite, got: %s", logData)
	}
}

func TestSevenUpSkipsAssistantAuthSyncWhenSpriteExists(t *testing.T) {
	// Claude and codex credentials are synced once at creation, mirroring how
	// `gh` auth is bootstrapped. Re-syncing on every up against an existing
	// sprite was slow (per-call sprite-exec overhead across config + auth for
	// both assistants) and the source of a long pause after the v1.0.1 unfreeze.
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
	claudeDir := filepath.Join(fakeHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("failed to create fake claude dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeHome, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"test@example.com"}}`), 0o600); err != nil {
		t.Fatalf("failed to write fake claude auth file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"theme":"dark-ansi"}`), 0o600); err != nil {
		t.Fatalf("failed to write fake claude config file: %v", err)
	}
	codexDir := filepath.Join(fakeHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("failed to create fake codex dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(`{"OPENAI_API_KEY":null,"tokens":{"id_token":"x"}}`), 0o600); err != nil {
		t.Fatalf("failed to write fake codex auth file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("approval_policy = \"never\"\n"), 0o600); err != nil {
		t.Fatalf("failed to write fake codex config: %v", err)
	}

	binDir := t.TempDir()
	claudeScript := `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ] && [ "$3" = "--json" ]; then
  echo '{"loggedIn":true,"authMethod":"oauth","apiProvider":"firstParty"}'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(claudeScript), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	codexScript := `#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  echo "Logged in using ChatGPT"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(codexScript), 0o755); err != nil {
		t.Fatalf("failed to write fake codex: %v", err)
	}

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"GIT_CONFIG_NOSYSTEM=1",
		"PATH="+binDir+string(os.PathListSeparator)+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
		`SPRITE_EXEC_CLAUDE_AUTH_STATUS_JSON={"loggedIn":true}`,
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
	for _, banned := range []string{
		"syncing claude config into sprite",
		"syncing claude auth into sprite",
		"syncing claude credentials into sprite",
		"syncing codex config into sprite",
		"syncing codex auth into sprite",
	} {
		if strings.Contains(log, banned) {
			t.Fatalf("expected no %q for existing sprite, got: %s", banned, log)
		}
	}
}

func TestSevenInitClonesIntoRepoBaseDirForSibling(t *testing.T) {
	// A sibling sprite (myproj-02) should clone the repo into the family base
	// directory (myproj), not into a directory named after the sprite.
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console", "--sprite", "myproj-02")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, _ := os.ReadFile(logPath)
	log := string(logData)
	if !strings.Contains(log, "gh repo clone octo/hello myproj") {
		t.Fatalf("expected clone into repo base dir 'myproj', got: %s", log)
	}
	if strings.Contains(log, "clone octo/hello myproj-02") {
		t.Fatalf("expected clone dir to be the family base, not the sprite name, got: %s", log)
	}
	if !strings.Contains(log, "-env SEVEN_REPO_DIR=myproj,") {
		t.Fatalf("expected console bootstrap to cd into 'myproj', got: %s", log)
	}
}

func TestSevenUpConfiguresCAlias(t *testing.T) {
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

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logData), "claude --dangerously-skip-permissions") {
		t.Fatalf("expected c alias setup in sprite identity, got: %s", logData)
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
	if !strings.Contains(log, ".config/fish/conf.d/seven-console.fish") {
		t.Fatalf("expected fish shell bootstrap setup, got: %s", log)
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
		`SPRITE_EXEC_CLAUDE_AUTH_STATUS_JSON={"loggedIn":true}`,
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

func TestSevenInitFallsBackWhenClaudeAuthIsNotUsableInSprite(t *testing.T) {
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
		`SPRITE_EXEC_CLAUDE_AUTH_STATUS_JSON={"loggedIn":false}`,
		"SPRITE_EXEC_CLAUDE_AUTH_STATUS_EXIT=1",
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
	if !strings.Contains(log, "-env SEVEN_REPO_DIR="+spriteName+",SEVEN_ASSISTANT=codex") {
		t.Fatalf("expected codex fallback for console hint, got: %s", log)
	}
}

func TestDetectHostClaudeCredentialsLinux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Linux credential-file path; macOS uses the keychain")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := detectHostClaudeCredentials(upOptions{Logger: func(string) {}}); got.present() {
		t.Fatalf("expected no credentials before file exists, got %+v", got)
	}

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	credPath := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"claudeAiOauth":{"accessToken":"x","refreshToken":"y"}}`), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}

	got := detectHostClaudeCredentials(upOptions{Logger: func(string) {}})
	if got.FilePath != credPath || got.Keychain {
		t.Fatalf("expected FilePath=%q keychain=false, got %+v", credPath, got)
	}
}

func TestSevenInitSyncsClaudeCredentialsInSprite(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Linux credential-file path; macOS keychain extraction verified manually")
	}
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	fakeHome := t.TempDir()
	claudeDir := filepath.Join(fakeHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("failed to create fake claude dir: %v", err)
	}
	credPath := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"claudeAiOauth":{"accessToken":"x","refreshToken":"y"}}`), 0o600); err != nil {
		t.Fatalf("failed to write fake claude credentials: %v", err)
	}

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

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected sprite log: %v", err)
	}
	log := string(logData)
	wantSpec := "-file " + credPath + ":/tmp/host-claude-credentials.json"
	if !strings.Contains(log, wantSpec) {
		t.Fatalf("expected claude credentials file upload in sprite exec, got: %s", log)
	}
	if !strings.Contains(log, "$HOME/.claude/.credentials.json") {
		t.Fatalf("expected credentials install into ~/.claude/.credentials.json, got: %s", log)
	}
}

func TestDeepMergeJSON(t *testing.T) {
	t.Run("preserves sprite-only keys", func(t *testing.T) {
		sprite := map[string]interface{}{
			"skipDangerousModePermissionPrompt": true,
			"theme":                             "light",
		}
		host := map[string]interface{}{
			"theme": "dark-ansi",
		}
		got := deepMergeJSON(sprite, host)
		if got["skipDangerousModePermissionPrompt"] != true {
			t.Fatal("expected sprite-only key to be preserved")
		}
		if got["theme"] != "dark-ansi" {
			t.Fatal("expected host key to overwrite sprite key")
		}
	})

	t.Run("deep merges nested maps", func(t *testing.T) {
		sprite := map[string]interface{}{
			"projects": map[string]interface{}{
				"/home/sprite/obsidian": map[string]interface{}{
					"hasTrustDialogAccepted": true,
				},
			},
		}
		host := map[string]interface{}{
			"oauthAccount": map[string]interface{}{
				"emailAddress": "test@example.com",
			},
			"projects": map[string]interface{}{
				"/home/user/obsidian": map[string]interface{}{
					"hasTrustDialogAccepted": true,
				},
			},
		}
		got := deepMergeJSON(sprite, host)
		projects := got["projects"].(map[string]interface{})
		if _, ok := projects["/home/sprite/obsidian"]; !ok {
			t.Fatal("expected sprite-path project to be preserved")
		}
		if _, ok := projects["/home/user/obsidian"]; !ok {
			t.Fatal("expected host-path project to be present")
		}
		if got["oauthAccount"] == nil {
			t.Fatal("expected host-only key to be added")
		}
	})
}

func TestMergedJSONForSprite(t *testing.T) {
	// Write a host config file
	hostFile := filepath.Join(t.TempDir(), "host.json")
	hostContent := map[string]interface{}{"theme": "dark-ansi"}
	hostData, _ := json.Marshal(hostContent)
	if err := os.WriteFile(hostFile, hostData, 0o600); err != nil {
		t.Fatalf("write host file: %v", err)
	}

	t.Run("returns host path when sprite has no file", func(t *testing.T) {
		// spriteExecOutput will fail since there's no real sprite
		path, cleanup, err := mergedJSONForSprite("nonexistent", hostFile, `cat /nonexistent 2>/dev/null`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleanup != nil {
			defer cleanup()
		}
		if path != hostFile {
			t.Fatalf("expected original host path when sprite file missing, got: %s", path)
		}
	})
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

func TestSpriteColorStableAndInPalette(t *testing.T) {
	a := spriteColor("myrepo")
	if a != spriteColor("myrepo") {
		t.Fatalf("spriteColor should be stable for the same name")
	}
	inPalette := false
	for _, c := range spriteIdentityPalette {
		if a == fmt.Sprintf("%d", c) {
			inPalette = true
			break
		}
	}
	if !inPalette {
		t.Fatalf("spriteColor %q not in palette", a)
	}
	if spriteColor("myrepo") == spriteColor("myrepo-02") && spriteColor("myrepo-02") == spriteColor("myrepo-03") {
		t.Fatalf("expected sibling names to spread across colors, got identical colors")
	}
}

func TestNextSiblingSpriteName(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		listOut string
		want    string
	}{
		{
			// Regression: main + one adjacent sibling is the common case. A prior
			// regex implementation consumed the newline between them as a match
			// boundary, so only the main sprite matched and --new returned the
			// existing -02 (wrecking it) instead of allocating -03.
			name:    "main plus adjacent sibling",
			base:    "soclimmo",
			listOut: "soclimmo\nsoclimmo-02\n",
			want:    "soclimmo-03",
		},
		{
			name:    "main only",
			base:    "soclimmo",
			listOut: "soclimmo\n",
			want:    "soclimmo-02",
		},
		{
			name:    "no family members yet",
			base:    "soclimmo",
			listOut: "",
			want:    "soclimmo-02",
		},
		{
			name:    "gaps and unrelated families ignored",
			base:    "soclimmo",
			listOut: "soclimmo\nsoclimmo-04\nother\nother-02\n",
			want:    "soclimmo-05",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextSiblingSpriteName(tc.base, tc.listOut); got != tc.want {
				t.Fatalf("nextSiblingSpriteName(%q, %q) = %q, want %q", tc.base, tc.listOut, got, tc.want)
			}
		})
	}
}

func TestSiblingSpriteNameForOrdinal(t *testing.T) {
	cases := map[int]string{1: "base", 2: "base-02", 3: "base-03", 10: "base-10"}
	for n, want := range cases {
		if got := siblingSpriteNameForOrdinal("base", n); got != want {
			t.Fatalf("siblingSpriteNameForOrdinal(base, %d) = %q, want %q", n, got, want)
		}
	}
}

func TestSpriteFamilyMembers(t *testing.T) {
	listOut := "seven\nseven-02\nseven-03\nother-app\n"
	got := spriteFamilyMembers("seven", listOut)
	want := []string{"seven", "seven-02", "seven-03"}
	if len(got) != len(want) {
		t.Fatalf("spriteFamilyMembers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("spriteFamilyMembers[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestSevenUpOrdinalSelectsSibling(t *testing.T) {
	repo := createTempRepo(t)
	state, logPath, cleanup := createFakeSprite(t)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte("seven\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte("seven\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "up", "2", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up 2 failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(filepath.Join(repo, ".sprite"))
	if err != nil {
		t.Fatalf("expected .sprite file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "seven-02" {
		t.Fatalf("expected .sprite to select seven-02, got %q", got)
	}
	logData, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logData), "create seven-02") {
		t.Fatalf("expected create log for seven-02, got: %s", logData)
	}
}

func TestSevenUpConfiguresSpriteIdentity(t *testing.T) {
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

	cmd := exec.Command(testSevenBin, "up", "--assume-logged-in", "--no-tui", "--no-console")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
		"SPRITE_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven up failed: %v\n%s", err, output)
	}

	logData, _ := os.ReadFile(logPath)
	log := string(logData)
	if !strings.Contains(log, ".seven-sprite-id.sh") {
		t.Fatalf("expected sprite identity setup in log, got: %s", log)
	}
	if !strings.Contains(log, "SEVEN_SPRITE_NAME="+spriteName) {
		t.Fatalf("expected identity env with sprite name, got: %s", log)
	}
}

func TestSevenListListsFamily(t *testing.T) {
	// "list" is the primary command; "ls" is an accepted alias.
	for _, sub := range []string{"list", "ls"} {
		t.Run(sub, func(t *testing.T) {
			repo := t.TempDir()
			state, logPath, cleanup := createFakeSprite(t)
			defer cleanup()

			if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte("seven\n"), 0o644); err != nil {
				t.Fatalf("failed to write .sprite: %v", err)
			}
			if err := os.WriteFile(state, []byte("seven\nseven-02\nother-app\n"), 0o644); err != nil {
				t.Fatalf("failed to write state: %v", err)
			}

			cmd := exec.Command(testSevenBin, sub)
			cmd.Dir = repo
			cmd.Env = append(os.Environ(),
				"NO_COLOR=1",
				"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
				"SPRITE_STATE="+state,
				"SPRITE_LOG="+logPath,
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("seven %s failed: %v\n%s", sub, err, output)
			}
			out := string(output)
			if !strings.Contains(out, "sprite family for seven") {
				t.Fatalf("expected family header, got: %s", out)
			}
			if !strings.Contains(out, "seven (main)") {
				t.Fatalf("expected main sprite labeled, got: %s", out)
			}
			if !strings.Contains(out, "seven-02") {
				t.Fatalf("expected sibling listed, got: %s", out)
			}
			if strings.Contains(out, "other-app") {
				t.Fatalf("list should not show unrelated sprites, got: %s", out)
			}
			if !strings.Contains(out, "*") {
				t.Fatalf("expected selected marker, got: %s", out)
			}
		})
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

func TestSevenDestroyPositionalNameTargetsThatSprite(t *testing.T) {
	// Regression: "seven destroy other-sprite" (positional name) must destroy
	// other-sprite, not silently fall back to the selected current-sprite.
	repo := t.TempDir()
	state, _, cleanup := createFakeSprite(t)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte("current-sprite\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte("current-sprite\nother-sprite\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "destroy", "other-sprite")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seven destroy <name> failed: %v\n%s", err, output)
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

func TestSevenDestroyRejectsTooManyArguments(t *testing.T) {
	repo := t.TempDir()
	state, _, cleanup := createFakeSprite(t)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(repo, ".sprite"), []byte("current-sprite\n"), 0o644); err != nil {
		t.Fatalf("failed to write .sprite: %v", err)
	}
	if err := os.WriteFile(state, []byte("current-sprite\n"), 0o644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	cmd := exec.Command(testSevenBin, "destroy", "one", "two")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Dir(state)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SPRITE_STATE="+state,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected seven destroy to fail with too many arguments\n%s", output)
	}
	if !bytes.Contains(output, []byte("too many arguments")) {
		t.Fatalf("expected too-many-arguments error, got: %s", output)
	}
	// The selected sprite must be left untouched.
	stateData, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
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
    case "$exec_args" in
      *" -- claude auth status --json")
        if [ -n "${SPRITE_EXEC_CLAUDE_AUTH_STATUS_JSON:-}" ]; then
          printf '%s\n' "$SPRITE_EXEC_CLAUDE_AUTH_STATUS_JSON"
        fi
        if [ -n "${SPRITE_EXEC_CLAUDE_AUTH_STATUS_EXIT:-}" ]; then
          exit "$SPRITE_EXEC_CLAUDE_AUTH_STATUS_EXIT"
        fi
        exit 0
        ;;
      *"command -v bun"*)
        if [ "${SPRITE_EXEC_BUN_MISSING:-}" = "1" ]; then
          exit 1
        fi
        exit 0
        ;;
	  *"printf 'present'"*sprite-tooling.manifest*)
		if [ "${SPRITE_EXEC_MANIFEST_PROBE_FAIL:-}" = "1" ]; then
		  exit 1
		fi
		if [ "${SPRITE_EXEC_PROJECT_MANIFEST:-}" = "1" ] || [ "${SPRITE_EXEC_GSTACK_REQUIRED:-}" = "1" ]; then
		  printf 'present'
		else
		  printf 'absent'
		fi
		exit 0
		;;
	  *SEVEN_TOOLING_MANIFEST*)
		if [ "${SPRITE_EXEC_PROJECT_TOOLING_FAIL:-}" = "1" ]; then
		  exit 1
		fi
		exit 0
		;;
	  *"cat \""*sprite-tooling.manifest*)
		if [ -n "${SPRITE_EXEC_PROJECT_MANIFEST_CONTENT:-}" ]; then
		  printf '%s' "$SPRITE_EXEC_PROJECT_MANIFEST_CONTENT"
		elif [ "${SPRITE_EXEC_GSTACK_REQUIRED:-}" = "1" ]; then
		  printf 'gstack gstack %s -\n' "${SPRITE_EXEC_GSTACK_REVISION:-a3259400a366593e0c909dd9ac3e59752efd2488}"
		else
		  printf 'npm tool tool@1.0.0 tool --version\n'
		fi
		exit 0
		;;
	  *sprite-tooling.manifest*)
        # Simulate a project tooling manifest being present/absent in the cloned repo.
        # Default: absent (exit 1), so most tests don't trigger the install path.
        if [ "${SPRITE_EXEC_PROJECT_MANIFEST:-}" = "1" ]; then
          exit 0
        fi
        exit 1
        ;;
    esac
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
