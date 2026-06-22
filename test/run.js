#!/usr/bin/env node
// Test runner for claude-statusline. Zero dependencies — runs each scenario
// as a child node invocation against fixture stdin payloads, then asserts on
// stdout. Bails on first failure with full context.

'use strict';

const cp   = require('child_process');
const fs   = require('fs');
const os   = require('os');
const path = require('path');

const SCRIPT = path.join(__dirname, '..', 'statusline.js');

let passed = 0;
let failed = 0;
const failures = [];

function run(payload, env = {}) {
  // Always strip the ambient hyperlink-flipping vars unless the test sets them.
  const cleanEnv = { ...process.env, ...env };
  for (const k of ['FORCE_HYPERLINK', 'WT_SESSION', 'KITTY_WINDOW_ID',
                   'VTE_VERSION', 'ALACRITTY_WINDOW_ID', 'TERM_PROGRAM',
                   'LC_TERMINAL']) {
    if (!(k in env)) delete cleanEnv[k];
  }
  const r = cp.spawnSync('node', [SCRIPT], {
    input: typeof payload === 'string' ? payload : JSON.stringify(payload),
    env: cleanEnv,
    encoding: 'utf8',
  });
  return { stdout: r.stdout, stderr: r.stderr, status: r.status };
}

function test(name, fn) {
  try {
    fn();
    passed++;
    process.stdout.write(`  ok   ${name}\n`);
  } catch (e) {
    failed++;
    failures.push({ name, error: e });
    process.stdout.write(`  FAIL ${name}\n`);
  }
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg || 'assertion failed');
}
function assertEq(actual, expected, msg) {
  if (actual !== expected) {
    throw new Error(`${msg || 'expected equal'}\n  expected: ${JSON.stringify(expected)}\n  actual:   ${JSON.stringify(actual)}`);
  }
}
function assertMatch(actual, re, msg) {
  if (!re.test(actual)) {
    throw new Error(`${msg || 'expected match'}\n  pattern: ${re}\n  actual:  ${JSON.stringify(actual)}`);
  }
}
function assertNotMatch(actual, re, msg) {
  if (re.test(actual)) {
    throw new Error(`${msg || 'expected NOT to match'}\n  pattern: ${re}\n  actual:  ${JSON.stringify(actual)}`);
  }
}

// ── Fixture: a fresh git repo with origin set ────────────────────────────────
function gitFixture({ remote = 'https://github.com/owner/repo.git', branch = 'main', detached = false } = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'cs-test-'));
  cp.execFileSync('git', ['init', '-q', '-b', 'main'], { cwd: dir });
  cp.execFileSync('git', ['config', 'user.email', 't@t'], { cwd: dir });
  cp.execFileSync('git', ['config', 'user.name', 't'], { cwd: dir });
  cp.execFileSync('git', ['commit', '--allow-empty', '-qm', 'init'], { cwd: dir });
  cp.execFileSync('git', ['remote', 'add', 'origin', remote], { cwd: dir });
  if (branch !== 'main') {
    cp.execFileSync('git', ['checkout', '-qb', branch], { cwd: dir });
  }
  if (detached) {
    const sha = cp.execFileSync('git', ['rev-parse', 'HEAD'], { cwd: dir, encoding: 'utf8' }).trim();
    cp.execFileSync('git', ['checkout', '-q', '--detach', sha], { cwd: dir });
  }
  return dir;
}

function cleanup(dir) {
  try { fs.rmSync(dir, { recursive: true, force: true }); } catch {}
}

const stdoutAsForwardSlash = (s) => s.replace(/\\/g, '/');

// ── Tests ────────────────────────────────────────────────────────────────────

process.stdout.write('claude-statusline tests\n');

test('empty stdin degrades visibly to [ctx —], no fabricated default', () => {
  const { stdout, status } = run('');
  assertEq(status, 0);
  assertEq(stdout, '[ctx —]\n');
});

test('malformed JSON renders gracefully (visible degrade, not [0/200k])', () => {
  const { stdout, status } = run('not json {{');
  assertEq(status, 0);
  assertEq(stdout, '[ctx —]\n');
});

test('context_window with neither tokens nor percentage -> [ctx —]', () => {
  const { stdout } = run({ context_window: { resets_at: 'whatever' } });
  assertMatch(stdout, /\[ctx —\]/, 'unknown context shape must degrade visibly');
  assertNotMatch(stdout, /\[0\/200k\]/, 'must never fabricate the 200k default');
});

test('context_window.used_percentage (documented contract) -> [N%]', () => {
  const { stdout } = run({ context_window: { used_percentage: 42.7, remaining_percentage: 57.3 } });
  assertMatch(stdout, /\[43%\]/, 'rounds used_percentage');
});

test('context_window.remaining_percentage only -> derived [N%]', () => {
  const { stdout } = run({ context_window: { remaining_percentage: 90 } });
  assertMatch(stdout, /\[10%\]/, 'derives used from remaining');
});

test('token formatting: k and M suffixes (when raw counts present)', () => {
  const r1 = run({ context_window: { total_input_tokens: 12500, context_window_size: 200000 } });
  assertMatch(r1.stdout, /\[13k\/200k\]/);
  const r2 = run({ context_window: { total_input_tokens: 750000, context_window_size: 1000000 } });
  assertMatch(r2.stdout, /\[750k\/1M\]/);
  const r3 = run({ context_window: { total_input_tokens: 1_200_000, context_window_size: 1_000_000 } });
  assertMatch(r3.stdout, /\[1\.2M\/1M\]/);
});

test('raw token counts win over percentage when both present', () => {
  const { stdout } = run({ context_window: { total_input_tokens: 50000, context_window_size: 200000, used_percentage: 25 } });
  assertMatch(stdout, /\[50k\/200k\]/, 'prefers exact token form');
  assertNotMatch(stdout, /\[25%\]/);
});

test('token formatting: 999_999 promotes to 1M (no "1000k" seam)', () => {
  const { stdout } = run({ context_window: { total_input_tokens: 999999, context_window_size: 1000000 } });
  assertMatch(stdout, /\[1M\/1M\]/);
  assertNotMatch(stdout, /1000k/);
});

test('percentage clamps to documented 0–100 range', () => {
  assertMatch(run({ context_window: { used_percentage: -5 } }).stdout, /\[0%\]/, 'negative -> 0%');
  assertMatch(run({ context_window: { used_percentage: 150 } }).stdout, /\[100%\]/, 'over -> 100%');
});

test('model slug strips claude- prefix, date suffix, and [tag]', () => {
  const r1 = run({ model: { id: 'claude-opus-4-7' } });
  assertMatch(r1.stdout, /(^|\s)opus-4\.7(\s|$)/);
  const r2 = run({ model: { id: 'claude-sonnet-4-6-20251001' } });
  assertMatch(r2.stdout, /(^|\s)sonnet-4\.6(\s|$)/);
  const r3 = run({ model: { id: 'claude-opus-4-7[1m]' } });
  assertMatch(r3.stdout, /(^|\s)opus-4\.7(\s|$)/);
});

test('tilde-collapses HOME in cwd', () => {
  const home = os.homedir();
  const cwd = path.join(home, 'somewhere');
  const { stdout } = run({ workspace: { current_dir: cwd } });
  assertMatch(stdoutAsForwardSlash(stdout), /^~\/somewhere\//);
});

test('cwd outside HOME stays absolute', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/elsewhere/project' } });
  assertMatch(stdoutAsForwardSlash(stdout), /^D:\/elsewhere\/project\//);
});

test('cwd gets trailing slash', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/proj' } });
  assertMatch(stdout, /^D:\/proj\/ /);
});

test('github https remote on main: bare URL + ⎇ main chip', () => {
  const dir = gitFixture({ remote: 'https://github.com/owner/repo.git', branch: 'main' });
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertMatch(stdout, /https:\/\/github\.com\/owner\/repo /, 'bare repo URL present');
    assertNotMatch(stdout, /\/tree\/main /, 'no /tree/main on the repo link');
    assertMatch(stdout, /⎇ /, 'branch chip rendered');
    assertMatch(stdout, /\/tree\/main\b/, 'branch chip targets /tree/main');
  } finally { cleanup(dir); }
});

test('github ssh remote: normalised to https', () => {
  const dir = gitFixture({ remote: 'git@github.com:owner/repo.git', branch: 'main' });
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertMatch(stdout, /https:\/\/github\.com\/owner\/repo/);
    assertNotMatch(stdout, /git@github\.com/);
  } finally { cleanup(dir); }
});

test('feature branch: /tree/<branch> URL in chip', () => {
  const dir = gitFixture({ branch: 'feature/cool-thing' });
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertMatch(stdout, /\/tree\/feature\/cool-thing/);
  } finally { cleanup(dir); }
});

test('branch URL escapes URL-significant chars per segment (# -> %23)', () => {
  const dir = gitFixture({ branch: 'feature/fix#1' });   // # is git-legal, URL-significant
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertMatch(stdout, /\/tree\/feature\/fix%231/, '# encoded, / preserved as path sep');
    assertNotMatch(stdout, /\/tree\/feature\/fix#1/, 'raw # must not reach the URL');
  } finally { cleanup(dir); }
});

test('repo URL percent-encodes an unquoted special char in slug (? -> %3F)', () => {
  // `?` is not a git-config comment char, so git stores it verbatim and the
  // parser accepts it — the slug must still be URL-encoded, like the branch.
  const dir = gitFixture({ remote: 'https://github.com/owner/re?po.git', branch: 'main' });
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertMatch(stdout, /github\.com\/owner\/re%3Fpo\b/, '? encoded to %3F in URL target');
    assertNotMatch(stdout, /github\.com\/owner\/re\?po/, 'no raw ? in URL target');
  } finally { cleanup(dir); }
});

test('remote URL with a space: slug space encoded to %20 (valid URL, no raw space)', () => {
  // git stores a space-bearing URL verbatim (no quoting — only #/; trigger that),
  // so the parser sees a raw space and the slug encoder must turn it into %20.
  const dir = gitFixture({ remote: 'https://github.com/ow ner/repo.git', branch: 'main' });
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertMatch(stdout, /github\.com\/ow%20ner\/repo\b/, 'space -> %20 in URL target');
    assertNotMatch(stdout, /github\.com\/ow ner/, 'no raw space in URL target');
  } finally { cleanup(dir); }
});

test('remote URL git-quotes a "#" -> no repo chip (safe degrade boundary)', () => {
  // `#` is a git-config comment char, so git wraps the value in quotes. The
  // parser sees a leading `"`, fails the https://github.com/ prefix check, and
  // emits no chip rather than a malformed hyperlink. Documented boundary.
  const dir = gitFixture({ remote: 'https://github.com/owner/re#po.git', branch: 'main' });
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertNotMatch(stdout, /github\.com/, 'quoted remote must not surface as a chip');
  } finally { cleanup(dir); }
});

test('control chars in remote URL: no stray ESC/BEL outside OSC 8 framing', () => {
  const dir = gitFixture({ remote: 'https://github.com/owner/repo.git', branch: 'main' });
  try {
    const cfgPath = path.join(dir, '.git', 'config');
    const cfg = fs.readFileSync(cfgPath, 'utf8')
      .replace('https://github.com/owner/repo.git', 'https://github.com/owner/re\x1b]8;;evilpo.git');
    fs.writeFileSync(cfgPath, cfg);
    const { stdout } = run({ workspace: { current_dir: dir } }, { FORCE_HYPERLINK: '1' });
    // Strip our own legitimate OSC 8 framing; nothing with ESC/BEL may remain.
    const stripped = stdout.replace(/\x1b\]8;;[^\x07]*\x07/g, '');
    assertNotMatch(stripped, /[\x1b\x07]/, `stray ESC/BEL leaked: ${JSON.stringify(stdout)}`);
  } finally { cleanup(dir); }
});

test('control chars in branch (crafted HEAD): no stray ESC/BEL outside framing', () => {
  // git forbids control chars in real refnames, so simulate a hostile .git/HEAD.
  const dir = gitFixture({ remote: 'https://github.com/owner/repo.git', branch: 'main' });
  try {
    fs.writeFileSync(path.join(dir, '.git', 'HEAD'), 'ref: refs/heads/ev\x1bil\x07\n');
    const { stdout } = run({ workspace: { current_dir: dir } }, { FORCE_HYPERLINK: '1' });
    const stripped = stdout.replace(/\x1b\]8;;[^\x07]*\x07/g, '');
    assertNotMatch(stripped, /[\x1b\x07]/, `stray ESC/BEL leaked: ${JSON.stringify(stdout)}`);
  } finally { cleanup(dir); }
});

test('detached HEAD: shows HEAD @<sha7> linking to /commit/', () => {
  const dir = gitFixture({ detached: true });
  try {
    const { stdout } = run({ workspace: { current_dir: dir } });
    assertMatch(stdout, /HEAD @[0-9a-f]{7}/);
    assertMatch(stdout, /\/commit\/[0-9a-f]{7,40}/);
  } finally { cleanup(dir); }
});

test('OSC 8 disabled when no hyperlink env present', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/x' } });
  assertNotMatch(stdout, /\x1b\]8/, 'no OSC 8 sequences');
});

test('OSC 8 enabled with FORCE_HYPERLINK=1', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/x' } }, { FORCE_HYPERLINK: '1' });
  assertMatch(stdout, /\x1b\]8;;file:\/\/\/D:\/x\/\x07/);
});

test('file URL percent-encodes spaces, #, and unicode in cwd', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/my repo#1/café' } }, { FORCE_HYPERLINK: '1' });
  // space -> %20, # -> %23, é -> %C3%A9 ; no raw space/# survives in the URL target
  assertMatch(stdout, /file:\/\/\/D:\/my%20repo%231\/caf%C3%A9\//, 'canonical percent-encoding');
  assertNotMatch(stdout, /file:[^\x07]* repo/, 'no raw space in URL target');
});

test('control chars in cwd are stripped (no terminal injection)', () => {
  // A hostile path tries to smuggle BEL + its own OSC 8 sequence. The only raw
  // ESC/BEL bytes that may reach the terminal are this script's OWN OSC 8
  // framing: ESC ]8;; URL BEL  label  ESC ]8;; BEL  → exactly 2 ESC, 2 BEL.
  // The payload's ESC/BEL must be stripped (label) or %-encoded (URL), so any
  // residual "]8;;evil" is inert plaintext, never an active escape sequence.
  const evil = 'D:/x\x07\x1b]8;;evil\x07';
  const { stdout } = run({ workspace: { current_dir: evil } }, { FORCE_HYPERLINK: '1' });
  const escCount = (stdout.match(/\x1b/g) || []).length;
  const belCount = (stdout.match(/\x07/g) || []).length;
  assertEq(escCount, 2, `payload ESC leaked: ${JSON.stringify(stdout)}`);
  assertEq(belCount, 2, `payload BEL leaked: ${JSON.stringify(stdout)}`);
});

test('control chars stripped even when hyperlinks are OFF', () => {
  const evil = 'D:/x\x1b[31mRED';   // raw CSI color injection attempt
  const { stdout } = run({ workspace: { current_dir: evil } });
  assertNotMatch(stdout, /\x1b\[31m/, 'CSI sequence must not reach stdout');
});

test('OSC 8 enabled with WT_SESSION (Windows Terminal)', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/x' } }, { WT_SESSION: 'abc' });
  assertMatch(stdout, /\x1b\]8;;/);
});

test('OSC 8 enabled with TERM_PROGRAM=Hyper', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/x' } }, { TERM_PROGRAM: 'Hyper' });
  assertMatch(stdout, /\x1b\]8;;/);
});

test('OSC 8 enabled with ALACRITTY_WINDOW_ID', () => {
  const { stdout } = run({ workspace: { current_dir: 'D:/x' } }, { ALACRITTY_WINDOW_ID: '1' });
  assertMatch(stdout, /\x1b\]8;;/);
});

test('worktree: config + branch resolved via commondir', () => {
  const main = gitFixture({ remote: 'https://github.com/owner/repo.git', branch: 'main' });
  const wt   = fs.mkdtempSync(path.join(os.tmpdir(), 'cs-wt-')) + '-x';
  try {
    cp.execFileSync('git', ['worktree', 'add', wt, '-b', 'wt-branch'], { cwd: main });
    const { stdout } = run({ workspace: { current_dir: wt } });
    assertMatch(stdout, /https:\/\/github\.com\/owner\/repo/, 'origin URL found via commondir');
    assertMatch(stdout, /\/tree\/wt-branch/, 'worktree branch reflected');
  } finally {
    try { cp.execFileSync('git', ['worktree', 'remove', '--force', wt], { cwd: main }); } catch {}
    cleanup(main);
    cleanup(wt);
  }
});

test('no leading space when cwd is empty', () => {
  const { stdout } = run({ model: { id: 'claude-opus-4-7' } });
  // first char must not be a space
  assert(stdout[0] !== ' ', `got leading space: ${JSON.stringify(stdout)}`);
});

// ── Installer behaviour ──────────────────────────────────────────────────────

function runInstaller(args, env = {}) {
  return cp.spawnSync('node', [path.join(__dirname, '..', 'cli.js'), ...args], {
    env: { ...process.env, ...env },
    encoding: 'utf8',
  });
}

test('installer refuses to clobber a foreign statusLine without --force', () => {
  const settings = path.join(os.tmpdir(), `cs-inst-${Date.now()}-${Math.random()}.json`);
  const dest     = path.join(os.tmpdir(), `cs-dest-${Date.now()}.js`);
  try {
    fs.writeFileSync(settings, JSON.stringify({
      statusLine: { type: 'command', command: 'bash ~/my-cool-statusline.sh' }
    }, null, 2));

    const r = runInstaller(['install', '--settings', settings, '--dest', dest]);
    assertEq(r.status, 2, 'should exit 2 to signal refused-to-overwrite');
    assertMatch(r.stderr, /Refusing to overwrite/);
    // Settings file unchanged
    const after = JSON.parse(fs.readFileSync(settings, 'utf8'));
    assertEq(after.statusLine.command, 'bash ~/my-cool-statusline.sh');
  } finally {
    try { fs.unlinkSync(settings); } catch {}
    try { fs.unlinkSync(dest); } catch {}
  }
});

test('installer refuses a FOREIGN */statusline.js (basename is not identity)', () => {
  const settings = path.join(os.tmpdir(), `cs-inst-${Date.now()}-${Math.random()}.json`);
  const dest     = path.join(os.tmpdir(), `cs-dest-${Date.now()}.js`);
  try {
    // An unrelated tool whose script happens to be named statusline.js.
    fs.writeFileSync(settings, JSON.stringify({
      statusLine: { type: 'command', command: 'python /opt/someoneelse/statusline.js --their-flag' }
    }, null, 2));

    const r = runInstaller(['install', '--settings', settings, '--dest', dest]);
    assertEq(r.status, 2, 'must refuse (exit 2), not silently clobber a foreign statusline.js');
    assertMatch(r.stderr, /Refusing to overwrite/);
    const after = JSON.parse(fs.readFileSync(settings, 'utf8'));
    assertEq(after.statusLine.command, 'python /opt/someoneelse/statusline.js --their-flag', 'foreign entry untouched');
  } finally {
    try { fs.unlinkSync(settings); } catch {}
    try { fs.unlinkSync(dest); } catch {}
  }
});

test('installer with --force backs up foreign statusLine to statusLineBackup', () => {
  const settings = path.join(os.tmpdir(), `cs-inst-${Date.now()}-${Math.random()}.json`);
  const dest     = path.join(os.tmpdir(), `cs-dest-${Date.now()}.js`);
  try {
    const original = { type: 'command', command: 'bash ~/my-cool-statusline.sh' };
    fs.writeFileSync(settings, JSON.stringify({ statusLine: original }, null, 2));

    const r = runInstaller(['install', '--settings', settings, '--dest', dest, '--force']);
    assertEq(r.status, 0);
    const after = JSON.parse(fs.readFileSync(settings, 'utf8'));
    assertEq(after.statusLineBackup.command, 'bash ~/my-cool-statusline.sh');
    assert(after.statusLine.command.endsWith(dest.replace(/\\/g, '/')),
           `expected our command, got: ${after.statusLine.command}`);
  } finally {
    try { fs.unlinkSync(settings); } catch {}
    try { fs.unlinkSync(dest); } catch {}
  }
});

test('uninstall restores statusLineBackup if present', () => {
  const settings = path.join(os.tmpdir(), `cs-inst-${Date.now()}-${Math.random()}.json`);
  const dest     = path.join(os.tmpdir(), `cs-dest-${Date.now()}.js`);
  try {
    fs.writeFileSync(settings, JSON.stringify({
      statusLine:       { type: 'command', command: `node ${dest.replace(/\\/g, '/')}` },
      statusLineBackup: { type: 'command', command: 'bash ~/original.sh' },
    }, null, 2));
    // Make the dest exist so removeCopiedScript has something to delete.
    fs.writeFileSync(dest, '#!/usr/bin/env node\n');

    const r = runInstaller(['uninstall', '--settings', settings, '--dest', dest]);
    assertEq(r.status, 0);
    const after = JSON.parse(fs.readFileSync(settings, 'utf8'));
    assertEq(after.statusLine.command, 'bash ~/original.sh');
    assert(!('statusLineBackup' in after), 'backup should be consumed');
    assertMatch(r.stdout, /restored previous statusLine/);
  } finally {
    try { fs.unlinkSync(settings); } catch {}
    try { fs.unlinkSync(dest); } catch {}
  }
});

test('installer is idempotent (no changes when already wired)', () => {
  const settings = path.join(os.tmpdir(), `cs-inst-${Date.now()}-${Math.random()}.json`);
  const dest     = path.join(os.tmpdir(), `cs-dest-${Date.now()}.js`);
  try {
    runInstaller(['install', '--settings', settings, '--dest', dest]);
    const first = fs.readFileSync(settings, 'utf8');
    const r2 = runInstaller(['install', '--settings', settings, '--dest', dest]);
    assertEq(r2.status, 0);
    const second = fs.readFileSync(settings, 'utf8');
    assertEq(first, second, 'settings.json content should be unchanged on re-run');
    assertMatch(r2.stdout, /already up to date/);
  } finally {
    try { fs.unlinkSync(settings); } catch {}
    try { fs.unlinkSync(dest); } catch {}
  }
});

test('installer preserves all other settings (env, permissions, etc.)', () => {
  const settings = path.join(os.tmpdir(), `cs-inst-${Date.now()}-${Math.random()}.json`);
  const dest     = path.join(os.tmpdir(), `cs-dest-${Date.now()}.js`);
  try {
    fs.writeFileSync(settings, JSON.stringify({
      theme: 'dark',
      permissions: { defaultMode: 'bypassPermissions' },
      env: { OTEL_TRACES_EXPORTER: 'otlp', FORCE_HYPERLINK: 'existing-value' },
      enabledPlugins: { foo: true },
    }, null, 2));
    runInstaller(['install', '--settings', settings, '--dest', dest]);
    const after = JSON.parse(fs.readFileSync(settings, 'utf8'));
    assertEq(after.theme, 'dark');
    assertEq(after.permissions.defaultMode, 'bypassPermissions');
    assertEq(after.env.OTEL_TRACES_EXPORTER, 'otlp');
    assertEq(after.env.FORCE_HYPERLINK, 'existing-value', 'should not overwrite existing FORCE_HYPERLINK');
    assertEq(after.enabledPlugins.foo, true);
    assert(after.statusLine.command.includes(dest.replace(/\\/g, '/')),
           `statusLine should point at dest, got: ${JSON.stringify(after.statusLine)}`);
  } finally {
    try { fs.unlinkSync(settings); } catch {}
    try { fs.unlinkSync(dest); } catch {}
  }
});

test('STATUSLINE_DEBUG=0 does not log; =1 logs', () => {
  // Redirect HOME/USERPROFILE so we never touch the real ~/.claude log.
  const fakeHome = fs.mkdtempSync(path.join(os.tmpdir(), 'cs-home-'));
  const logPath  = path.join(fakeHome, '.claude', 'statusline-debug.log');
  try {
    run({ cwd: 'D:/x' }, { STATUSLINE_DEBUG: '0', HOME: fakeHome, USERPROFILE: fakeHome });
    assert(!fs.existsSync(logPath), 'STATUSLINE_DEBUG=0 must NOT write a log');

    run({ cwd: 'D:/x' }, { STATUSLINE_DEBUG: '1', HOME: fakeHome, USERPROFILE: fakeHome });
    assert(fs.existsSync(logPath), 'STATUSLINE_DEBUG=1 must create the log (dir auto-created)');
  } finally { cleanup(fakeHome); }
});

test('process exits 0 on every fixture (clean termination)', () => {
  for (const fixture of [
    '',
    '{}',
    '{"cwd":"D:/x"}',
    'garbage',
  ]) {
    const r = run(fixture);
    assertEq(r.status, 0, `non-zero exit for input: ${fixture}`);
  }
});

// ── Report ───────────────────────────────────────────────────────────────────

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) {
  process.stdout.write('\nFailures:\n');
  for (const f of failures) {
    process.stdout.write(`\n  ${f.name}\n`);
    process.stdout.write(String(f.error.stack || f.error.message).split('\n').map(l => '    ' + l).join('\n') + '\n');
  }
  process.exit(1);
}
