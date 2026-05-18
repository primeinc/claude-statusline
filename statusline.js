#!/usr/bin/env node
// claude-statusline — a single-file, non-blocking statusline for Claude Code.
// Output: <cwd> <model> [used/total] [<owner/repo>] [⎇ <branch>]
//
// All state comes from Claude Code's stdin payload (no transcript I/O) and
// directly-parsed .git files (no `git` subprocess). Pure sync code, no
// timers or event listeners — process exits the tick after writing stdout.
//
// See README.md for installation, env vars, and the docs citations behind
// each non-obvious choice.

'use strict';

const fs   = require('fs');
const os   = require('os');
const path = require('path');

// Defensive: if Claude Code tore down our stdout mid-render (in-flight cancel
// per docs), Node would otherwise throw EPIPE. Output is tiny so this is
// unreachable today, but the cost of guarding is one line.
process.stdout.on('error', () => process.exit(0));

// ── Read stdin payload from Claude Code ──────────────────────────────────────
let raw = '';
try { raw = fs.readFileSync(0, 'utf8'); } catch { raw = ''; }

let data = {};
try { data = JSON.parse(raw); } catch { data = {}; }

// ── Diagnostics (opt-in via STATUSLINE_DEBUG=1) ──────────────────────────────
// Appends a JSON line per render to ~/.claude/statusline-debug.log.
// Off by default; turn on only when verifying terminal/env behaviour.
const HOME  = os.homedir();
const DEBUG = !!process.env.STATUSLINE_DEBUG;
function dbg(stage, payload) {
  if (!DEBUG) return;
  try {
    fs.appendFileSync(
      path.join(HOME, '.claude', 'statusline-debug.log'),
      JSON.stringify({ t: new Date().toISOString(), stage, ...payload }) + '\n',
    );
  } catch { /* never break statusline */ }
}

dbg('input', {
  stdin_bytes: raw.length,
  parsed_keys: Object.keys(data),
  ctx_window: data.context_window || null,
  // Every env signal that can flip Claude Code's hyperlink detection. The
  // built-in list in the binary is: FORCE_HYPERLINK, TERM_PROGRAM matches
  // ["ghostty","Hyper","kitty","alacritty","iTerm.app","iTerm2"], plus
  // LC_TERMINAL, tmux, and TERM containing "kitty". WT_SESSION is NOT in
  // the list — that's why Windows Terminal needs FORCE_HYPERLINK=1.
  env: {
    FORCE_HYPERLINK:      process.env.FORCE_HYPERLINK      ?? null,
    WT_SESSION:           process.env.WT_SESSION           ?? null,
    WT_PROFILE_ID:        process.env.WT_PROFILE_ID        ?? null,
    TERM_PROGRAM:         process.env.TERM_PROGRAM         ?? null,
    TERM_PROGRAM_VERSION: process.env.TERM_PROGRAM_VERSION ?? null,
    LC_TERMINAL:          process.env.LC_TERMINAL          ?? null,
    TERM:                 process.env.TERM                 ?? null,
    COLORTERM:            process.env.COLORTERM            ?? null,
    KITTY_WINDOW_ID:      process.env.KITTY_WINDOW_ID      ?? null,
    VTE_VERSION:          process.env.VTE_VERSION          ?? null,
    NO_COLOR:             process.env.NO_COLOR             ?? null,
  },
  stdout_isTTY: !!process.stdout.isTTY,
});

// ── CWD with tilde-collapse ──────────────────────────────────────────────────
// Trailing '/' marks directories per ls -F / RFC 8089.
const cwd = (data.workspace && data.workspace.current_dir) || data.cwd || '';

function tildeCollapse(p) {
  if (!p || !HOME) return p;
  const homeFwd = HOME.replace(/\\/g, '/');
  const cwdFwd  = p.replace(/\\/g, '/');
  if (cwdFwd === homeFwd)                    return '~';
  if (cwdFwd.startsWith(homeFwd + '/'))      return '~' + cwdFwd.slice(homeFwd.length);
  return p;
}

let cwdShort = tildeCollapse(cwd);
if (cwdShort && !cwdShort.endsWith('/') && !cwdShort.endsWith('\\')) cwdShort += '/';

// ── Short model slug ─────────────────────────────────────────────────────────
// claude-sonnet-4-6-20251001 → sonnet-4.6
// claude-opus-4-7            → opus-4.7
const modelId  = (data.model && data.model.id) || '';
let slug       = modelId.replace(/^claude-/, '');
slug           = slug.replace(/-\d{8}$/, '');   // strip -YYYYMMDD
slug           = slug.replace(/\[.*?\]$/, '');   // strip trailing [...]
const lastDash = slug.lastIndexOf('-');
const modelSlug = lastDash !== -1
  ? slug.slice(0, lastDash) + '.' + slug.slice(lastDash + 1)
  : slug;

// ── Context usage [used/total] ───────────────────────────────────────────────
// Live values straight from Claude Code's stdin (v2.1.132+). No transcript I/O.
const cw       = data.context_window || {};
const ctxUsed  = cw.total_input_tokens   || 0;
const ctxTotal = cw.context_window_size  || 200_000;

function fmtTokens(n) {
  if (n >= 1_000_000) return (n / 1_000_000).toPrecision(3).replace(/\.?0+$/, '') + 'M';
  if (n >= 1_000)     return Math.round(n / 1_000) + 'k';
  return String(n);
}
const ctxField = `[${fmtTokens(ctxUsed)}/${fmtTokens(ctxTotal)}]`;

// ── GitHub link + branch chip ────────────────────────────────────────────────
// All in-process: read .git files directly. Worktrees, packed-refs, and
// SSH-style remotes all handled.
function findGitDir(start) {
  let dir = start;
  while (dir) {
    const candidate = path.join(dir, '.git');
    try {
      const st = fs.statSync(candidate);
      if (st.isDirectory()) return candidate;
      if (st.isFile()) {
        // Linked worktree: .git is a file containing "gitdir: <path>"
        const txt = fs.readFileSync(candidate, 'utf8').trim();
        const m   = txt.match(/^gitdir:\s*(.+)$/);
        if (m) return path.resolve(dir, m[1]);
      }
    } catch { /* not here */ }
    const parent = path.dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
  return null;
}

// In a linked worktree, per-worktree gitdir holds HEAD/index but config,
// packed-refs, and refs/remotes live in the common dir. Resolve via `commondir`.
function commonDir(gitDir) {
  try {
    const txt = fs.readFileSync(path.join(gitDir, 'commondir'), 'utf8').trim();
    return path.resolve(gitDir, txt);
  } catch { return gitDir; }
}

function parseOriginUrl(commonGitDir) {
  try {
    const cfg = fs.readFileSync(path.join(commonGitDir, 'config'), 'utf8');
    const m   = cfg.match(/\[remote\s+"origin"\][^\[]*?url\s*=\s*(.+)/);
    return m ? m[1].trim() : '';
  } catch { return ''; }
}

function currentBranch(gitDir) {
  try {
    const head = fs.readFileSync(path.join(gitDir, 'HEAD'), 'utf8').trim();
    const m    = head.match(/^ref:\s*refs\/heads\/(.+)$/);
    return m ? m[1] : 'HEAD'; // detached
  } catch { return ''; }
}

// origin/HEAD may be a loose file, a symbolic ref in packed-refs, or absent.
function defaultBranch(commonGitDir) {
  try {
    const head = fs.readFileSync(path.join(commonGitDir, 'refs/remotes/origin/HEAD'), 'utf8').trim();
    const m    = head.match(/^ref:\s*refs\/remotes\/origin\/(.+)$/);
    if (m) return m[1];
  } catch { /* try packed */ }
  try {
    const packed = fs.readFileSync(path.join(commonGitDir, 'packed-refs'), 'utf8');
    const sym    = packed.match(/^ref:\s*refs\/remotes\/origin\/(.+)$/m);
    if (sym) return sym[1];
  } catch { /* no packed-refs */ }
  return '';
}

let repoSlug   = '';
let repoUrl    = '';
let branchUrl  = '';
let branchName = '';
const gitDir = cwd ? findGitDir(cwd) : null;
if (gitDir) {
  const common = commonDir(gitDir);
  const remote = parseOriginUrl(common);
  if (remote.startsWith('https://github.com/')) repoSlug = remote.slice('https://github.com/'.length);
  else if (remote.startsWith('git@github.com:')) repoSlug = remote.slice('git@github.com:'.length);
  repoSlug = repoSlug.replace(/\.git$/, '');

  if (repoSlug) {
    repoUrl = `https://github.com/${repoSlug}`;
    const branch = currentBranch(gitDir);
    const def    = defaultBranch(common);
    if (branch && branch !== 'HEAD' && (!def || branch !== def)) {
      branchName = branch;
      branchUrl  = `${repoUrl}/tree/${branch}`;
    }
  }
}

// ── Hyperlink (OSC 8) detection ──────────────────────────────────────────────
// Windows Terminal supports OSC 8 natively since v1.4 (Sep 2020) but Claude
// Code's auto-detect list doesn't include WT_SESSION, hence FORCE_HYPERLINK=1.
const env = process.env;
const tp  = env.TERM_PROGRAM || '';
const hyperlinks = !!(
  env.FORCE_HYPERLINK ||
  env.WT_SESSION ||                             // Windows Terminal
  env.KITTY_WINDOW_ID ||                        // Kitty
  env.VTE_VERSION ||                            // GNOME Terminal etc.
  tp === 'iTerm.app' ||
  tp === 'WezTerm' ||
  tp === 'vscode' ||
  tp === 'ghostty'
);

// OSC 8: ESC ] 8 ;; URL BEL  TEXT  ESC ] 8 ;; BEL
function link(url, label) {
  if (!hyperlinks) return label;
  return `\x1b]8;;${url}\x07${label}\x1b]8;;\x07`;
}

// file:/// URL for cwd, with RFC 8089 trailing slash for directories.
const cwdUrl = cwd ? 'file:///' + cwd.replace(/\\/g, '/').replace(/\/?$/, '/') : '';

// ── Assemble output ──────────────────────────────────────────────────────────
// Repo label is always `owner/repo`; branch lives in its own ⎇ chip so each
// click target is unambiguous.
const cwdField    = cwdUrl ? link(cwdUrl, cwdShort) : cwdShort;
const repoField   = repoSlug
  ? (hyperlinks ? link(repoUrl, repoSlug) : repoUrl)
  : '';
const branchField = branchName
  ? (hyperlinks ? `⎇ ${link(branchUrl, branchName)}` : `⎇ ${branchName} ${branchUrl}`)
  : '';

// Skip empty fields so we never emit leading/double spaces.
const line = [cwdField, modelSlug, ctxField, repoField, branchField]
  .filter(Boolean)
  .join(' ') + '\n';

dbg('output', {
  hyperlinks_enabled: hyperlinks,
  line_hex:  Buffer.from(line).toString('hex'),
  line_text: line.replace(/\x1b/g, '\\x1b').replace(/\x07/g, '\\x07'),
});

process.stdout.write(line);
