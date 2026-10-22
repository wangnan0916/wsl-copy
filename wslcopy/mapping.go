package wslcopy

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/muonsoft/validation/validate"
)

// An explicit target always wins. Mapping may create parents below an existing Windows root.
func resolveDestination(source string, opts Options) (windowsPath, string, error) {
	if opts.WindowsTarget != "" {
		target, err := destinationPath(opts.WindowsTarget)
		return target, "", err
	}
	bad := errors.New("provide WINDOWS_TARGET or both source_root and windows_root; mapped sources must be strictly below source_root")
	if !filepath.IsAbs(opts.SourceRoot) || slices.Contains(strings.Split(opts.SourceRoot, "/"), "..") || opts.WindowsRoot == "" {
		return windowsPath{}, "", bad
	}
	root := filepath.Clean(opts.SourceRoot)
	if err := checkDirectory(root, false); err != nil {
		return windowsPath{}, "", err
	}
	rel, err := filepath.Rel(root, source)
	if err != nil || rel == "." || !within(root, source) {
		return windowsPath{}, "", bad
	}
	// Backslashes in Linux path components must be rejected, not reinterpreted as separators.
	for name := range strings.SplitSeq(rel, "/") {
		if err := validate.FileName(name, validate.WithWindowsFileNameRestrictions()); err != nil {
			return windowsPath{}, "", err
		}
	}
	windowsRoot, err := destinationPath(opts.WindowsRoot)
	if err != nil {
		return windowsPath{}, "", err
	}
	target, err := destinationPath(windowsRoot.native + `\` + strings.ReplaceAll(rel, "/", `\`))
	return target, windowsRoot.native, err
}
