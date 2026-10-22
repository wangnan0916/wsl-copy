# wsl-copy — WSL to Windows directory copy

**Copy WSL directories to Windows. Explicit or mapped targets, optional directory replacement, no background sync.**

`wsl-copy` is a standalone Linux CLI and reusable Go library for copying folders from Windows Subsystem for Linux (WSL2) to Windows. Transfers use one uncompressed GNU tar archive and one Windows tar extraction process.

Use it to copy build artifacts, front-end bundles or WeChat mini-program output from the WSL filesystem to Windows tools. The source stays unchanged. Existing destinations are refused by default; explicit replacement renames the old directory, copies into the original path, and schedules background retirement after success. It returns when the new copy is ready, without waiting for old-tree checks or deletion. It does not merge files, build projects, watch for changes or control Windows applications.

[Quick start](#quick-start-copy-a-wsl-folder-to-windows) · [Go library](#go-library-api-and-project-layout) · [Safety](#copying-and-replacement-safety) · [Benchmarks](#performance-benchmarks) · [FAQ](#faq)

## Quick start: copy a WSL folder to Windows

### Install in WSL (no Go required)

Run this one command in a **WSL terminal**, not Windows PowerShell:

```bash
curl -fsSL https://raw.githubusercontent.com/wangnan0916/wsl-copy/main/install.sh | sh
```

The [installer](install.sh) detects x86-64 or ARM64, downloads the newest public GitHub release **including prereleases**, verifies the archive's SHA256 checksum, and installs `~/.local/bin/wsl-copy` with license notices in `~/.local/share/wsl-copy/`. It uses curl and standard WSL utilities; no Go, Python or sudo is needed. [v0.1.0](https://github.com/wangnan0916/wsl-copy/releases/tag/v0.1.0) is the initial preview release.

To pin a version or bypass GitHub API rate limits when discovering releases:

```bash
curl -fsSL https://raw.githubusercontent.com/wangnan0916/wsl-copy/main/install.sh | sh -s -- v0.2.0
```

Rerun the installer to upgrade an older release. The development CLI also provides `wsl-copy update` (see below). The installer replaces the executable and license notices only after download, checksum and binary-version checks pass. There is no background updater. The installer does not modify shell configuration, install dependencies or guess Windows tool locations. It prints PATH instructions and warns about missing runtime tools; see [Windows tool paths](#windows-tool-paths). Add the PATH command below to your shell startup file if needed. You can read or download `install.sh` before running it instead of piping remote code into a shell.

Then copy an existing WSL directory to a **new** Windows directory:

```bash
export PATH="$HOME/.local/bin:$PATH"
wsl-copy "$HOME/project/dist" 'D:\Copies\my-build'
```

| WSL architecture | Archive | Validation |
| --- | --- | --- |
| x86-64 (`uname -m`: `x86_64`) | `wsl-copy-linux-amd64.tar.gz` | Tested with native Windows tools |
| ARM64 (`uname -m`: `aarch64`) | `wsl-copy-linux-arm64.tar.gz` | Cross-built only; not tested on ARM64 hardware |

For manual installation, download the matching archive and `SHA256SUMS` from [Releases](https://github.com/wangnan0916/wsl-copy/releases). Each archive contains `wsl-copy`, `LICENSE` and `THIRD_PARTY_NOTICES`. These are Linux binaries, not Windows `.exe` files. See [Windows tool paths](#windows-tool-paths) if dependencies are not on your PATH, and [known Windows tar limitations](#known-windows-tar-limitations) before copying affected names or paths.

### Update command

**Development version:** `v0.2.0` does not yet contain this command. Build from source, or bootstrap once with the one-command installer after a release containing it is published.

```bash
wsl-copy update           # Newest public release, including prereleases
wsl-copy update v0.2.0    # Install a specific published version (including downgrade)
wsl-copy update --help
```

This is an explicit, foreground update, not a background updater. It downloads the official `main/install.sh` over HTTPS, completely and with a 64 KiB limit before execution. Installer redirects are refused, HTTP operations are bounded, and the update has a five-minute overall timeout. The existing installer performs release checksum/version checks and staged executable replacement; dependencies are not installed automatically.

Updates always install to `$HOME/.local/bin/wsl-copy`, with notices under `$HOME/.local/share/wsl-copy`. They do not modify user configuration, copy targets, shell startup files or a custom binary at another location. Update bypasses copy configuration and Windows-tool preflight, and prints installer progress as text rather than copy JSON. Argument errors exit 2; download/install errors exit 1. Cancellation signals the installer and its child processes so cleanup traps can run; cancellation after executable replacement cannot undo that committed update.

### Target mapping and replacement

Mapping, replacement, background retirement and CLI copy locking are available in `v0.2.0` and newer. The original `v0.1.0` remains create-only.

An explicit target takes precedence over mapping. Use `--replace` to replace an existing ordinary directory:

```bash
wsl-copy "$HOME/project/dist" 'D:\Copies\my-build' --replace
```

For repeated copies, create `~/.config/wsl-copy/config.json` (or `$XDG_CONFIG_HOME/wsl-copy/config.json`):

```json
{
  "source_root": "/home/you",
  "windows_root": "D:\\Copies",
  "replace": true
}
```

With that configuration, `wsl-copy /home/you/project/dist` maps to `D:\Copies\project\dist`. Both roots must already exist, and the source must be strictly below `source_root`; missing destination parents below `windows_root` are created only during a real copy, never during dry-run. No user, drive, mount point or distribution is assumed.

You can also pass `--source-root` and `--windows-root` directly. `--config FILE` selects a configuration file; `--config ''` disables configuration. Flags override configuration, including `--replace=false`. Optional `windows_tar` and `powershell` config fields specify WSL executable paths; tool resolution is flag → environment → config → PATH. Unknown config fields and invalid JSON are rejected. Help and version commands do not read configuration. No configuration file is created automatically.

Replacement is a full directory replacement, not a merge. Windows-only files and IDE settings in the old target are not preserved in the new target. The original path is **not usable during copying**; consumers must wait for the result. A new, previously absent target works with or without `--replace`.

### Copy coordination

The CLI uses one nonblocking exclusive lock for all real copies by the same WSL user: `os.UserCacheDir()/wsl-copy/copy.lock` (normally `$HOME/.cache/wsl-copy/copy.lock`). This deliberately serializes even unrelated targets, avoiding same-target and parent/child overlap without path-based lock aliases. A busy call fails immediately with exit 1 and `status: "failed"`; there is no wait queue or automatic retry.

Use `--lock-file /absolute/wsl/path` or configuration `"lock_file": "/absolute/wsl/path"` to share an existing lock with other callers. The path must be outside the source, on the WSL filesystem, with no symlink ancestry; missing parents are created only for real copies. Never delete, replace or relocate a lock file while callers may use it. Explicit `--lock-file ''` disables coordination only when the caller already provides it. Different lock files/users/distributions and external tools are not coordinated.

The lock covers destination preflight, copying, rollback and retirement handoff. It is released on return or process exit, and is not inherited by tar or the detached retirement process. Cleanup therefore never delays admission of the next copy. Dry-run validates without acquiring or creating a lock. This does not lock source producers, Windows applications or retired-tree ancestors; keep builds finished and discarded containers untouched.

### Build from source

Clone [wangnan0916/wsl-copy](https://github.com/wangnan0916/wsl-copy), then build inside WSL with Go 1.27.3 or newer:

```bash
make build
./dist/wsl-copy --version
./dist/wsl-copy "$HOME/project/dist" 'D:\Copies\my-build'
```

Without Make: `CGO_ENABLED=0 go build -trimpath -o dist/wsl-copy ./cmd/wsl-copy`.

The result is a standalone Linux executable. Running it does not require Go or Python, but still requires:

- WSL with Windows executable interoperability.
- GNU `tar` and `wslpath` on the WSL `PATH`.
- Windows `tar.exe` and Windows PowerShell, available through `PATH` or explicit paths.
- A destination volume mounted and accessible from WSL.

Argument parsing uses [pflag](https://github.com/spf13/pflag), and Windows filename checks use [muonsoft/validation](https://github.com/muonsoft/validation). Archive transfer uses the Go standard library and system tools. No username, distribution, drive letter, mount point or mirror directory is hardcoded.

Replace the example paths with your own. The source must be an existing absolute WSL filesystem directory. Without replacement, every existing target is refused, including empty directories and dangling links. Replacement accepts only ordinary directories, not files, symlinks or junctions. An explicit target requires an existing parent; mapped targets may create parents below the configured existing Windows root. Quote Windows paths in your shell.

Single files, Windows-mounted sources, UNC/device destinations, drive roots, `..` components, symbolic-link paths and junction ancestors are not supported.

`--dry-run` validates paths, source entries and lock configuration, but does not scan old descendants or acquire the copy lock, without creating, renaming or deleting directories or running tar. It does not prove extraction compatibility or guarantee that later rename/cleanup operations will succeed. Options may appear before, between or after positional arguments; `--` ends option parsing. Long options use GNU syntax (`--dry-run`, not `-dry-run`). Use `--help` or `-h` for help.

### Windows tool paths

If Windows executables are not on your WSL `PATH`, supply their **WSL executable paths**:

```bash
wsl-copy "$HOME/project/dist" 'D:\Copies\my-build' \
  --windows-tar /path/to/tar.exe \
  --powershell /path/to/powershell.exe
```

Resolution order: explicit options, then `WSL_COPY_WINDOWS_TAR` / `WSL_COPY_POWERSHELL`, then config fields, then `PATH`. Missing dependencies are reported, not installed or searched for in guessed drive locations.

## Go library API and project layout

The public package `github.com/wangnan0916/wsl-copy/wslcopy` (package name `wslcopy`) exposes the copy operation independently of the CLI:

```bash
go get github.com/wangnan0916/wsl-copy/wslcopy@v0.2.0
```

```go
func Copy(ctx context.Context, opts Options) (Result, error)
```

```go
import "github.com/wangnan0916/wsl-copy/wslcopy"

result, err := wslcopy.Copy(ctx, wslcopy.Options{
    Source: source,
    WindowsTarget: target,
})
```

`Options.SourceRoot` / `Options.WindowsRoot` enable mapping when `WindowsTarget` is empty; `Options.Replace` opts into replacement. `Options.DryRun` validates without mutation or tar execution. The library does not load configuration files. Set `Options.LockFile` to one shared absolute WSL file to serialize cooperating library calls; empty leaves coordination to the caller. Lock contention returns an error wrapping `wslcopy.ErrBusy` without changing the target.

Inspect `Result` on both success and failure. `DestinationCreated` is historical ownership: it stays true even when failed new output is moved aside and the previous directory is restored. `RetainedWindowsPath` locates that failed output; `DestinationRestored` reports successful rollback. `PreviousWindowsPath` is the last-known old-data location on copy or retirement-launch failure. A successful launch clears that field; it does **not** mean deletion has completed. `CleanupError` reports only failure to prepare/start retirement, without rolling back the completed copy. Later worker failures stay in its `cleanup.log`, not in the already-returned result. Empty recovery fields are omitted by the CLI.

`Options.WindowsTar` and `Options.PowerShell` accept WSL executable paths; empty values use `PATH`. Only the CLI reads the `WSL_COPY_*` environment defaults. The library accepts the caller's context, installs no signal handlers, and does not write JSON, select exit codes or use process stdout/stderr directly. Set `Options.Diagnostics` to an `io.Writer` to receive command logs and subprocess diagnostics; nil discards them.

```text
.
├── .github/workflows/release.yml  # CI builds and tag-triggered releases
├── cmd/wsl-copy/
│   ├── main.go                    # Arguments, signals and CLI output
│   ├── main_test.go               # Argument, help and version tests
│   ├── config.go, config_test.go   # Optional user configuration
│   ├── update_linux.go, update_test.go # Bounded installer download and explicit updates
│   └── install_test.go            # Installer checks with isolated downloads
├── wslcopy/
│   ├── copy.go, copy_test.go       # Public API and copy orchestration
│   ├── paths.go, paths_test.go     # Path, filename and source-tree validation
│   ├── mapping.go, mapping_test.go # Source-relative destination mapping
│   ├── lock_linux.go, lock_test.go # Shared nonblocking copy lock and process-exit tests
│   ├── replace.go, replace_test.go # Retirement ownership, launch and rollback
│   ├── background_linux.go        # Detached child lifetime and process reaping
│   ├── background_other.go        # Non-Linux build compatibility
│   ├── background_test.go         # Nonblocking retirement and cancellation isolation
│   ├── background_integration_test.go # Native worker survival after CLI exit
│   ├── command.go, command_test.go # Subprocesses, Windows checks and archive transfer
│   ├── fixtures_test.go           # Shared test fixtures and assertions
│   ├── cli_test.go                # Compiled CLI/library boundary tests
│   ├── integration_test.go        # Opt-in native Windows tests
│   ├── replace_integration_test.go # Native replacement and mapping checks
│   └── scripts/                   # Embedded path/tree checks and old-tree retirement
├── install.sh                    # User-local release installer
├── README.md
├── LICENSE, THIRD_PARTY_NOTICES
├── Makefile
└── go.mod, go.sum
```

The PowerShell scripts are embedded at build time, not loaded from the working directory. No separate script file is needed at runtime; retain the accompanying license notices when redistributing the binary.

## Copying and replacement safety

- Read-only Windows checks reject reparse points/devices before path conversion. Existing destinations are refused unless replacement is enabled. Replacement validates the root and its ancestors, but does not scan old descendants on the copy's critical path. A nested old-tree junction can therefore be retired by rename, but its background deletion is refused. The converted parent is mapped back to Windows for validation. An exclusive `mkdir` prevents entering a destination created by another caller.
- The source is checked once for regular files/directories and case-insensitive collisions using Unicode simple folding. Source names and destination components use conservative Windows filename validation on every host OS: invalid UTF-8, control characters, reserved device names (including `CONIN$` and `CONOUT$`), and trailing dots/spaces are rejected. This does not normalize filenames or prove extraction compatibility.
- GNU tar creates one uncompressed POSIX archive in WSL temporary storage. `TAR_OPTIONS` is removed from the packing environment. The archive must be outside the source; set `TMPDIR` to another writable directory if necessary.
- Archive bytes go directly through binary stdin to Windows tar, not through a PowerShell text pipeline. Hidden files and empty directories are included. Hard links become independent content. `-m` prevents restoration of source modification times; Linux ownership, permissions, ACLs and executable bits are not guaranteed.
- The temporary archive is removed on normal exit or error. Failed new output is **never deleted**. Without a previous directory it stays at the target. During replacement, an ordinary copy failure moves new output aside and attempts to restore the previous directory without overwriting another entry. A failed rollback retains both trees for recovery.
- Replacement reserves a unique sibling `.wsl-copy-<id>` directory and renames the old target into its `previous` child **before packing/copying**. After successful extraction, one detached native PowerShell process revalidates the entire owned retired tree, then deletes entries bottom-up without following reparse points or using recursive directory deletion. **Copy/CLI return after launching it, without waiting for checks or deletion.** The worker has independent lifetime, null stdio and no dependency on the caller's context, terminal or output pipes; it continues after normal CLI exit. Only this invocation's retired tree can be removed; failed `partial` output is retained. No queue, persistent service or automatic retry is involved.
- Cancellation during copying conservatively retains the old and new trees in their last-known locations, rather than restoring old data into a path that a Windows writer may still be using. There is no automatic crash recovery or retry. Inspect reported recovery paths before a later replacement attempt.
- Finish builds and writes first. Keep the source and destination parent stable until return. After handoff, never modify, adopt or reuse a retirement container or replace its ancestors; these are discarded paths, not current build outputs. Copying is not an atomic publication; consumers wait for `copied`, then use `windows_path` immediately without polling retirement. The shared copy lock prevents cooperating foreground copies from overlapping; it does not defend against external writes, concurrent junction swaps or callers using different/no locks. Newly created mapping parents can remain after a failure.

A successful worker deletes its container and `cleanup.log`. On failure it retains the remaining old data and a best-effort error log at `.wsl-copy-<id>/cleanup.log`; the old tree may be partly deleted and must not be restored automatically. Failed startup, forced termination or WSL shutdown may leave a queued/running log or no final diagnostic. This is operator evidence for troubleshooting, not a completion API; normal callers must not wait or poll it.

There are no content hashes, manifests, source rechecks, automatic retries, persistent cleanup services, attribute/ACL changes or application-control operations in the copy operation.

## CLI JSON output and exit codes

During normal execution, the CLI writes one JSON object to stdout; diagnostics and subprocess commands go to stderr.

| Exit code | `status` | Meaning |
| --- | --- | --- |
| 0 | `copied` | Packing and extraction succeeded. |
| 0 | `dry_run` | Validation succeeded; nothing was copied. |
| 1 | `failed` | A runtime failure. |
| 2 | — | Invalid CLI arguments; diagnostic text instead of JSON. |

The result contains `status` and `elapsed_seconds`, plus `source_path` and `windows_path` once validated. Failures include `error`; `destination_retained: true` appears when this invocation created new output and then failed. After rollback, `retained_windows_path` identifies that output and `destination_restored: true` means the previous directory is back at `windows_path`. `previous_windows_path` reports the last-known old-data location on copy or retirement-launch failure; normal background handoff omits it.

If retirement cannot be prepared/started, `cleanup_error` accompanies `status: "copied"` and exit 0: the new target is usable and old data is retained. Never roll back from that warning. Once the worker is launched, later cleanup failures cannot change stdout, stderr, the exit code or the returned `Result`; they are recorded in its private log when possible. Successful handoff and create-only calls both keep the normal copy JSON field set.

`copied` means successful packing and extraction, **not content or filename round-trip verification** and not a guarantee that old-tree retirement succeeded. `--help`, `--version` and `update` print text instead of JSON. Forced termination can leave a partial destination and may not produce JSON; there is no crash recovery.

## Known Windows tar limitations

Native tests against Windows tar 3.8.9 identified these unresolved cases:

| Case | Observed behavior |
| --- | --- |
| Emoji filenames | Extraction fails with an unreadable-filename error. |
| Decomposed Unicode filenames (NFD) | Names can be normalized to NFC without a nonzero exit status. |
| Long destination paths | tar cannot change into some long `-C` paths, even with the tested extended-path prefix. |

These are tool compatibility gaps, not Windows filesystem limitations. The CLI does not rename, case-convert or normalize filenames. Avoid affected names and paths when exact preservation is required, especially before replacement: a successful tar exit can trigger retirement even if tar silently changed a filename.

## Performance benchmarks

Local WSL2-to-Windows measurements on an Intel Core Ultra 7 265K, using Go 1.27.3 and Windows bsdtar 3.8.9. Sources were on WSL ext4; every run copied to a fresh directory on the Windows C: drive. These measurements are create-only copies; they do not include retirement of a previous destination.

| Workload | File data | Median copy time | Runs |
| --- | ---: | ---: | ---: |
| One large file | 512 MiB | 1.16 s | 3 |
| 10,000 files, 4 KiB each | 39.1 MiB | 2.29 s | 3 |
| Real mini-program build: 8,309 files, 2,552 subdirectories | 45.3 MiB | 2.68 s | 5 |

Times include CLI startup, validation, packing and extraction, but exclude fixture generation, verification and cleanup. The real build's first copy took 3.34 s; subsequent copies took 2.63–2.73 s. Its dry-run validation took about 0.22 s.

These are illustrative results, not speed guarantees. Caches were not flushed, synthetic sources were freshly written, and `/tmp` was a memory-backed tmpfs used for tar staging. No forced disk flush was measured. File count, directory structure, storage and caching can materially change performance.

## FAQ

### Does wsl-copy synchronize files or overwrite an existing Windows folder?

There is no background synchronization or file merging. By default, every existing destination is refused. `--replace` (or `replace: true` in configuration) explicitly enables full directory replacement: rename old, copy new, then retire old in the background. Windows-only files do not carry over.

### Can I copy a single file or copy from Windows to WSL?

No. The source must be a directory on the WSL filesystem, and the destination is a Windows directory. Individual-file and Windows-to-WSL copying are outside the supported scope.

### Do I need Go or Python to run the CLI?

No. Go is needed to build the binary, not to run it. The Linux executable runs inside WSL and uses GNU tar, `wslpath`, Windows tar and PowerShell. See [Windows tool paths](#windows-tool-paths) if those executables are not on your PATH.

## Development and tests

```bash
go test ./...
go vet ./...
go fix -diff ./...
```

Follow the [Modern Go Guidelines](https://github.com/JetBrains/go-modern-guidelines) for the version declared in `go.mod`, without changing existing copy or JSON contracts. `go fix -diff` is read-only and exits nonzero if modernization suggestions remain.

Tests are organized by contract, not repeated across every layer:

- `wslcopy/copy_test.go` covers safety, dry-run validation, caller cancellation and exclusive destination creation.
- `wslcopy/replace_test.go` covers replacement ordering, rollback, collisions and cleanup warnings. `mapping_test.go` covers source-relative mapping and its boundaries. `cmd/wsl-copy/config_test.go` covers strict configuration parsing.
- `wslcopy/command_test.go` exercises transfers through the public `Copy` API: content, empty directories, packing/extraction failures, ownership and archive cleanup. `paths_test.go` holds path and filename validation tables.
- `cmd/wsl-copy/main_test.go` covers arguments, help and version output. `wslcopy/cli_test.go` builds the real CLI and checks only its JSON, diagnostics and exit codes from outside the checkout, including omitted fields and invalid-UTF-8 replacement.
- `cmd/wsl-copy/install_test.go` exercises the shell installer from stdin using local release fixtures and an isolated HOME: version/architecture selection, checksum and download failures, and temporary-file cleanup. It does not install into the developer's actual home or use the network.
- `wslcopy/integration_test.go` and `replace_integration_test.go` cover native Windows paths, junctions, tar compatibility, repeated replacement, rollback, read-only cleanup failure and mapped-parent creation.

Shared fixtures live in `wslcopy/fixtures_test.go`. Tree assertions compare exact entry sets, types and contents; the local tar shim forwards extraction arguments after translating the destination path. Local command fixtures and GNU tar substitutions are not proof of Windows tar compatibility. Add tests for distinct contracts or reproduced regressions, rather than duplicating filesystem assertions in the CLI layer.

Run native tests explicitly, setting the tool-path environment variables above if needed:

```bash
make test-windows
# Equivalent: WSL_COPY_WINDOWS_TESTS=1 go test -count=1 -run '^TestWindows$' -v ./wslcopy
```

Native tests use only randomly named fixtures in the current Windows user's temporary directory. Compatibility skips require the recognized tar version and specific observed failure; preparation errors and unrelated failures still fail the test. A runtime that preserves the tested names and paths passes those tests normally.

Keep copying, mapping and replacement safety in the public `wslcopy/` package, with tests beside the code they exercise. Keep argument parsing, configuration, environment defaults, signals, timing, JSON and exit codes in `cmd/wsl-copy`. Use pflag for CLI argument parsing and muonsoft/validation for filename checks, and prefer the standard library and system tools elsewhere. Keep archive transfer separate from destination policy. Deletion is limited to an invocation-owned retired tree after copy success; never remove failed new output or broaden deletion to arbitrary paths. Do not add parallel extraction, archive splitting, services or workflow frameworks. New copy policies require an explicit safety contract. Write documentation, help and comments in English; multilingual test filenames are intentional. Development automation must not install dependencies or commands, change global configuration, or publish releases without explicit approval.

## Automated builds and releases

[Build and release workflow](https://github.com/wangnan0916/wsl-copy/actions/workflows/release.yml) runs on `main` pushes, pull requests and manual dispatches. It installs the Go version from `go.mod`, checks formatting, runs Linux tests with the race detector, vets code, verifies dependencies, checks modernization, and packages Linux amd64/arm64 archives with SHA256SUMS. CI artifacts are retained for seven days.

Only **pushing a new version tag** publishes a GitHub Release. Branch/PR runs and manual dispatches build but do not publish. Actions are pinned to commit SHAs; the build job is read-only, and only the tag-gated publish job has release-write permission through the built-in `GITHUB_TOKEN`. No personal access token is needed.

After reviewing the commit and running native Windows tests locally, release a new version:

```bash
git tag -a v0.2.1 -m "Release v0.2.1"
git push origin v0.2.1
```

Tags must use `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-PRERELEASE`. Tags starting with `v0.` and tags with a prerelease suffix are marked as GitHub prereleases. The workflow uploads the two archives and checksum file to a draft before publishing it. It never overwrites an existing release or its assets; if an upload fails, inspect the draft before any manual recovery. Do not move published tags or replace published assets.

Hosted Linux CI cannot replace WSL/Windows-native tests; ARM64 remains cross-build-only. The existing `v0.1.0` release was built locally. Automation applies to new tags containing the workflow; it does not rebuild or change that release.

### Local release builds

```bash
make release VERSION=v0.2.0
```

This builds `dist/wsl-copy-linux-amd64` and `dist/wsl-copy-linux-arm64` with CGO disabled, then packages each as `dist/wsl-copy-linux-<arch>.tar.gz` with `LICENSE` and `THIRD_PARTY_NOTICES`. `dist/SHA256SUMS` covers the two release archives. It does not install commands or publish anything.

Publish only the two archives and `SHA256SUMS` as release assets, not the entire `dist/` directory: local benchmark reports and packaging intermediates are not distribution files. Build releases from a clean, tagged commit and state which architectures were actually tested.

## License

[MIT](LICENSE). Bundled dependencies and the Go runtime retain their respective licenses; see [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
