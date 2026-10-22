//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstaller(t *testing.T) {
	script, err := os.ReadFile("../../install.sh")
	must(t, err)
	realInstall, err := exec.LookPath("install")
	must(t, err)
	payload, release := t.TempDir(), t.TempDir()
	files := map[string]string{
		"wsl-copy": "#!/bin/sh\nprintf 'wsl-copy v0.1.0\\n'\n",
		"LICENSE":  "license notice\n", "THIRD_PARTY_NOTICES": "third-party notices\n",
	}
	for name, content := range files {
		must(t, os.WriteFile(filepath.Join(payload, name), []byte(content), 0o755))
	}
	var sums strings.Builder
	for _, arch := range []string{"amd64", "arm64"} {
		name := "wsl-copy-linux-" + arch + ".tar.gz"
		out, err := exec.CommandContext(t.Context(), "tar", "-czf", filepath.Join(release, name), "-C", payload, "wsl-copy", "LICENSE", "THIRD_PARTY_NOTICES").CombinedOutput()
		if err != nil {
			t.Fatalf("create release fixture: %v: %s", err, out)
		}
		data, err := os.ReadFile(filepath.Join(release, name))
		must(t, err)
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
	}
	must(t, os.WriteFile(filepath.Join(release, "SHA256SUMS"), []byte(sums.String()), 0o600))
	for _, tc := range []struct {
		name, arch, version, fault, wantError string
		update                                bool
	}{
		{name: "latest", arch: "x86_64"},
		{name: "update-latest", arch: "x86_64", update: true},
		{name: "update-pinned", arch: "x86_64", version: "v0.1.0", update: true},
		{name: "update-checksum", arch: "x86_64", fault: "checksum", wantError: "Checksum verification failed", update: true},
		{name: "explicit", arch: "x86_64", version: "v0.1.0"},
		{name: "arm64-selection", arch: "aarch64"},
		{name: "checksum", arch: "x86_64", fault: "checksum", wantError: "Checksum verification failed"},
		{name: "download", arch: "x86_64", fault: "download", wantError: "injected download failure"},
		{name: "install-failure", arch: "x86_64", fault: "install", wantError: "injected installation failure"},
		{name: "size-limit", arch: "x86_64", fault: "size-limit", wantError: "Download exceeds the size limit"},
		{name: "unsupported", arch: "riscv64", wantError: "Unsupported architecture"},
		{name: "truncated-script", arch: "x86_64", fault: "truncated", wantError: "syntax error"},
		{name: "invalid-version", arch: "x86_64", version: "v0.1.0\nother", wantError: "No valid release version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home, temp, bin := filepath.Join(root, "home with spaces"), filepath.Join(root, "tmp"), filepath.Join(root, "bin")
			for _, dir := range []string{home, temp, bin, filepath.Join(home, ".local/bin")} {
				must(t, os.MkdirAll(dir, 0o700))
			}
			target := filepath.Join(home, ".local/bin/wsl-copy")
			must(t, os.WriteFile(target, []byte("previous installation"), 0o700))
			config := filepath.Join(home, ".config", "wsl-copy", "config.json")
			must(t, os.MkdirAll(filepath.Dir(config), 0700))
			must(t, os.WriteFile(config, []byte("keep user configuration"), 0600))
			must(t, os.WriteFile(filepath.Join(bin, "uname"), []byte("#!/bin/sh\ncase $1 in -s) echo Linux;; -m) echo \"$TEST_ARCH\";; esac\n"), 0o700))
			must(t, os.WriteFile(filepath.Join(bin, "curl"), []byte(`#!/bin/sh
set -eu
output= head= url=
while [ "$#" -gt 0 ]; do
    case "$1" in
        --output) output=$2; shift 2 ;;
        --head) head=1; shift ;;
        https://*) url=$1; shift ;;
        *) shift ;;
    esac
done
printf '%s\n' "$url" >> "$TEST_REQUESTS"
if [ "$TEST_FAULT" = download ]; then echo 'injected download failure' >&2; exit 22; fi
if [ -n "$head" ]; then
    if [ "$TEST_FAULT" = size-limit ]; then printf 'Content-Length: 999999999\r\n'; fi
    exit 0
fi
case "$url" in
    'https://api.github.com/repos/wangnan0916/wsl-copy/releases?per_page=1')
        printf '[{"tag_name":"v0.1.0","prerelease":true}]\n' > "$output" ;;
    https://github.com/wangnan0916/wsl-copy/releases/download/v0.1.0/*)
        cp "$TEST_RELEASE/${url##*/}" "$output"
        if [ "$TEST_FAULT" = checksum ] && [ "${url##*/}" = SHA256SUMS ]; then
            printf '%064d  wsl-copy-linux-amd64.tar.gz\n' 0 > "$output"
        fi ;;
    *) echo 'unexpected URL' >&2; exit 90 ;;
esac
`), 0o700))
			must(t, os.WriteFile(filepath.Join(bin, "install"), []byte(`#!/bin/sh
set -eu
for target do :; done
case "$target" in
    */wsl-copy|*/.wsl-copy.*)
        if [ "$TEST_FAULT" = install ]; then
            printf partial > "$target"
            echo 'injected installation failure' >&2
            exit 1
        fi ;;
esac
exec "$TEST_REAL_INSTALL" "$@"
`), 0o700))
			for key, value := range map[string]string{
				"HOME": home, "TMPDIR": temp, "PATH": bin + ":" + os.Getenv("PATH"),
				"TEST_ARCH": tc.arch, "TEST_RELEASE": release, "TEST_FAULT": tc.fault, "TEST_REQUESTS": filepath.Join(root, "requests"),
				"TEST_REAL_INSTALL": realInstall,
				"TAR_OPTIONS":       "--invalid-option", // Installer must not inherit extraction options.
			} {
				t.Setenv(key, value)
			}
			cmd := exec.CommandContext(t.Context(), "sh", "-s", "--")
			if tc.version != "" {
				cmd.Args = append(cmd.Args, tc.version)
			}
			input := string(script)
			if tc.fault == "truncated" {
				input = input[:strings.LastIndex(input, "\n}")]
			}
			cmd.Stdin = strings.NewReader(input)
			var out []byte
			if tc.update {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						fmt.Fprint(w, input)
					}
				}))
				defer server.Close()
				var versions []string
				if tc.version != "" {
					versions = []string{tc.version}
				}
				var output bytes.Buffer
				err = updateInstallation(t.Context(), server.URL, versions, &output, &output)
				out = output.Bytes()
			} else {
				out, err = cmd.CombinedOutput()
			}
			configData, configErr := os.ReadFile(config)
			must(t, configErr)
			if string(configData) != "keep user configuration" {
				t.Fatal("configuration changed during installation/update")
			}
			if tc.wantError != "" {
				if err == nil || !strings.Contains(strings.ToLower(string(out)), strings.ToLower(tc.wantError)) {
					t.Fatalf("expected %q: %v: %s", tc.wantError, err, out)
				}
				data, err := os.ReadFile(target)
				must(t, err)
				if string(data) != "previous installation" {
					t.Fatal("failed installer replaced the existing installation")
				}
			} else {
				if err != nil {
					t.Fatalf("installer failed: %v: %s", err, out)
				}
				for name, content := range files {
					path := filepath.Join(home, ".local/share/wsl-copy", name)
					if name == "wsl-copy" {
						path = target
					}
					data, err := os.ReadFile(path)
					must(t, err)
					if string(data) != content {
						t.Fatalf("incorrect installed file: %s", name)
					}
				}
				requests, err := os.ReadFile(filepath.Join(root, "requests"))
				must(t, err)
				arch := "amd64"
				if tc.arch == "aarch64" {
					arch = "arm64"
				}
				if !strings.Contains(string(requests), "/wsl-copy-linux-"+arch+".tar.gz") {
					t.Fatal("incorrect download architecture")
				}
				if strings.Contains(string(requests), "api.github.com") != (tc.version == "") {
					t.Fatal("explicit version must bypass release discovery")
				}
			}
			staged, err := os.ReadDir(filepath.Dir(target))
			must(t, err)
			if len(staged) != 1 || staged[0].Name() != "wsl-copy" {
				t.Fatal("installer left staged executables behind")
			}
			entries, err := os.ReadDir(temp)
			must(t, err)
			if len(entries) != 0 {
				t.Fatal("installer left temporary downloads behind")
			}
		})
	}
}
