# claude-statusline

A single-file, zero-dependency [Claude Code](https://code.claude.com/) statusline.

Renders one tight line:

```
~/dev/claude-statusline/ opus-4.7 [63k/200k] primeinc/claude-statusline ⎇ feature/foo
```

Everything is OSC 8 clickable in terminals that honour hyperlinks (Windows Terminal, iTerm2, WezTerm, Kitty, vscode, Ghostty). The `~/path` opens the folder; `primeinc/claude-statusline` opens the repo; `⎇ feature/foo` opens the branch view.

## Why a custom statusline

The bundled examples in [Claude Code's statusline docs](https://code.claude.com/docs/en/statusline) shell out to `git`, `gh`, and `jq` on every render. That's ~6 subprocess spawns per refresh. This one:

- Reads `.git/config`, `.git/HEAD`, `packed-refs`, and `commondir` directly — no `git` subprocess
- Reads context token usage from Claude Code's `context_window.*` stdin payload (added in v2.1.132) — no transcript file walk
- Resolves git worktrees via `commondir` so `config` and `refs/remotes/origin/HEAD` are found in the common dir, not the per-worktree gitdir
- Falls back to scanning `packed-refs` when `refs/remotes/origin/HEAD` isn't a loose file
- Normalises SSH remotes (`git@github.com:owner/repo.git`) to `https://github.com/owner/repo`
- Detects hyperlink-capable terminals and emits OSC 8; otherwise falls back to raw URLs

Render time on Windows is ~90 ms, dominated by Node startup. The actual JS work is <5 ms. Claude Code debounces statusline updates at 300 ms, so we're well inside the budget.

## Install

**One-liner (recommended):**

```bash
npm i -g @primeinc/claude-statusline && claude-statusline-install
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
| `context_window.total_input_tokens` | `[used/total]` numerator |
| `context_window.context_window_size` | `[used/total]` denominator (handles 200k vs 1M models automatically) |

Then it reads from `.git/`:

| File | Used for |
|---|---|
| `HEAD` (or `<worktree>/.git/HEAD` after resolving via `commondir`) | Current branch name |
| `config` (in common dir) | Origin remote URL → `owner/repo` |
| `refs/remotes/origin/HEAD` or `packed-refs` | Default branch — used to suppress the `⎇ branch` chip when you're already on it |

## Triggers and cadence

[Per the docs](https://code.claude.com/docs/en/statusline#how-status-lines-work), Claude Code reruns the statusline:

- after each new assistant message
- after `/compact`
- on permission-mode change
- on vim-mode toggle

Triggers are debounced at 300 ms; in-flight scripts are cancelled if a new trigger fires. No `refreshInterval` is needed for this script — all data is push-driven.

## Hardening

- Pure synchronous I/O — no timers, no promises, no event listeners. Process exits the tick after stdout flush.
- Every `fs.readFileSync` for git files is wrapped in `try/catch` — missing files, permission errors, dir-shaped placeholders, and symlink loops all degrade gracefully.
- `process.stdout.on('error')` guards against EPIPE if Claude Code cancels mid-render.
- Empty/malformed stdin renders a minimal line, no crash.
- The debug-log append is also wrapped — disk-full or unwritable HOME won't break the render.

## License

MIT — see `LICENSE`.
