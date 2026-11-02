'use strict';
const fs = require('node:fs');
const path = require('node:path');

// Watch directories, so Go's atomic JSON replacement does not detach a watcher.
// Do not watch course files/logs or our own health receipt.
function watchState(root, changed) {
  const watchers = new Map();
  let closed = false;
  const locations = [
    [root, name => ['config.json', 'keepalive-status.json', 'data'].includes(name)],
    [path.join(root, 'data'), name => /^(?:daemon-health|actions-health|messages-health|cli-health|claude-execution|codex-execution|workspace-scan|schedule|sync-result|editor-endpoint|lark-reconnect)\.json$/.test(name) || name === 'tasks'],
    [path.join(root, 'data', 'tasks'), name => /^[a-f0-9]{16}\.json$/.test(name)]
  ];
  function rescan() {
    if (closed) return;
    for (const [directory, relevant] of locations) {
      if (watchers.has(directory)) continue;
      try {
        const watcher = fs.watch(directory, (_event, filename) => {
          if (closed || (filename && !relevant(String(filename)))) return;
          rescan(); changed();
        });
        watchers.set(directory, watcher);
        watcher.unref();
        watcher.on('error', () => { watcher.close(); watchers.delete(directory); });
      } catch { /* A fresh install may create data/tasks after the App starts. */ }
    }
  }
  rescan();
  return {rescan, close() { closed = true; for (const watcher of watchers.values()) watcher.close(); watchers.clear(); }};
}
module.exports = {watchState};
