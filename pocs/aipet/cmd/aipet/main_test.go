package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildAipet compiles the CLI once per test binary run into a temp dir, so
// the R9 surface (bare `aipet`, `aipet setup`) can be exercised as a real
// process — main() calls os.Exit and reads os.Args directly, so it cannot
// be driven in-process without forking a subprocess. Every invocation below
// sets HOME to an isolated temp dir (R7): none of this may ever touch the
// real ~/.claude, ~/.codex, or ~/.aipet.
func buildAipet(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "aipet")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = mustGetwd(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build aipet: %v\n%s", err, out)
	}
	return bin
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// run executes the built binary with an isolated HOME and returns
// stdout+stderr combined, so assertions can check either stream without
// caring which one a given line landed on.
func run(t *testing.T, bin, home string, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return out.String(), code
}

// TestBareCommandNonInteractiveNoInstall: piped/non-TTY stdin with no prior
// setup must print manual instructions and exit 0 — it must never block
// waiting for a y/n answer nobody can give (this IS the non-interactive
// case; exec.Command's default stdin is not a TTY, so this alone proves the
// no-hang path).
func TestBareCommandNonInteractiveNoInstall(t *testing.T) {
	bin := buildAipet(t)
	home := t.TempDir()

	out, code := run(t, bin, home, "", []string{}...)
	if code != 0 {
		t.Fatalf("bare aipet (non-interactive, not installed) exit=%d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "aipet setup") {
		t.Errorf("expected manual setup instructions, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".aipet", "setup.json")); !os.IsNotExist(err) {
		t.Error("non-interactive bare aipet must not install anything")
	}
}

// TestBareCommandAfterInstallShowsCardAndHint verifies the R9 "manifest
// exists" branch: card + the /aipet hint line, no wizard prompt.
func TestBareCommandAfterInstallShowsCardAndHint(t *testing.T) {
	bin := buildAipet(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if out, code := run(t, bin, home, "", "setup"); code != 0 {
		t.Fatalf("setup failed: %d\n%s", code, out)
	}

	out, code := run(t, bin, home, "")
	if code != 0 {
		t.Fatalf("bare aipet (installed) exit=%d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "type /aipet inside Claude Code") {
		t.Errorf("expected the /aipet hint line, got:\n%s", out)
	}
	if !strings.Contains(out, "egg") {
		t.Errorf("expected the pet card (egg state) in bare output, got:\n%s", out)
	}
}

// TestSetupNoHostsFound: with neither ~/.claude nor ~/.codex present, setup
// must say so clearly and exit 0 rather than error.
func TestSetupNoHostsFound(t *testing.T) {
	bin := buildAipet(t)
	home := t.TempDir()

	out, code := run(t, bin, home, "", "setup")
	if code != 0 {
		t.Fatalf("setup with no hosts exit=%d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "No Claude Code") {
		t.Errorf("expected a clear no-hosts message, got:\n%s", out)
	}
}

// TestSetupPrintWritesNothing verifies the CLI plumbing for --print: the
// underlying host.Install(Print:true) contract is tested directly in
// internal/host, this just confirms the flag reaches it and nothing lands
// on disk via the compiled binary.
func TestSetupPrintWritesNothing(t *testing.T) {
	bin := buildAipet(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, bin, home, "", "setup", "--print")
	if code != 0 {
		t.Fatalf("setup --print exit=%d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "WOULD WRITE") {
		t.Errorf("expected a WOULD WRITE preview, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "commands", "aipet.md")); !os.IsNotExist(err) {
		t.Error("setup --print must not write the command file")
	}
	if _, err := os.Stat(filepath.Join(home, ".aipet", "setup.json")); !os.IsNotExist(err) {
		t.Error("setup --print must not write a manifest")
	}
}

// TestSetupRemoveUndoesInstall exercises the full setup -> remove round
// trip through the CLI.
func TestSetupRemoveUndoesInstall(t *testing.T) {
	bin := buildAipet(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if out, code := run(t, bin, home, "", "setup"); code != 0 {
		t.Fatalf("setup failed: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "commands", "aipet.md")); err != nil {
		t.Fatalf("expected command file after setup: %v", err)
	}

	out, code := run(t, bin, home, "", "setup", "--remove")
	if code != 0 {
		t.Fatalf("setup --remove exit=%d, want 0. output:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "commands", "aipet.md")); !os.IsNotExist(err) {
		t.Error("command file should be gone after --remove")
	}
	if _, err := os.Stat(filepath.Join(home, ".aipet", "setup.json")); !os.IsNotExist(err) {
		t.Error("manifest should be gone after --remove")
	}
}

// TestUsageListsNewSurface checks the R9 usage() rewrite surfaces the new
// commands and no longer advertises daemon/status/dex/leaderboard/config as
// primary entry points.
func TestUsageListsNewSurface(t *testing.T) {
	bin := buildAipet(t)
	home := t.TempDir()

	out, code := run(t, bin, home, "", "help")
	if code != 0 {
		t.Fatalf("help exit=%d, want 0", code)
	}
	for _, want := range []string{"aipet setup", "aipet tui", "/aipet"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage() missing %q, got:\n%s", want, out)
		}
	}
	for _, oldEntry := range []string{"aipet daemon ", "aipet leaderboard "} {
		if strings.Contains(out, oldEntry) {
			t.Errorf("usage() should no longer list %q as a primary command, got:\n%s", oldEntry, out)
		}
	}
}

// TestDaemonStillWorksWithDeprecationNote: R9 says daemon/status/dex/
// leaderboard/config keep working exactly as before, just delisted from
// usage(). This checks the daemon path still starts (and prints the new
// pointer note) rather than having been removed.
func TestDaemonPrintsDeprecationNote(t *testing.T) {
	bin := buildAipet(t)
	home := t.TempDir()

	cmd := exec.Command(bin, "daemon")
	cmd.Env = append(os.Environ(), "HOME="+home)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Read until the deprecation note appears (it's the first line printed,
	// before the daemon does any collection work) rather than racing a fixed
	// sleep against process scheduling.
	buf := make([]byte, 4096)
	var collected strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			collected.Write(buf[:n])
			if strings.Contains(collected.String(), "hooks replace the daemon") {
				return
			}
		}
		if readErr != nil {
			break
		}
	}
	t.Errorf("expected the deprecation note in daemon output, got:\n%s", collected.String())
}
