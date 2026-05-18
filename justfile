# claude-statusline dev commands

# default: list recipes
default:
    @just --list

# run the test suite
test:
    node test/run.js

# show what would be packed (dry-run, no upload)
pack-check:
    npm pack --dry-run

# render the statusline against a sample payload
render:
    @echo '{"workspace":{"current_dir":"{{justfile_directory()}}"},"model":{"id":"claude-opus-4-7"},"context_window":{"total_input_tokens":12345,"context_window_size":200000}}' | node statusline.js

# bump patch version (0.1.x -> 0.1.x+1) without git tag
bump:
    npm version patch --no-git-tag-version

# publish to npmjs.org (forced via project .npmrc + --registry as belt+suspenders)
publish:
    npm publish --registry=https://registry.npmjs.org/

# verify the published package is reachable on npmjs.org
verify:
    npm view @primeinc/claude-statusline --registry=https://registry.npmjs.org/

# verify the EXACT current package.json version is on the registry; fails loud
# if it isn't (catches the silent 2FA-not-completed publish failure).
verify-version:
    #!/usr/bin/env bash
    set -euo pipefail
    V=$(node -p "require('./package.json').version")
    echo "Looking for @primeinc/claude-statusline@${V} on npmjs.org…"
    for i in 1 2 3 4 5; do
      if npm view "@primeinc/claude-statusline@${V}" version \
           --prefer-online --registry=https://registry.npmjs.org/ \
           > /dev/null 2>&1; then
        echo "OK: ${V} is live on npmjs.org"
        exit 0
      fi
      echo "  attempt ${i}/5: not yet, waiting 6s for propagation…"
      sleep 6
    done
    echo "ERROR: ${V} not found on npmjs.org after 30s."
    echo "  Most likely cause: 2FA browser auth wasn't completed during 'npm publish'."
    echo "  Re-run \`npm publish\` and complete the browser flow before pressing anything."
    exit 1

# install the local checkout into ~/.claude/statusline.js + wire settings.json
install-local:
    node cli.js install

# uninstall (restores statusLineBackup if present)
uninstall-local:
    node cli.js uninstall

# end-to-end: test, pack-check, bump, publish, verify-version (fails if not live)
release: test pack-check bump publish verify-version
    @V=$(node -p "require('./package.json').version") && \
      echo "Released ${V}. Run: npx @primeinc/claude-statusline@${V} install"
