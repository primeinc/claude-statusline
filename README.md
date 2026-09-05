# claude-statusline

One-line [Claude Code](https://code.claude.com/docs/en/statusline) status line, one Go binary, no `git` subprocess.

```
~/dev/claude-statusline/ fable-5.1 [43%] primeinc/claude-statusline ⎇ go-rewrite scratch sess
```

Every chip is an OSC 8 hyperlink when hyperlinks are on:

| chip | opens |
|---|---|
| `~/path/` | the working directory |
| `owner/repo` | the repository on its forge |
| `⎇ branch` | the branch view, or the commit when HEAD is detached |
| `scratch` | this session's scratchpad: `<tmp>/claude/<project>/<session_id>/scratchpad` |
| `sess` | this session's folder under `~/.claude/projects/` (subagents, tool results); the project folder until it exists |

Hyperlinks: `FORCE_HYPERLINK` is Claude Code's own override and wins when set (`0` disables, anything else enables). Unset, `WT_SESSION` (Windows Terminal, observed on this machine, not auto-detected by Claude Code) enables them. Off, the line shows the repo URL and branch name as plain text and omits `scratch` and `sess`.

## Install

Until a tagged release exists on `main`, install from a checkout:

```bash
git clone https://github.com/primeinc/claude-statusline
cd claude-statusline
go install .
claude-statusline install
```

`install` writes `statusLine.command` (this binary's path) and `env.FORCE_HYPERLINK=1` into `~/.claude/settings.json`, preserving every other key and the file's order. The whole file is re-emitted with two-space indentation and LF line endings. The write goes through a temp file and rename (a symlinked settings.json is followed to its target; a dangling link is replaced by a file; a hard link would be severed). It recognises its own entry exactly (no substring or basename matching; case-insensitive on Windows) and the previous Node installer's entry only when the file at `~/.claude/statusline.js` begins with that script's header. Any other existing `statusLine` is refused unless `--force`.

`uninstall` removes the entry, restores a `statusLineBackup` left by the previous Node installer if one exists, and leaves `env.FORCE_HYPERLINK` alone because Claude Code's own links use it.

```
claude-statusline install [--force] [--print] [--settings PATH]
claude-statusline uninstall [--print] [--settings PATH]
```

Exit codes: 0 ok, 1 error, 2 refused to overwrite a foreign `statusLine`. `CLAUDE_SETTINGS` overrides the settings path.

## Input

Fields read from the stdin payload: `workspace.current_dir` (or `cwd`), `workspace.repo`, `model.id`, `context_window.*`, `session_id`, `transcript_path`. The documented full payload is a test fixture (`internal/render/testdata/docs-payload.json`).

Context field precedence: `[used/total]` when both token counts are present, else `[N%]` from `used_percentage` or `remaining_percentage`, else `[ctx —]`. Nothing is fabricated.

Model slug: `claude-sonnet-4-6-20251001` → `sonnet-4.6`, `claude-opus-4-7[1m]` → `opus-4.7`, `claude-opus-5` → `opus-5`. Ids in other shapes (older `claude-3-5-sonnet-…`, Bedrock or Vertex prefixes) render with only the `claude-` prefix and date removed.

A payload with one mistyped field keeps every other field; only unparseable input renders the bare `[ctx —]` line, with a note on stderr (`claude --debug` logs it).

## Git

Repository identity comes from the payload's `workspace.repo` when Claude Code supplies it, which resolves `include`, `url.insteadOf` and other cases git handles. Otherwise `remote.origin.url` is read from `.git/config` with a git-config parser (gcfg); a config git itself would reject, or one with a UTF-8 BOM, renders no repo chip on that path. HEAD is always read from `.git`: the per-worktree directory for linked worktrees, with `config` from `commondir`. Detached HEAD renders `HEAD @<sha7>`.

Forges: `github.com` (`/tree/<branch>`, `/commit/<sha>`) and `git.title.dev` (Forgejo: `/src/branch/<branch>`, `/commit/<sha>`). Remote forms: `https://`, `ssh://`, `git://`, `git+ssh://`, `git+https://`, and `git@host:owner/repo.git`. Other hosts render no repo chip.

Every repo-controlled string is stripped of Unicode control (Cc) and format (Cf) characters plus line and paragraph separators, and every URL is built from percent-encoded segments, before it reaches the terminal.

## Cost

Claude Code on Windows runs the command through the Git Bash launcher named by `CLAUDE_CODE_GIT_BASH_PATH` (`C:\Git\bin\bash.exe`), which spawns `usr\bin\bash.exe`, which runs the command: three fresh processes per render, observed with a PID-logging wrapper. `just bench` measures through that exact launcher (hyperfine, 50 runs, warmup 5):

| command under `C:/Git/bin/bash.exe -c` | mean |
|---|---|
| `true` | 28.2 ms |
| `claude-statusline` (full render) | 39.3 ms |

The `true` row is the floor any command pays under that launcher, a persistent daemon's client included. Not built.

## Development

```
just test           # go test ./...
just lint           # go vet + golangci-lint (.golangci.yml: default all, reasoned disables)
just fmt            # gofmt + goimports
just check          # test + lint + formatting diff
just bench          # hyperfine through the Git Bash launcher
just real-configs 'C:/Users/you/dev/*/.git/config'   # parse every real .git/config with the shipped reader
```

CI (`.github/workflows/ci.yml`) declares vet and tests on Windows and Ubuntu, and golangci-lint pinned to the local version, on pull requests and pushes to `main`.

## Provenance

| concern | source | revision | relation |
|---|---|---|---|
| payload contract | code.claude.com/docs/en/statusline | fetched 2026-09-04; fixture in testdata | contract |
| `FORCE_HYPERLINK=0` opt-out | anthropics/claude-code CHANGELOG.md | entry under 2.1.217 | contract |
| scratchpad and session dirs | observed on this machine, 61 projects | 2026-09-04 | observed, not documented upstream |
| Windows shell chain | observed: PID-logging wrapper, 3 sessions | 2026-09-04 | observed; docs state Git Bash |
| remote URL normalisation | cli/cli `git/url.go` | ad2a338 | adapted |
| git-config parsing | go-git/gcfg | v1.5.1-0.20230307220236-3a3c6141e376 | dependency |
| settings.json editing | tidwall/sjson, gjson, pretty | v1.2.5, v1.19.0, v1.2.1 | dependency |
| Forgejo URL scheme | forgejo/forgejo `modules/git/ref.go:205`, `routers/web/web.go:1781` | d7471ea | cited |
| lint baseline | golangci-lint `.golangci.reference.yml`; cli/cli, oh-my-posh configs | 2b2fbaf; ad2a338, bc0845d | adapted |
| CI | golangci-lint docs `welcome/install/ci.md`; golangci-lint-action `action.yml` | 2b2fbaf; 2ed87ef | adapted |
| daemon architecture | oh-my-posh `src/cli/serve.go` | bc0845d | observed, rejected: its shell owns the daemon's stdin; Claude Code spawns a fresh shell per render |

## License

MIT — see `LICENSE`.
