#!/usr/bin/env node
// claude-statusline — a single-file, non-blocking statusline for Claude Code.
// Output: <cwd> <model> [used/total] [<owner/repo>] [⎇ <branch>]
//
// All state comes from Claude Code's stdin payload (no transcript I/O) and
// directly-parsed .git files (no `git` subprocess). Pure synchronous code with
// no timers and no long-lived async work — the one event listener is a
// fire-once stdout 'error' guard. Process exits the tick after writing stdout.
//
// See README.md for installation, env vars, and the docs citations behind
// each non-obvious choice.

'use strict';

const fs   = require('fs');
const os   = require('os');
const path = require('path');
const { pathToFileURL } = require('url');

// Repository-controlled values (paths, branch names, remotes) flow into terminal
// escape sequences. Strip C0/C1 control bytes so a hostile branch/path/remote
// can't inject its own OSC/CSI sequences into the statusline.
function cleanText(s) {
  return String(s == null ? '' : s).replace(/[\x00-\x1f\x7f-\x9f]/g, '');
}

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
const DEBUG = process.env.STATUSLINE_DEBUG === '1';   // exact match — `=0` is OFF
function dbg(stage, payload) {
  if (!DEBUG) return;
  try {
    const dir = path.join(HOME, '.claude');
    fs.mkdirSync(dir, { recursive: true });   // opt-in diagnostics must not silently no-op
    fs.appendFileSync(
      path.join(dir, 'statusline-debug.log'),
      JSON.stringify({ t: new Date().toISOString(), stage, ...payload }) + '\n',
    );
  } catch { /* never break statusline */ }
}

dbg('input', {
  stdin_bytes: raw.length,
  parsed_keys: Object.keys(data),
  ctx_window: data.context_window || null,
  // Every env signal we use for hyperlink detection (see HYPERLINK_TPS below).
  // Captured so the debug log shows exactly why a render did/didn't hyperlink.
  // Note: Windows Terminal sets WT_SESSION but is commonly not auto-detected,
  // which is why this project sets FORCE_HYPERLINK=1 on install.
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

// ── Context usage ────────────────────────────────────────────────────────────
// Straight from Claude Code's stdin `context_window` object. No transcript I/O.
//
// Documented contract (refs/anthropics/claude-code/CHANGELOG.md):
//   context_window.used_percentage       — % of the window consumed
//   context_window.remaining_percentage  — % still free
//   exceeds_200k_tokens (top-level bool) — over the 200k tier
// Older/other builds may ALSO surface raw token counts; if both a numerator
// and denominator are present we prefer the exact `[used/total]` form.
//
// Contract fallback: if NONE of these are present we render a visible `[ctx —]`
// rather than fabricate `[0/200k]`. A missing field must look missing.
const cw = data.context_window || {};

function fmtTokens(n) {
  // 999_500 boundary, not 1_000_000: above it `round(n/1000)` would render
  // "1000k" instead of promoting to "1M".
  if (n >= 999_500) return (n / 1_000_000).toPrecision(3).replace(/\.?0+$/, '') + 'M';
  if (n >= 1_000)   return Math.round(n / 1_000) + 'k';
  return String(n);
}

// Percentages come from the stdin payload — clamp to the documented 0–100 range
// so a bad upstream value can't render `[-5%]` / `[150%]`.
const clampPct = (p) => Math.max(0, Math.min(100, Math.round(p)));

let ctxField;
if (Number.isFinite(cw.total_input_tokens) && Number.isFinite(cw.context_window_size)) {
  ctxField = `[${fmtTokens(cw.total_input_tokens)}/${fmtTokens(cw.context_window_size)}]`;
} else if (Number.isFinite(cw.used_percentage)) {
  ctxField = `[${clampPct(cw.used_percentage)}%]`;
} else if (Number.isFinite(cw.remaining_percentage)) {
  ctxField = `[${clampPct(100 - cw.remaining_percentage)}%]`;
} else {
  ctxField = '[ctx —]';   // contract not satisfied — degrade visibly, never lie
}

// ── GitHub link + branch chip ────────────────────────────────────────────────
// All in-process: read .git files directly, no `git` subprocess.
// Support boundary (each backed by a fixture in test/run.js):
//   • normal repos (.git directory)
//   • linked worktrees (.git file → gitdir, config resolved via commondir)
//   • detached HEAD (raw sha in HEAD → /commit/<sha>)
//   • remotes: GitHub HTTPS (https://github.com/o/r[.git])
//             and GitHub SSH (git@github.com:o/r[.git])
// Intentionally NOT supported (rendered as no repo chip): non-GitHub hosts,
// GitHub Enterprise, ssh:// URLs, non-`origin` remotes, and branches whose
// current ref lives only in packed-refs (HEAD symref is still read fine).
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
    if (m) return { kind: 'branch', name: m[1], sha: '' };
    // Detached HEAD — `head` is a raw 40-char sha.
    if (/^[0-9a-f]{7,40}$/.test(head)) return { kind: 'detached', name: '', sha: head.slice(0, 7) };
    return { kind: 'unknown', name: '', sha: '' };
  } catch { return { kind: 'unknown', name: '', sha: '' }; }
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
    // Encode each owner/repo segment for URL use, exactly like branch segments
    // below — a hostile .git/config url must not put a raw space/#/? into the
    // OSC 8 target. `/` stays a path separator.
    const encodedSlug = repoSlug.split('/').map(encodeURIComponent).join('/');
    repoUrl = `https://github.com/${encodedSlug}`;
    const head = currentBranch(gitDir);
    if (head.kind === 'branch') {
      branchName = head.name;
      // Keep `/` as path separators (GitHub wants them) but escape spaces, #,
      // %, ?, and other URL-significant chars within each segment.
      const encodedBranch = head.name.split('/').map(encodeURIComponent).join('/');
      branchUrl  = `${repoUrl}/tree/${encodedBranch}`;
    } else if (head.kind === 'detached') {
      // Detached HEAD: show `HEAD @abc1234`, link to the commit on GitHub.
      branchName = `HEAD @${head.sha}`;
      branchUrl  = `${repoUrl}/commit/${head.sha}`;
    }
  }
}

// ── Hyperlink (OSC 8) detection ──────────────────────────────────────────────
// Windows Terminal sets WT_SESSION but is commonly not auto-detected by Claude
// Code, so this project sets FORCE_HYPERLINK=1 on install to force OSC 8 there.
const env = process.env;
const tp  = env.TERM_PROGRAM || '';
// Locally-maintained allowlist of TERM_PROGRAM values whose terminals ship OSC 8
// support. This is a compatibility assumption, NOT a mirror of Claude Code's
// internal detection — verify against current Claude Code behavior if a link
// fails to render. When unsure for a given terminal, set FORCE_HYPERLINK=1.
// (Apple_Terminal / Terminal.app deliberately omitted: no verified OSC 8 support.)
const HYPERLINK_TPS = new Set([
  'iTerm.app', 'iTerm2',
  'WezTerm',
  'vscode',
  'ghostty',
  'Hyper',
]);
const hyperlinks = !!(
  env.FORCE_HYPERLINK ||
  env.WT_SESSION ||                             // Windows Terminal
  env.KITTY_WINDOW_ID ||                        // Kitty
  env.VTE_VERSION ||                            // GNOME Terminal etc.
  env.ALACRITTY_WINDOW_ID ||                    // Alacritty
  env.LC_TERMINAL === 'iTerm2' ||
  env.TERM?.includes('kitty') ||
  HYPERLINK_TPS.has(tp)
);

// OSC 8: ESC ] 8 ;; URL BEL  TEXT  ESC ] 8 ;; BEL
// Both url and label are sanitized — they carry repo-controlled data.
function link(url, label) {
  label = cleanText(label);
  url   = cleanText(url);
  if (!hyperlinks) return label;
  return `\x1b]8;;${url}\x07${label}\x1b]8;;\x07`;
}

// Canonical file:// URL for cwd via Node's pathToFileURL — correctly percent-
// encodes spaces, #, %, ?, and non-ASCII, and emits the right drive-letter form
// on Windows. Trailing path.sep marks it a directory (RFC 8089).
let cwdUrl = '';
if (cwd) {
  try { cwdUrl = pathToFileURL(path.resolve(cwd) + path.sep).href; }
  catch { cwdUrl = ''; }
}

// ── Assemble output ──────────────────────────────────────────────────────────
// Repo label is always `owner/repo`; branch lives in its own ⎇ chip so each
// click target is unambiguous.
// link() sanitizes its inputs; the non-hyperlink branches emit raw repo-controlled
// strings, so cleanText() them here too. Nothing reaches stdout unscrubbed.
const cwdField    = cwdUrl ? link(cwdUrl, cwdShort) : cleanText(cwdShort);
const repoField   = repoSlug
  ? (hyperlinks ? link(repoUrl, repoSlug) : cleanText(repoUrl))
  : '';
const branchField = branchName
  ? (hyperlinks
      ? `⎇ ${link(branchUrl, branchName)}`
      : `⎇ ${cleanText(branchName)} ${cleanText(branchUrl)}`)
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
