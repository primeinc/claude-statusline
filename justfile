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

# install the local checkout into ~/.claude/statusline.js + wire settings.json
install-local:
    node cli.js install

# uninstall (restores statusLineBackup if present)
uninstall-local:
    node cli.js uninstall

# end-to-end: test, pack-check, bump, publish, verify
release: test pack-check bump publish verify
    @echo "Released. Run: npx @primeinc/claude-statusline install"
