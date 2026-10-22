//go:build linux

package wslcopy

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDestinationPaths(t *testing.T) {
	for _, value := range []string{"", "relative", "/mnt/q/output", "Q:relative", `Q:\`, `Q:\.\`, `\\server\share\output`,
		`\\?\Q:\output`, `Q:\parent\..\output`, `Q:\NUL.txt`, `Q:\bad:stream`, `Q:\trailing.`, `Q:\COM¹.txt`, `Q:\bad?name`,
		`Q:\trailing `, `Q:\CONIN$`, `Q:\conout$.txt`, `Q:\CONIN$\output`, `Q:\NUL .txt`,
		"Q:\\bad\x7f.txt", "Q:\\bad\u0085.txt", "Q:\\bad" + string([]byte{0xff})} {
		if _, err := destinationPath(value); err == nil {
			t.Errorf("accepted invalid destination %q", value)
		}
	}
	path, err := destinationPath(`q:/portable//中文 O'Brien`)
	must(t, err)
	if path.native != `q:\portable\中文 O'Brien` || path.parent != `q:\portable` || path.leaf != "中文 O'Brien" {
		t.Fatalf("incorrect destination: %+v", path)
	}
	for _, name := range []string{".hidden", " leading name.txt", "cafe\u0301.txt", "sample-😀.txt", "COM10.txt"} {
		path, err := destinationPath(`Q:\parent\` + name)
		must(t, err)
		if path.leaf != name || path.native != `Q:\parent\`+name {
			t.Fatalf("destination filename changed: %q => %+v", name, path)
		}
	}
}

func TestSourceValidation(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	must(t, os.Mkdir(source, 0700))
	writeFile(t, filepath.Join(source, "notes.txt"), "content")
	_, err := sourcePath(source)
	must(t, err)
	alias := filepath.Join(filepath.Dir(source), "alias")
	must(t, os.Symlink(source, alias))
	must(t, os.Mkdir(filepath.Join(source, "nested"), 0700))
	for _, path := range []string{"relative", source + "/..", source + "/missing", source + "/notes.txt", alias, filepath.Join(alias, "nested")} {
		if _, err := sourcePath(path); err == nil {
			t.Errorf("accepted invalid source %q", path)
		}
	}
	for _, name := range []string{"NUL.txt", "bad:name", "trailing.", "COM1", "COM¹.txt", string([]byte{0xff}),
		"trailing ", "CONIN$", "conout$.txt", "NUL .txt", "bad\x7f.txt", "bad\u0085.txt"} {
		path := filepath.Join(source, name)
		writeFile(t, path, "invalid")
		if err := inspectSource(source); err == nil {
			t.Errorf("accepted invalid source name %q", name)
		}
		must(t, os.Remove(path))
	}
	for _, name := range []string{".hidden", " leading name.txt", "中文 O'Brien.txt", "cafe\u0301.txt", "sample-😀.txt", "COM10.txt"} {
		path := filepath.Join(source, name)
		writeFile(t, path, "unchanged")
		must(t, inspectSource(source))
		if readFile(t, path) != "unchanged" {
			t.Fatalf("source modified during validation: %q", name)
		}
		must(t, os.Remove(path))
	}
	link := filepath.Join(source, "unsupported")
	must(t, os.Symlink("notes.txt", link))
	if err := inspectSource(source); err == nil {
		t.Fatal("accepted source symlink")
	}
	must(t, os.Remove(link))
	must(t, syscall.Mkfifo(link, 0600))
	if err := inspectSource(source); err == nil {
		t.Fatal("accepted FIFO")
	}
	must(t, os.Remove(link))
	for _, names := range [][2]string{{"A.js", "a.js"}, {"K.txt", "K.txt"}, {"σ.txt", "ς.txt"}} {
		for _, name := range names {
			writeFile(t, filepath.Join(source, name), "collision")
		}
		if err := inspectSource(source); err == nil {
			t.Errorf("accepted case collision %q", names)
		}
		for _, name := range names {
			must(t, os.Remove(filepath.Join(source, name)))
		}
	}
	t.Setenv("TMPDIR", source)
	if _, err := sourcePath(source); err == nil {
		t.Fatal("accepted an archive location inside the source")
	}
}
