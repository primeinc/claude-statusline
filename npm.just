# npm distribution: @primeinc/claude-statusline launcher plus one binary package per platform

set export
set script-interpreter := ['bash', '-euo', 'pipefail']

# repo root as a native path (C:/... on Windows), for npm and node
root := replace(justfile_directory(), '\', '/')

# cross-compile every platform binary into its npm package
[script]
build:
    for t in windows/amd64/win32-x64 windows/arm64/win32-arm64 darwin/amd64/darwin-x64 \
             darwin/arm64/darwin-arm64 linux/amd64/linux-x64 linux/arm64/linux-arm64; do
      IFS=/ read -r goos goarch target <<< "$t"
      ext=""; [ "$goos" = windows ] && ext=".exe"
      out="$root/npm/claude-statusline-$target/claude-statusline$ext"
      CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags='-s -w' -o "$out" .
      echo "built $out"
    done
    cp README.md LICENSE npm/claude-statusline/

# set VERSION on every package and on the launcher's optionalDependencies
[script]
version VERSION:
    for d in npm/*/; do
      (cd "$d" && npm pkg set version="$VERSION")
    done
    cd npm/claude-statusline
    for d in ../claude-statusline-*/; do
      npm pkg set "optionalDependencies.@primeinc/$(basename "$d")=$VERSION"
    done
    npm pkg get version optionalDependencies

# pack every package into dist/npm; no upload
[script]
pack: build
    rm -rf dist/npm
    mkdir -p dist/npm
    for d in npm/*/; do
      npm pack "./$d" --pack-destination dist/npm
    done

# install the packed launcher and this host's binary package into dist/smoke, then install and render through them
[script]
smoke: pack
    v=$(node -p "require('./npm/claude-statusline/package.json').version")
    host=$(node -p "process.platform + '-' + process.arch")
    ext=$(node -p "process.platform === 'win32' ? '.exe' : ''")
    smoke="$root/dist/smoke"
    home="$smoke/home"
    rm -rf "$smoke"
    mkdir -p "$home"
    npm install --prefix "$smoke" --no-save --no-package-lock \
      "$root/dist/npm/primeinc-claude-statusline-$v.tgz" \
      "$root/dist/npm/primeinc-claude-statusline-$host-$v.tgz"
    launcher="$smoke/node_modules/@primeinc/claude-statusline/bin/claude-statusline"
    payload='{"workspace":{"current_dir":"'"$root"'"},"model":{"id":"claude-opus-5"},"context_window":{"used_percentage":42}}'
    echo "--- launcher render"
    printf '%s' "$payload" | HOME="$home" USERPROFILE="$home" FORCE_HYPERLINK=0 node "$launcher"
    echo "--- launcher install"
    HOME="$home" USERPROFILE="$home" node "$launcher" install
    echo "--- $home/.claude/settings.json"
    cat "$home/.claude/settings.json"
    echo "--- render through the installed copy"
    printf '%s' "$payload" | FORCE_HYPERLINK=0 "$home/.claude/claude-statusline$ext"
    echo "--- launcher uninstall"
    HOME="$home" USERPROFILE="$home" node "$launcher" uninstall
    cat "$home/.claude/settings.json"

# publish the smoke-tested tarballs: platform packages first, launcher last
[script]
publish: smoke
    v=$(node -p "require('./npm/claude-statusline/package.json').version")
    for d in npm/claude-statusline-*/; do
      npm publish "dist/npm/primeinc-$(basename "$d")-$v.tgz" --access public
    done
    npm publish "dist/npm/primeinc-claude-statusline-$v.tgz" --access public

# fail unless every package at the launcher's version is on the registry
[script]
verify:
    v=$(node -p "require('./npm/claude-statusline/package.json').version")
    for d in npm/*/; do
      name="@primeinc/$(basename "$d")"
      echo "$name@$v: $(npm view "$name@$v" version --prefer-online)"
    done
