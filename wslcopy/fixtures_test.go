//go:build linux

package wslcopy

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	must(t, os.WriteFile(path, []byte(content), 0600))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	must(t, err)
	return string(data)
}

func writeScript(t *testing.T, path, script string) {
	t.Helper()
	must(t, os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script), 0700))
}

func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing path %s, got %v", path, err)
	}
}

type localFixture struct {
	source, parent, target, native, bin, archives string
}

func newLocalFixture(t *testing.T) localFixture {
	t.Helper()
	gnuTar, err := exec.LookPath("tar")
	must(t, err)
	root := t.TempDir()
	f := localFixture{
		source: filepath.Join(root, "source"), parent: filepath.Join(root, "parent"),
		native: `Q:\portable\中文 O'Brien`, bin: filepath.Join(root, "bin"), archives: filepath.Join(root, "archives"),
	}
	f.target = filepath.Join(f.parent, "中文 O'Brien")
	for _, dir := range []string{f.source, f.parent, f.bin, f.archives} {
		must(t, os.Mkdir(dir, 0700))
	}
	writeFile(t, filepath.Join(f.source, "notes.txt"), "content")
	for key, value := range map[string]string{
		"PATH": f.bin + string(os.PathListSeparator) + os.Getenv("PATH"), "TMPDIR": f.archives,
		"TEST_SOURCE": f.source, "TEST_PARENT": f.parent, "TEST_TARGET": f.target, "TEST_GNU_TAR": gnuTar,
		"TEST_TARGET_NATIVE": f.native,
		"TEST_SOURCE_NATIVE": `\\wsl.localhost\TestDistro\source`, "TEST_PARENT_NATIVE": `Q:\portable`,
		"TEST_NATIVE_ERROR": "", "TEST_EXTRACT_FAIL": "",
	} {
		t.Setenv(key, value)
	}
	writeScript(t, filepath.Join(f.bin, "wslpath"), `
if [ "$1" = '-w' ]; then
  if [ "$2" = '/' ]; then printf '%s\n' '\\wsl.localhost\TestDistro\'; exit 0; fi
  if [ "$2" = "$TEST_SOURCE" ]; then printf '%s\n' "$TEST_SOURCE_NATIVE"; exit 0; fi
  if [ "$2" = "$TEST_PARENT" ]; then printf '%s\n' "$TEST_PARENT_NATIVE"; exit 0; fi
elif [ "$1" = '-u' ]; then
  printf '%s\n' "$TEST_PARENT"; exit 0
fi
exit 91
`)
	writeScript(t, filepath.Join(f.bin, "powershell.exe"), `
if [ "$TEST_NATIVE_ERROR" = '1' ]; then echo 'preparation refused' >&2; exit 1; fi
`)
	writeScript(t, filepath.Join(f.bin, "tar.exe"), `
if [ "$#" -lt 4 ] || [ "$3" != '-C' ] || [ "$4" != "$TEST_TARGET_NATIVE" ]; then
  echo 'unexpected extraction arguments' >&2; exit 90
fi
mode="$1"
archive="$2"
shift 4
if [ "$TEST_EXTRACT_FAIL" = '1' ]; then
  printf 'partial data' > "$TEST_TARGET/partial"
  echo 'simulated extraction failure' >&2
  exit 2
fi
unset TAR_OPTIONS
exec "$TEST_GNU_TAR" "$mode" "$archive" -C "$TEST_TARGET" "$@"
`)
	return f
}

func (f localFixture) options() Options {
	return Options{Source: f.source, WindowsTarget: f.native}
}

func assertTree(t *testing.T, source, target string) {
	t.Helper()
	seen := make(map[string]bool)
	must(t, filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		seen[rel] = true
		destination := filepath.Join(target, rel)
		info, err := os.Lstat(destination)
		if err != nil {
			return err
		}
		if entry.Type() != info.Mode().Type() {
			return errors.New("entry type mismatch: " + rel)
		}
		if !entry.IsDir() && readFile(t, path) != readFile(t, destination) {
			return errors.New("content mismatch: " + rel)
		}
		return nil
	}))
	must(t, filepath.WalkDir(target, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(target, path)
		if err != nil {
			return err
		}
		if !seen[rel] {
			return errors.New("unexpected destination entry: " + rel)
		}
		return nil
	}))
}
