#!/usr/bin/env node
// claude-statusline installer.
//
// Copies statusline.js to a permanent location (default: ~/.claude/statusline.js)
// and wires the path into ~/.claude/settings.json so Claude Code runs it.
//
// Designed to work three ways with the same UX:
//   npx @primeinc/claude-statusline install            # one-shot, copies file
//   npm i -g @primeinc/claude-statusline && claude-statusline-install
//   git clone … && node install.js                     # from source
//
// Re-running is idempotent: overwrites the script (so `npx … install` again
// picks up package updates), preserves all other settings, won't double-add
// the env block.
//
// Flags:
//   --print, -n            Show what would change, write nothing
//   --uninstall            Reverse: remove settings entry, delete script copy
//   --dest <path>          Where to copy statusline.js (default ~/.claude/statusline.js)
//   --settings <path>      Override settings.json location (env: CLAUDE_SETTINGS)
//   --no-copy              Don't copy; point settings.json at the package's own file
//                          (useful for `npm i -g` users who want updates via `npm update`)
//   --help, -h

'use strict';

const fs   = require('fs');
const os   = require('os');
const path = require('path');

const args = process.argv.slice(2);
// Support `install`/`uninstall` as positional verbs for npx UX:
//   npx @primeinc/claude-statusline install
//   npx @primeinc/claude-statusline uninstall
const verb = args.find((a) => !a.startsWith('-'));
const isUninstall = args.includes('--uninstall') || verb === 'uninstall';
const flag = (n) => args.includes(n);
const valueOf = (n) => {
  const i = args.indexOf(n);
  return i >= 0 && i + 1 < args.length ? args[i + 1] : null;
};

const PRINT_ONLY = flag('--print') || flag('-n');
const NO_COPY    = flag('--no-copy');
const HELP       = flag('--help') || flag('-h');

const HOME = os.homedir();
const SETTINGS_PATH = valueOf('--settings')
  || process.env.CLAUDE_SETTINGS
  || path.join(HOME, '.claude', 'settings.json');

const SOURCE_SCRIPT = path.join(__dirname, 'statusline.js');
const DEFAULT_DEST  = path.join(HOME, '.claude', 'statusline.js');
const DEST          = valueOf('--dest') || (NO_COPY ? SOURCE_SCRIPT : DEFAULT_DEST);
const COMMAND       = `node ${DEST.replace(/\\/g, '/')}`;

if (HELP) {
  process.stdout.write(`@primeinc/claude-statusline installer

Copies statusline.js to a permanent location and wires it into
Claude Code's settings.json. Idempotent — safe to re-run for updates.

Usage:
  npx @primeinc/claude-statusline install
  npx @primeinc/claude-statusline uninstall
  npx @primeinc/claude-statusline install --print

Options:
  --print, -n            Dry run — show what would change, write nothing
  --force                Overwrite an existing foreign statusLine entry.
                         The previous value is saved to settings.statusLineBackup
                         and restored on uninstall.
  --uninstall            Remove settings entry; delete the copied script.
                         If statusLineBackup exists, restore it.
                         (equivalent to running with verb \`uninstall\`)
  --dest <path>          Override script destination
                         (default: ${DEFAULT_DEST})
  --settings <path>      Override settings.json location
                         (env: CLAUDE_SETTINGS)
                         (default: ${SETTINGS_PATH})
  --no-copy              Don't copy; point settings.json at the installed
                         package's own statusline.js (good for \`npm i -g\`
                         users who want updates via \`npm update\`)
  --help, -h             This message

Resolved paths:
  source:    ${SOURCE_SCRIPT}
  dest:      ${DEST}
  settings:  ${SETTINGS_PATH}
  command:   ${COMMAND}
`);
  process.exit(0);
}

function readSettings() {
  try {
    const raw = fs.readFileSync(SETTINGS_PATH, 'utf8');
    return { existed: true, data: JSON.parse(raw) };
  } catch (e) {
    if (e.code === 'ENOENT') return { existed: false, data: {} };
    throw new Error(`Cannot read ${SETTINGS_PATH}: ${e.message}`);
  }
}

function writeSettings(data) {
  fs.mkdirSync(path.dirname(SETTINGS_PATH), { recursive: true });
  fs.writeFileSync(SETTINGS_PATH, JSON.stringify(data, null, 2) + '\n');
}

function copyScript() {
  if (NO_COPY) return { copied: false };
  fs.mkdirSync(path.dirname(DEST), { recursive: true });
  fs.copyFileSync(SOURCE_SCRIPT, DEST);
  // Best-effort chmod for non-Windows (no-op on Win32, fine).
  try { fs.chmodSync(DEST, 0o755); } catch {}
  return { copied: true };
}

function removeCopiedScript() {
  if (NO_COPY || DEST === SOURCE_SCRIPT) return { removed: false };
  try {
    fs.unlinkSync(DEST);
    return { removed: true };
  } catch (e) {
    if (e.code === 'ENOENT') return { removed: false };
    throw e;
  }
}

// ── Main ─────────────────────────────────────────────────────────────────────

const { existed, data } = readSettings();
const before = JSON.stringify(data, null, 2);

if (isUninstall) {
  let touched = false;
  const settingsCmd = (data.statusLine && data.statusLine.command || '').replace(/\\/g, '/');
  const destNorm = DEST.replace(/\\/g, '/');
  const sourceNorm = SOURCE_SCRIPT.replace(/\\/g, '/');
  // Match the configured DEST, the package's source, or a "statusline.js"
  // tail — covers users who relocated, used --no-copy, or installed via
  // a different mechanism.
  const isOurs = settingsCmd.includes(destNorm)
              || settingsCmd.includes(sourceNorm)
              || /\/statusline\.js(\s|$)/.test(settingsCmd);
  let restored = false;
  if (data.statusLine && data.statusLine.type === 'command' && isOurs) {
    if (data.statusLineBackup) {
      data.statusLine = data.statusLineBackup;
      delete data.statusLineBackup;
      restored = true;
    } else {
      delete data.statusLine;
    }
    touched = true;
  }
  // Intentionally leave env.FORCE_HYPERLINK alone — user may want it for
  // other reasons (Claude Code's own file-link hyperlinks).
  const after = JSON.stringify(data, null, 2);

  if (PRINT_ONLY) {
    process.stdout.write(`Would write to: ${SETTINGS_PATH}\n--- after ---\n${after}\n`);
    if (!NO_COPY) process.stdout.write(`Would delete:   ${DEST}\n`);
    process.exit(0);
  }

  if (touched) writeSettings(data);
  const { removed } = removeCopiedScript();

  if (!touched && !removed) {
    process.stdout.write('Nothing to uninstall.\n');
  } else {
    process.stdout.write(`Uninstalled.\n`);
    if (touched && restored) {
      process.stdout.write(`  restored previous statusLine from statusLineBackup in ${SETTINGS_PATH}\n`);
    } else if (touched) {
      process.stdout.write(`  removed statusLine from ${SETTINGS_PATH}\n`);
    }
    if (removed) process.stdout.write(`  deleted ${DEST}\n`);
    process.stdout.write('Run `/reload-plugins` in Claude Code (or restart) to pick up the change.\n');
  }
  process.exit(0);
}

// Install path — refuse to clobber a foreign statusLine without --force.
const FORCE = flag('--force');
const existingCmd = (data.statusLine && data.statusLine.command || '').replace(/\\/g, '/');
const destNorm   = DEST.replace(/\\/g, '/');
const sourceNorm = SOURCE_SCRIPT.replace(/\\/g, '/');
const isOurs = !data.statusLine
            || existingCmd.includes(destNorm)
            || existingCmd.includes(sourceNorm)
            || /\/statusline\.js(\s|$)/.test(existingCmd);
const foreign = data.statusLine && !isOurs;

if (foreign && !FORCE) {
  process.stderr.write(`Refusing to overwrite an existing statusLine entry in ${SETTINGS_PATH}:

  current: ${JSON.stringify(data.statusLine)}
  ours:    ${JSON.stringify({ type: 'command', command: COMMAND })}

To replace it, re-run with --force. To keep yours, do nothing.
A copy of the current statusLine will be saved to settings.json under
"statusLineBackup" when --force is used.
`);
  process.exit(2);
}

if (foreign && FORCE) {
  data.statusLineBackup = data.statusLine;
}
data.statusLine = { type: 'command', command: COMMAND };
data.env = data.env || {};
const addedHyperlink = !('FORCE_HYPERLINK' in data.env);
if (addedHyperlink) data.env.FORCE_HYPERLINK = '1';

const after = JSON.stringify(data, null, 2);

if (PRINT_ONLY) {
  process.stdout.write(`source:   ${SOURCE_SCRIPT}\n`);
  process.stdout.write(`dest:     ${DEST}${NO_COPY ? ' (no-copy, in place)' : ''}\n`);
  process.stdout.write(`settings: ${SETTINGS_PATH}${existed ? '' : ' (would be created)'}\n`);
  process.stdout.write(`--- would write ---\n${after}\n`);
  process.exit(0);
}

const { copied } = copyScript();
const settingsChanged = before !== after;
if (settingsChanged) writeSettings(data);

process.stdout.write(`Installed @primeinc/claude-statusline.\n`);
if (copied)            process.stdout.write(`  copied  ${SOURCE_SCRIPT}\n       -> ${DEST}\n`);
if (NO_COPY)           process.stdout.write(`  using   ${SOURCE_SCRIPT} in place\n`);
if (settingsChanged)   process.stdout.write(`  wrote   ${SETTINGS_PATH}\n`);
if (!settingsChanged)  process.stdout.write(`  settings already up to date: ${SETTINGS_PATH}\n`);
if (addedHyperlink)    process.stdout.write(`  added   env.FORCE_HYPERLINK=1 (needed for Windows Terminal)\n`);
if (foreign && FORCE)  process.stdout.write(`  backed up previous statusLine -> settings.statusLineBackup\n`);
process.stdout.write('\nRun `/reload-plugins` in Claude Code (or restart) to pick up the change.\n');
