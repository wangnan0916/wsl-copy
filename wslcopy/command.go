package wslcopy

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Native checks are read-only and precede any junction-resolving path conversion.
//
//go:embed scripts/check-destination.ps1
var destinationScript string

type commandRunner struct {
	ctx    context.Context
	stderr io.Writer
}

func (r commandRunner) run(stdin io.Reader, env []string, name string, args ...string) ([]byte, error) {
	quoted := make([]string, 0, len(args)+1)
	for _, arg := range append([]string{name}, args...) {
		quoted = append(quoted, strconv.Quote(arg))
	}
	fmt.Fprintln(r.stderr, "$", strings.Join(quoted, " "))
	cmd := exec.CommandContext(r.ctx, name, args...)
	cmd.Stdin, cmd.Env = stdin, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	diagnostic := strings.TrimPrefix(strings.ToValidUTF8(stderr.String(), "\uFFFD"), "\uFEFF")
	if diagnostic != "" {
		fmt.Fprint(r.stderr, diagnostic)
	}
	if err != nil {
		if contextErr := r.ctx.Err(); contextErr != nil {
			return nil, fmt.Errorf("%s: %w", name, contextErr)
		}
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return nil, fmt.Errorf("%s failed (%d): %s", filepath.Base(name), exit.ExitCode(), strings.TrimSpace(diagnostic))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return stdout.Bytes(), nil
}

func findTool(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("required executable not found: %s; add it to PATH or supply an explicit tool path", name)
	}
	return path, nil
}

func (r commandRunner) convertPath(path, direction string) (string, error) {
	output, err := r.run(nil, nil, "wslpath", direction, path)
	return strings.TrimRight(string(output), "\r\n"), err
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (r commandRunner) checkDestination(target windowsPath, powershell string) error {
	parents := make([]string, 0, len(target.ancestors))
	for _, parent := range target.ancestors {
		parents = append(parents, psQuote(`\\?\`+parent))
	}
	script := "$parents = @(" + strings.Join(parents, ", ") + "); $p = " + psQuote(`\\?\`+target.native) + ";\n" + destinationScript
	_, err := r.run(nil, nil, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	return err
}

// The helper owns only its temporary archive, never the destination directory.
func (r commandRunner) copyArchive(source, nativeTarget, windowsTar string) error {
	archive, err := os.CreateTemp("", "wsl-copy-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TAR_OPTIONS=") {
			env = append(env, entry)
		}
	}
	if _, err := r.run(nil, env, "tar", "--format=posix", "--hard-dereference", "-C", source, "-cf", archive.Name(), "."); err != nil {
		return err
	}
	// GNU tar writes through a separate descriptor; this handle stays at offset zero.
	_, err = r.run(archive, nil, windowsTar, "-xmf", "-", "-C", nativeTarget, "--no-same-owner", "--no-same-permissions")
	return err
}
