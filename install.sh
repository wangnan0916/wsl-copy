#!/bin/sh
set -eu

fail() {
    printf 'wsl-copy install: %s\n' "$*" >&2
    exit 1
}

fetch() (
    url=$1 output=$2 limit=$3
    set -- --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 120 \
        --speed-limit 1 --speed-time 30 --max-redirs 5 -fsSL
    headers=$(curl "$@" --head "$url")
    printf '%s\n' "$headers" | awk -v limit="$limit" '
        tolower($1) == "content-length:" && $2 + 0 > limit { exit 1 }
    ' || fail 'Download exceeds the size limit'
    # POSIX sh uses 512-byte blocks; also bound writes on older curl versions.
    ulimit -f "$((limit / 512))"
    curl "$@" --max-filesize "$limit" --output "$output" "$url"
)

main() {
    case "${1-}" in
        -h|--help)
            printf 'Usage: sh install.sh [VERSION]\nDefaults to the newest public GitHub release, including prereleases.\n'
            return
            ;;
    esac
    [ "$#" -le 1 ] || fail 'Expected at most one version argument'
    for tool in curl uname awk grep tar sha256sum mktemp install mkdir mv rm; do
        command -v "$tool" >/dev/null 2>&1 || fail "Required command not found: $tool"
    done
    [ "$(uname -s)" = Linux ] || fail 'Run this installer inside WSL, not Windows PowerShell or macOS'
    case "$(uname -m)" in
        x86_64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) fail 'Unsupported architecture; expected x86-64 or ARM64' ;;
    esac
    case "${HOME-}" in
        /*) ;;
        *) fail 'HOME must be an absolute directory path' ;;
    esac

    work=$(mktemp -d)
    staged=
    trap 'rm -rf -- "$work"; [ -z "$staged" ] || rm -f -- "$staged"' 0
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    cd "$work"
    work=$(pwd -P)
    version=${1-}
    if [ -z "$version" ]; then
        fetch 'https://api.github.com/repos/wangnan0916/wsl-copy/releases?per_page=1' release.json 1048576
        # Read only tag_name from GitHub's response; validate it before constructing URLs.
        version=$(awk -F '"' '
            { for (i = 1; i < NF; i++)
                if ($i == "tag_name" && $(i+1) ~ /^[[:space:]]*:[[:space:]]*$/) {
                    print $(i+2); exit
                }
            }' release.json)
    fi
    case "$version" in
        ''|*[!a-zA-Z0-9.-]*) fail 'No valid release version found; try an explicit version such as v0.1.0' ;;
    esac
    printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$' || fail 'Invalid release version'

    archive="wsl-copy-linux-$arch.tar.gz"
    base="https://github.com/wangnan0916/wsl-copy/releases/download/$version"
    printf 'Installing wsl-copy %s (%s)...\n' "$version" "$arch"
    fetch "$base/$archive" "$archive" 33554432
    fetch "$base/SHA256SUMS" SHA256SUMS 1048576
    awk -v name="$archive" '$2 == name { print }' SHA256SUMS > selected-checksum
    sha256sum --check --strict selected-checksum || fail 'Checksum verification failed; nothing was installed'
    unset TAR_OPTIONS
    tar -xzf "$archive" --no-same-owner --no-same-permissions -- wsl-copy LICENSE THIRD_PARTY_NOTICES
    for file in wsl-copy LICENSE THIRD_PARTY_NOTICES; do
        [ -f "$file" ] && [ ! -L "$file" ] || fail "Expected a regular file in the release: $file"
    done
    [ "$(./wsl-copy --version)" = "wsl-copy $version" ] || fail 'Unexpected binary version; nothing was installed'

    mkdir -p "$HOME/.local/bin"
    staged=$(mktemp "$HOME/.local/bin/.wsl-copy.XXXXXX")
    install -m755 wsl-copy "$staged"
    install -Dm644 LICENSE "$HOME/.local/share/wsl-copy/LICENSE"
    install -Dm644 THIRD_PARTY_NOTICES "$HOME/.local/share/wsl-copy/THIRD_PARTY_NOTICES"
    mv -fT -- "$staged" "$HOME/.local/bin/wsl-copy"
    staged=
    printf 'Installed %s\n' "$HOME/.local/bin/wsl-copy"
    case ":$PATH:" in
        *":$HOME/.local/bin:"*) ;;
        *) printf '%s\n' 'To use it by name, add this to your shell configuration:' '  export PATH="$HOME/.local/bin:$PATH"' ;;
    esac
    for tool in wslpath "${WSL_COPY_WINDOWS_TAR:-tar.exe}" "${WSL_COPY_POWERSHELL:-powershell.exe}"; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            printf 'Runtime dependency not found: %s. See the README Windows tool paths section.\n' "$tool" >&2
        fi
    done
}

# Keep execution last so an interrupted curl | sh cannot execute a partial function body.
main "$@"
