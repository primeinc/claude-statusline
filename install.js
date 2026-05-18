#!/usr/bin/env node
// claude-statusline installer.
// Wires this package's statusline.js into ~/.claude/settings.json.
//
// Usage:
//   npx @primeinc/claude-statusline install            # add / replace
//   npx @primeinc/claude-statusline install --print    # show what would change
//   npx @primeinc/claude-statusline install --uninstall  # remove our entry
//   npx @primeinc/claude-statusline install --settings /custom/path/settings.json

'use strict';

const fs   = require('fs');
const os   = require('os');
const path = require('path');

const args = process.argv.slice(2);
const flag = (n) => args.includes(n);
const valueOf = (n) => {
  const i = args.indexOf(n);
  return i >= 0 && i + 1 < args.length ? args[i + 1] : null;
};

const PRINT_ONLY = flag('--print') || flag('-n');
const UNINSTALL  = flag('--uninstall');
const HELP       = flag('--help') || flag('-h');

const SETTINGS_PATH = valueOf('--settings')
  || process.env.CLAUDE_SETTINGS
  || path.join(os.homedir(), '.claude', 'settings.json');

const SCRIPT_PATH = path.join(__dirname, 'statusline.js').replace(/\\/g, '/');
const COMMAND     = `node ${SCRIPT_PATH}`;

if (HELP) {
  process.stdout.write(`claude-statusline installer

Wires this package's statusline.js into ~/.claude/settings.json so Claude Code
runs it as the statusline command. Idempotent — re-running just updates the
path. Preserves all other settings.

Options:
  --print, -n            Print what would change, don't write
  --uninstall            Remove our statusLine entry (preserves other settings)
  --settings <path>      Use a non-default settings.json location
                         (env: CLAUDE_SETTINGS)
  --help, -h             This message

Settings file: ${SETTINGS_PATH}
Statusline:    ${COMMAND}
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

const { existed, data } = readSettings();
const before = JSON.stringify(data, null, 2);

if (UNINSTALL) {
  let removed = false;
  if (data.statusLine && data.statusLine.type === 'command'
      && typeof data.statusLine.command === 'string'
      && data.statusLine.command.includes('statusline.js')) {
    delete data.statusLine;
    removed = true;
  }
  // Don't touch the env block — FORCE_HYPERLINK may be wanted independently.
  if (!removed) {
    process.stdout.write('No claude-statusline statusLine entry found. Nothing to remove.\n');
    process.exit(0);
  }
} else {
  data.statusLine = { type: 'command', command: COMMAND };
  // Add FORCE_HYPERLINK only if not already set — preserves user's value.
  data.env = data.env || {};
  if (!('FORCE_HYPERLINK' in data.env)) {
    data.env.FORCE_HYPERLINK = '1';
  }
}

const after = JSON.stringify(data, null, 2);

if (PRINT_ONLY) {
  process.stdout.write(`Settings file: ${SETTINGS_PATH}${existed ? '' : ' (would be created)'}\n`);
  process.stdout.write('--- would write ---\n');
  process.stdout.write(after + '\n');
  process.exit(0);
}

if (before === after) {
  process.stdout.write(`No changes needed. ${SETTINGS_PATH} already wired.\n`);
  process.exit(0);
}

writeSettings(data);

if (UNINSTALL) {
  process.stdout.write(`Removed claude-statusline from ${SETTINGS_PATH}.\n`);
  process.stdout.write('Restart Claude Code for changes to take effect.\n');
} else {
  process.stdout.write(`${existed ? 'Updated' : 'Created'} ${SETTINGS_PATH}.\n`);
  process.stdout.write(`statusLine.command = ${COMMAND}\n`);
  if (!('FORCE_HYPERLINK' in (data.env || {}))) {
    // shouldn't reach here, just for safety
  }
  process.stdout.write('Restart Claude Code for changes to take effect.\n');
}
