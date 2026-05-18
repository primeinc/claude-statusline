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

# Verify the EXACT current package.json version is reachable on npmjs.org.
# Hits the registry HTTP API directly (curl), bypassing npm's _cacache which
# can serve a pre-publish manifest for minutes after a successful publish.
verify-version:
    #!/usr/bin/env bash
    set -euo pipefail
    V=$(node -p "require('./package.json').version")
    URL="https://registry.npmjs.org/@primeinc%2Fclaude-statusline"
    echo "Looking for @primeinc/claude-statusline@${V} at ${URL}…"
    for i in 1 2 3 4 5; do
      LIVE=$(curl -sf "$URL" | node -e "
        const j = JSON.parse(require('fs').readFileSync(0,'utf8'));
        process.exit(j.versions && j.versions['${V}'] ? 0 : 1);
      " && echo yes || echo no)
      if [ "$LIVE" = "yes" ]; then
        echo "OK: ${V} is live on npmjs.org"
        # Bust npm's stale manifest cache so the next install can see it.
        rm -rf "$(npm config get cache)/_cacache" "$(npm config get cache)/_npx" 2>/dev/null || true
        echo "Cleaned local npm _cacache and _npx so installs see the new version."
        exit 0
      fi
      echo "  attempt ${i}/5: not in registry response yet, waiting 6s…"
      sleep 6
    done
    echo "ERROR: ${V} not found on npmjs.org after 30s."
    echo "  The publish output said success but the canonical registry"
    echo "  doesn't have this version. Investigate the npm publish output."
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
