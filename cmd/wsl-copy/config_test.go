package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	const valid = `{
		"source_root": "/home/example",
		"windows_root": "D:\\Copies",
		"replace": true,
		"lock_file": "/state/shared.lock",
		"windows_tar": "/tools/tar.exe",
		"powershell": "/tools/powershell.exe"
	}`
	want := configuration{
		SourceRoot: "/home/example", WindowsRoot: `D:\Copies`, Replace: true, LockFile: "/state/shared.lock",
		WindowsTar: "/tools/tar.exe", PowerShell: "/tools/powershell.exe",
	}
	for _, tt := range []struct {
		name     string
		path     string
		explicit bool
		content  string
		missing  bool
		want     configuration
		wantErr  bool
	}{
		{name: "default missing", missing: true},
		{name: "default valid", content: valid, want: want},
		{name: "explicit valid", path: "custom.json", explicit: true, content: valid + "\n\t", want: want},
		{name: "explicit disabling", explicit: true, content: "invalid default config"},
		{name: "explicit missing", path: "custom.json", explicit: true, missing: true, wantErr: true},
		{name: "empty object", path: "custom.json", explicit: true, content: `{}`},
		{name: "empty file", path: "custom.json", explicit: true, wantErr: true},
		{name: "malformed", path: "custom.json", explicit: true, content: `{"source_root":`, wantErr: true},
		{name: "unknown field", path: "custom.json", explicit: true, content: `{"source_rooot":"/source"}`, wantErr: true},
		{name: "trailing JSON", path: "custom.json", explicit: true, content: `{} {}`, wantErr: true},
		{name: "trailing garbage", path: "custom.json", explicit: true, content: `{} invalid`, wantErr: true},
		{name: "null", path: "custom.json", explicit: true, content: `null`, wantErr: true},
		{name: "array", path: "custom.json", explicit: true, content: `[]`, wantErr: true},
		{name: "scalar", path: "custom.json", explicit: true, content: `true`, wantErr: true},
		{name: "wrong field type", path: "custom.json", explicit: true, content: `{"replace":"true"}`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
			configFile := filepath.Join(root, "config", "wsl-copy", "config.json")
			path := ""
			if tt.path != "" {
				path = filepath.Join(root, tt.path)
				configFile = path
			}
			if !tt.missing {
				must(t, os.MkdirAll(filepath.Dir(configFile), 0o700))
				must(t, os.WriteFile(configFile, []byte(tt.content), 0o600))
			}

			got, err := loadConfig(path, tt.explicit)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected invalid or missing config to fail")
				}
				return
			}
			must(t, err)
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
