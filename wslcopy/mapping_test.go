//go:build linux

package wslcopy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMapping(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name, source, sourceRoot, windowsRoot, explicit, want string
	}{
		{"mapped", root + "/project/dist", root, `D:\Copies`, "", `D:\Copies\project\dist`},
		{"explicit-wins", root + "/project", "invalid", "invalid", `E:\chosen`, `E:\chosen`},
		{"outside", root + "-other/project", root, `D:\Copies`, "", ""},
		{"root-itself", root, root, `D:\Copies`, "", ""},
		{"missing-root", root + "/project", "", `D:\Copies`, "", ""},
		{"drive-root", root + "/project", root, `D:\`, "", ""},
		{"parent-component", root + "/project", root + "/..", `D:\Copies`, "", ""},
		{"backslash-name", root + `/bad\name/dist`, root, `D:\Copies`, "", ""},
		{"reserved-name", root + "/CON/dist", root, `D:\Copies`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, mappedRoot, err := resolveDestination(tc.source, Options{SourceRoot: tc.sourceRoot, WindowsRoot: tc.windowsRoot, WindowsTarget: tc.explicit})
			if tc.want == "" {
				if err == nil {
					t.Fatal("invalid mapping accepted:", target)
				}
				return
			}
			must(t, err)
			if target.native != tc.want || (mappedRoot == "") != (tc.explicit != "") {
				t.Fatalf("incorrect mapping: %+v, root %q", target, mappedRoot)
			}
		})
	}
	t.Run("public-copy", func(t *testing.T) {
		f := newLocalFixture(t)
		source := filepath.Join(filepath.Dir(f.source), filepath.Base(f.target))
		must(t, os.Rename(f.source, source))
		f.source = source
		t.Setenv("TEST_SOURCE", source)
		opts := f.options()
		opts.WindowsTarget = ""
		opts.SourceRoot, opts.WindowsRoot = filepath.Dir(source), `Q:\portable`
		output, err := Copy(t.Context(), opts)
		must(t, err)
		if output.WindowsPath != f.native || !output.DestinationCreated {
			t.Fatal("public API did not resolve the mapped target:", output)
		}
		assertTree(t, source, f.target)
	})
}
