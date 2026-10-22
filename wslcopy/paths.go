package wslcopy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/muonsoft/validation/validate"
)

func checkDirectory(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("expected a directory without symlinks: %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sourcePath(value string) (string, error) {
	if !filepath.IsAbs(value) || slices.Contains(strings.Split(value, "/"), "..") {
		return "", errors.New("pass an absolute WSL source directory without .. components")
	}
	source := filepath.Clean(value)
	if err := checkDirectory(source); err != nil {
		return "", err
	}
	temp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("temporary archive location: %w", err)
	}
	temp, err = filepath.Abs(temp)
	if err != nil {
		return "", err
	}
	if within(source, temp) {
		return "", errors.New("source contains the temporary archive location; set TMPDIR outside the source")
	}
	return source, nil
}

type windowsPath struct {
	native, parent, leaf string
	ancestors            []string
}

// Parse only the supported drive-letter grammar, not arbitrary Windows namespaces.
func destinationPath(value string) (windowsPath, error) {
	value = strings.ReplaceAll(value, "/", `\`)
	bad := errors.New("pass an absolute Windows drive path below its root, without .. or invalid filename components; UNC/device paths are not supported")
	if len(value) < 3 || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) || value[1:3] != `:\` {
		return windowsPath{}, bad
	}
	root := value[:3]
	var parts []string
	for part := range strings.SplitSeq(value[3:], `\`) {
		if part == "" || part == "." {
			continue
		}
		if part == ".." || validate.FileName(part, validate.WithWindowsFileNameRestrictions()) != nil {
			return windowsPath{}, bad
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return windowsPath{}, bad
	}
	parent := root
	ancestors := []string{root}
	for i := 1; i < len(parts); i++ {
		parent = root + strings.Join(parts[:i], `\`)
		ancestors = append(ancestors, parent)
	}
	return windowsPath{root + strings.Join(parts, `\`), parent, parts[len(parts)-1], ancestors}, nil
}

func uncVolume(path string) string {
	path, ok := strings.CutPrefix(strings.ReplaceAll(path, "/", `\`), `\\`)
	if !ok {
		return ""
	}
	parts := strings.Split(path, `\`)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return `\\` + parts[0] + `\` + parts[1]
}

// A comparison key only: Unicode simple folding never rewrites actual filenames.
func foldName(name string) string {
	return strings.Map(func(r rune) rune {
		key := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			key = min(key, next)
		}
		return key
	}, name)
}

func inspectSource(root string) error {
	stack := []string{root}
	for len(stack) != 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		seen := make(map[string]bool, len(entries))
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(dir, name)
			key := foldName(name)
			if validate.FileName(name, validate.WithWindowsFileNameRestrictions()) != nil || seen[key] {
				return fmt.Errorf("invalid/colliding Windows filename: %s", path)
			}
			seen[key] = true
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				stack = append(stack, path)
			} else if !info.Mode().IsRegular() {
				return fmt.Errorf("only regular files/directories are supported: %s", path)
			}
		}
	}
	return nil
}
