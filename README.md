# claude-statusline

A single-file, zero-dependency [Claude Code](https://code.claude.com/) statusline.

Renders one tight line:

```
~/dev/claude-statusline/ opus-4.7 [63k/200k] primeinc/claude-statusline ⎇ feature/foo
```

Everything is OSC 8 clickable in terminals that honour hyperlinks (Windows Terminal, iTerm2, WezTerm, Kitty, vscode, Ghostty). The `~/path` opens the folder; `primeinc/claude-statusline` opens the repo; `⎇ feature/foo` opens the branch view.

## Why a custom statusline

The bundled examples in [Claude Code's statusline docs](https://code.claude.com/docs/en/statusline) shell out to `git`, `gh`, and `jq` on every render. That's ~6 subprocess spawns per refresh. This one:

- Reads `.git/config`, `.git/HEAD`, and `commondir` directly — no `git` subprocess
- Reads context usage from Claude Code's `context_window` stdin payload — no transcript file walk. When the payload omits those fields it shows a visible `[ctx —]` rather than a fabricated default
- Resolves git worktrees via `commondir` so `config` is read from the common dir, not the per-worktree gitdir
- Normalises GitHub SSH remotes (`git@github.com:owner/repo.git`) to `https://github.com/owner/repo`
- Sanitizes every repo-controlled value (paths, branch names, remotes) before terminal emission and percent-encodes all URLs, so a hostile repo can't inject escape sequences
- Detects hyperlink-capable terminals and emits OSC 8; otherwise falls back to raw URLs

Each behavior above is covered by a fixture in `test/run.js`. Render cost is dominated by Node startup; Claude Code debounces statusline updates at 300 ms.

## Install

**One-liner (recommended):**

```bash
npx @primeinc/claude-statusline install
```

The installer wires the command into `~/.claude/settings.json` and adds `FORCE_HYPERLINK=1` to the `env` block (required for Windows Terminal). It's idempotent and preserves all other settings.

Then restart Claude Code.

After a global install (`npm i -g @primeinc/claude-statusline`), the short `ccsl` command is also available:

```bash
ccsl install       # same as: npx @primeinc/claude-statusline install
ccsl uninstall
ccsl --help
```

**Installer flags:**

```bash
ccsl install --print           # dry run
ccsl install --force           # overwrite a foreign existing statusLine
                               # (backs it up to settings.statusLineBackup)
ccsl install --dest PATH       # non-default script destination
ccsl install --settings PATH   # non-default settings.json location
ccsl install --no-copy         # use the installed package's file in place
                               # (updates flow via `npm update -g`)
ccsl uninstall                 # restores statusLineBackup if present
ccsl --help
```

**Manual install (no npm):**

1. Drop `statusline.js` somewhere on disk (e.g. `~/.claude/statusline.js`).
2. Merge `examples/settings.fragment.json` into `~/.claude/settings.json`.

The fragment is plain JSON, deep-mergeable by anything (`jq`, ansible, manual edit):

```bash
jq -s '.[0] * .[1]' ~/.claude/settings.json examples/settings.fragment.json \
  | tee ~/.claude/settings.json.new && mv ~/.claude/settings.json.new ~/.claude/settings.json
```

Or just copy the fields by hand:

```json
{
  "env": {
    "FORCE_HYPERLINK": "1"
  },
  "statusLine": {
    "type": "command",
    "command": "node ~/.claude/statusline.js"
  }
}
```

Restart Claude Code.

## Environment variables

| Var | Effect |
|---|---|
| `FORCE_HYPERLINK=1` | Forces Claude Code (and this script) to emit OSC 8 hyperlinks even when the terminal isn't in the auto-detect list. Required for Windows Terminal. |
| `STATUSLINE_DEBUG=1` | Appends a JSON line per render to `~/.claude/statusline-debug.log` with the stdin payload summary, every hyperlink-related env var, and the exact bytes emitted. Off by default. |

## What it shows

Per render, Claude Code pipes a JSON payload over stdin. This script uses:

| Field | Used for |
|---|---|
| `workspace.current_dir` / `cwd` | The `~/path/` segment and the `file://` link target |
| `model.id` | The short model slug (`opus-4.7`) |
| `context_window.used_percentage` / `remaining_percentage` | Context field as `[N%]` — the [documented](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md) contract |
| `context_window.total_input_tokens` / `context_window_size` | If a build also supplies raw counts, the exact `[used/total]` form is preferred over the percentage |

If none of those fields are present, the context field renders `[ctx —]` — it never fabricates `[0/200k]`.

Then it reads from `.git/`:

| File | Used for |
|---|---|
| `HEAD` (or `<worktree>/.git/HEAD` after resolving via `commondir`) | Current branch name, or detached-HEAD sha |
| `config` (in common dir) | Origin remote URL → `owner/repo` |

### Git support boundary

Backed by fixtures in `test/run.js`: normal repos, linked worktrees, detached HEAD, GitHub HTTPS and SSH remotes.

Intentionally unsupported (no repo chip is shown): non-GitHub hosts, GitHub Enterprise, `ssh://` URLs, non-`origin` remotes, and refs that live only in `packed-refs` (the `HEAD` symref itself is still read correctly).

## Triggers and cadence

[Per the docs](https://code.claude.com/docs/en/statusline#how-status-lines-work), Claude Code reruns the statusline:

- after each new assistant message
- after `/compact`
- on permission-mode change
- on vim-mode toggle

Triggers are debounced at 300 ms; in-flight scripts are cancelled if a new trigger fires. No `refreshInterval` is needed for this script — all data is push-driven.

## Hardening

- Pure synchronous I/O — no timers, no promises, no long-lived async work. The only event listener is a fire-once `stdout` 'error' guard. Process exits the tick after stdout flush.
- Every `fs.readFileSync` for git files is wrapped in `try/catch` — missing files, permission errors, dir-shaped placeholders, and symlink loops all degrade gracefully.
- `process.stdout.on('error')` guards against EPIPE if Claude Code cancels mid-render.
- Empty/malformed stdin renders a minimal line, no crash.
- The debug-log append is also wrapped — disk-full or unwritable HOME won't break the render.

## License

MIT — see `LICENSE`.
