# claude-statusline dev commands

set minimum-version := '1.55.0'
set script-interpreter := ['bash', '-euo', 'pipefail']

binary := if os_family() == 'windows' { 'claude-statusline.exe' } else { 'claude-statusline' }

# list recipes
default:
    @just --list

# build the binary in the repo root
build:
    go build -o {{ binary }} .

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

# render against a sample payload from this checkout
[script]
render: build
    printf '%s' '{"workspace":{"current_dir":"'"$PWD"'"},"model":{"id":"claude-opus-4-7"},"context_window":{"total_input_tokens":12345,"context_window_size":200000}}' | ./{{ binary }}

# go install into GOPATH/bin, then wire ~/.claude/settings.json to it
install-local:
    go install .
    "$(go env GOPATH)/bin/{{ binary }}" install

# remove the settings.json entry
uninstall-local:
    "$(go env GOPATH)/bin/{{ binary }}" uninstall

# startup cost under the Git Bash wrapper Claude Code uses (needs hyperfine)
[script]
bench: build
    payload="$(mktemp)"
    printf '%s' '{"workspace":{"current_dir":"'"$PWD"'"},"model":{"id":"claude-opus-4-7"},"context_window":{"used_percentage":42}}' > "$payload"
    hyperfine --warmup 3 --runs 20 -N --input "$payload" \
      "bash -c ./{{ binary }}" \
      "bash -c true"
    rm -f "$payload"
