# claude-statusline

One-line [Claude Code](https://code.claude.com/docs/en/statusline) status line, one Go binary, no `git` subprocess.

```
~/dev/claude-statusline/ fable-5.1 [43%] primeinc/claude-statusline ⎇ go-rewrite scratch sess
```

Every chip is an OSC 8 hyperlink when `FORCE_HYPERLINK` or `WT_SESSION` is set:

| chip | opens |
|---|---|
| `~/path/` | the working directory |
| `owner/repo` | the repository on its forge |
| `⎇ branch` | the branch view, or the commit when HEAD is detached |
| `scratch` | this session's scratchpad: `<tmp>/claude/<project>/<session_id>/scratchpad` |
| `sess` | this session's folder under `~/.claude/projects/` (subagents, tool results); the project folder until it exists |

Without hyperlinks the line shows the repo URL and branch name as plain text and omits `scratch` and `sess`.

## Install

```bash
go install github.com/primeinc/claude-statusline@latest
claude-statusline install
```

`install` writes `statusLine.command` (this binary's path) and `env.FORCE_HYPERLINK=1` into `~/.claude/settings.json`, preserving every other key and the file's order. It replaces an entry written by the previous Node installer (`node …/.claude/statusline.js`) and refuses any other existing `statusLine` unless `--force`.

```
claude-statusline install [--force] [--print] [--settings PATH]
claude-statusline uninstall [--print] [--settings PATH]
```

Exit codes: 0 ok, 1 error, 2 refused to overwrite a foreign `statusLine`. `CLAUDE_SETTINGS` overrides the settings path.

## Input

Fields read from the stdin payload: `workspace.current_dir` (or `cwd`), `model.id`, `context_window.*`, `session_id`, `transcript_path`.

Context field precedence: `[used/total]` when both token counts are present, else `[N%]` from `used_percentage` or `remaining_percentage`, else `[ctx —]`. Nothing is fabricated.

Model slug: `claude-sonnet-4-6-20251001` → `sonnet-4.6`, `claude-opus-4-7[1m]` → `opus-4.7`, `claude-opus-5` → `opus-5`.

## Git

Read directly from `.git`: `HEAD` from the per-worktree directory, `config` from the common directory (`commondir`), so linked worktrees resolve. Detached HEAD renders `HEAD @<sha7>`.

Forges: `github.com` (`/tree/<branch>`, `/commit/<sha>`) and `git.title.dev` (Forgejo: `/src/branch/<branch>`, `/commit/<sha>`). Remote forms: `https://`, `ssh://`, `git://`, `git+ssh://`, `git+https://`, and `git@host:owner/repo.git`. Any other host, a missing origin, or a config git itself would reject renders no repo chip.

Every repo-controlled string is stripped of C0/C1 control bytes and every URL is built from percent-encoded segments before it reaches the terminal.

## Cost

Claude Code on Windows runs the command through Git Bash. Measured with hyperfine (30 runs, warmup 5, this machine):

| command under `bash -c` | mean |
|---|---|
| `true` | 28.5 ms |
| minimal Go process printing one line | 39.6 ms |
| `claude-statusline` (full render) | 40.7 ms |
| `node statusline.js` (previous implementation) | 84.3 ms |

A persistent daemon still needs a spawned client, whose floor is the second row. Its ceiling is therefore about 1 ms per render. Not built.

## Development

```
just test    # go test ./...
just lint    # go vet + golangci-lint (.golangci.yml)
just fmt     # gofmt + goimports
just check   # test + lint + formatting diff
just bench   # hyperfine under bash -c
```

## Provenance

| concern | source | revision | relation |
|---|---|---|---|
| payload contract | code.claude.com/docs/en/statusline | fetched 2026-09-04 | contract |
| scratchpad and session dirs | observed on this machine, 61 projects | 2026-09-04 | observed, not documented upstream |
| remote URL normalisation | cli/cli `git/url.go` | ad2a338 | adapted |
| git-config parsing | go-git/gcfg | 8c5976d | dependency |
| settings.json editing | tidwall/sjson, gjson, pretty | 3a21ce7, 8d89927, 9090695 | dependency |
| Forgejo URL scheme | forgejo/forgejo `modules/git/ref.go`, `routers/web/web.go` | d7471ea | cited |
| lint baseline | cli/cli, oh-my-posh `.golangci.yml` | ad2a338, bc0845d | adapted |
| daemon architecture | oh-my-posh `src/cli/serve.go` | bc0845d | observed, rejected: its shell owns the daemon's stdin; Claude Code spawns a fresh shell per render |

## License

MIT — see `LICENSE`.
