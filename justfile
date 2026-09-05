# claude-statusline dev commands

set minimum-version := '1.55.0'
set export
set script-interpreter := ['bash', '-euo', 'pipefail']

binary := if os_family() == 'windows' { 'claude-statusline.exe' } else { 'claude-statusline' }

# list recipes
default:
    @just --list

# build the binary in the repo root
build:
    go build -o "$binary" .

# run every test
test:
    go test ./...

# vet + golangci-lint (config: .golangci.yml)
lint:
    go vet ./...
    golangci-lint run

# format in place (gofmt + goimports via golangci-lint)
fmt:
    golangci-lint fmt

# fail if formatting or lint would change anything
check: test lint
    golangci-lint fmt --diff

# parse every real .git/config under a glob with the same reader the binary uses
real-configs glob:
    CLAUDE_STATUSLINE_CONFIG_GLOB="$glob" go test ./internal/gitinfo -run RealConfigs -v

# render against a sample payload from this checkout
[script]
render: build
    # native binaries need C:/... on Windows; $PWD is /c/... under Git Bash
    here="$(cygpath -m "$PWD" 2>&1 || printf '%s' "$PWD")"
    printf '%s' '{"workspace":{"current_dir":"'"$here"'"},"model":{"id":"claude-opus-4-7"},"context_window":{"total_input_tokens":12345,"context_window_size":200000}}' | "./$binary"

# go install into GOPATH/bin, then wire ~/.claude/settings.json to it
install-local:
    go install .
    "$(go env GOPATH)/bin/$binary" install

# remove the settings.json entry
uninstall-local:
    "$(go env GOPATH)/bin/$binary" uninstall

# render cost under the Git Bash wrapper Claude Code uses (needs hyperfine)
[script]
bench: build
    here="$(cygpath -m "$PWD" 2>&1 || printf '%s' "$PWD")"
    # Claude Code runs the command through the Git Bash launcher named by
    # CLAUDE_CODE_GIT_BASH_PATH (C:\Git\bin\bash.exe, which itself spawns
    # usr\bin\bash.exe). Use that exact binary; a bare "bash" under
    # hyperfine -N resolves System32\bash.exe (WSL) on Windows.
    sh="${CLAUDE_CODE_GIT_BASH_PATH:-$(command -v bash)}"
    sh="$(cygpath -m "$sh" 2>&1 || printf '%s' "$sh")"
    payload="$(mktemp)"
    payload_native="$(cygpath -m "$payload" 2>&1 || printf '%s' "$payload")"
    printf '%s' '{"workspace":{"current_dir":"'"$here"'"},"model":{"id":"claude-opus-4-7"},"context_window":{"used_percentage":42}}' > "$payload"
    FORCE_HYPERLINK=1 hyperfine --warmup 5 --runs 50 -N --input "$payload_native" \
      "$sh -c '$here/$binary'" \
      "$sh -c true"
    rm -f "$payload"
