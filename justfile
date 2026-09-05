# claude-statusline dev commands

set minimum-version := '1.55.0'
set export
set script-interpreter := ['bash', '-euo', 'pipefail']

binary := if os_family() == 'windows' { 'claude-statusline.exe' } else { 'claude-statusline' }

# repo root as a native path (C:/... on Windows), for the binary and hyperfine
root := replace(justfile_directory(), '\', '/')

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
    printf '%s' '{"workspace":{"current_dir":"'"$root"'"},"model":{"id":"claude-opus-4-7"},"context_window":{"total_input_tokens":12345,"context_window_size":200000}}' | "$root/$binary"

# go install into GOPATH/bin, then wire ~/.claude/settings.json to it
install-local:
    go install .
    "$(go env GOPATH)/bin/$binary" install

# remove the settings.json entry
uninstall-local:
    "$(go env GOPATH)/bin/$binary" uninstall

# render cost through the shell Claude Code spawns (needs hyperfine)
[script]
bench: build
    # On Windows Claude Code spawns the launcher named by CLAUDE_CODE_GIT_BASH_PATH
    # (C:\Git\bin\bash.exe, which spawns usr\bin\bash.exe). Use that exact
    # binary: a bare "bash" under hyperfine -N resolves System32\bash.exe (WSL).
    # Elsewhere plain bash is the shell.
    sh="${CLAUDE_CODE_GIT_BASH_PATH:-bash}"
    sh="${sh//\\//}"
    payload="$root/.bench-payload.json"
    printf '%s' '{"workspace":{"current_dir":"'"$root"'"},"model":{"id":"claude-opus-4-7"},"context_window":{"used_percentage":42}}' > "$payload"
    FORCE_HYPERLINK=1 hyperfine --warmup 5 --runs 50 -N --input "$payload" \
      "$sh -c '$root/$binary'" \
      "$sh -c true"
    rm -f "$payload"
