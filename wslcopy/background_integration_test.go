//go:build linux

package wslcopy

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func testWindowsRetirementLifetime(t *testing.T, env windowsEnvironment) {
	f := newWindowsFixture(t, env)
	must(t, os.Mkdir(f.target, 0700))
	writeFile(t, filepath.Join(f.target, "old-only"), "previous")
	binary := filepath.Join(t.TempDir(), "wsl-copy")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../cmd/wsl-copy")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	// The real native worker cannot delete anything until AFTER the CLI has exited.
	// This barrier is test-only; production never waits/polls for retirement.
	release, started := filepath.Join(f.root, "release"), filepath.Join(f.root, "started")
	t.Cleanup(func() { writeFile(t, release, "release") })
	gateScript := "[IO.File]::WriteAllText(" + psQuote(f.win(t, started)) + ", 'native worker alive'); " +
		"$deadline = [DateTime]::UtcNow.AddSeconds(30); while (-not [IO.File]::Exists(" + psQuote(f.win(t, release)) + ")) { " +
		"if ([DateTime]::UtcNow -gt $deadline) { throw 'test gate timed out' }; Start-Sleep -Milliseconds 20 }; "
	t.Setenv("TEST_GATE_SCRIPT", gateScript)
	t.Setenv("TEST_NATIVE_POWERSHELL", env.powershell)
	wrapper := filepath.Join(t.TempDir(), "powershell")
	writeScript(t, wrapper, `
for script; do :; done
case "$script" in
  *'Refusing an unrecognized retirement directory'*)
    exec "$TEST_NATIVE_POWERSHELL" -NoLogo -NoProfile -NonInteractive -Command "$TEST_GATE_SCRIPT$script" ;;
  *) exec "$TEST_NATIVE_POWERSHELL" "$@" ;;
esac
`)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, f.source, f.win(t, f.target), "--replace", "--config", "", "--windows-tar", env.tar, "--powershell", wrapper)
	cmd.WaitDelay = time.Second // Bound a regression that leaks CLI pipes into the worker.
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("CLI waited for retirement or failed: %v; %s", err, output)
	}
	cancel()
	var result map[string]any
	must(t, json.Unmarshal(output, &result))
	if result["status"] != "copied" || result["cleanup_error"] != nil || result["previous_windows_path"] != nil {
		t.Fatal("unexpected CLI result:", result)
	}
	assertTree(t, f.source, f.target)
	containers, err := filepath.Glob(filepath.Join(f.root, ".wsl-copy-*"))
	must(t, err)
	if len(containers) != 1 || readFile(t, filepath.Join(containers[0], "previous", "old-only")) != "previous" {
		t.Fatal("old tree was not retained at CLI exit")
	}
	writeFile(t, release, "release")
	must(t, waitRetirements(f.root))
	absent(t, containers[0])
	if readFile(t, started) != "native worker alive" {
		t.Fatal("native worker did not survive CLI exit")
	}
	assertTree(t, f.source, f.target)
}
