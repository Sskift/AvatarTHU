'use strict';
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const {watchState} = require('../state.cjs');

test('state watcher survives atomic replacements and late-created task directories without a health feedback loop', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'avatar-watch-'));
  let changes = 0;
  const watcher = watchState(root, () => changes++);
  async function waitForChange(before) {
    for (let i = 0; changes <= before && i < 100; i++) await new Promise(resolve => setTimeout(resolve, 20));
    assert.ok(changes > before, 'state change was not delivered');
  }
  async function replace(file, content) {
    const before = changes;
    await fs.writeFile(file + '.tmp', JSON.stringify(content));
    await fs.rename(file + '.tmp', file);
    await waitForChange(before);
  }
  try {
    await replace(path.join(root, 'config.json'), {writer: 'codex'});
    await replace(path.join(root, 'config.json'), {writer: 'claude'});
    let before = changes;
    await fs.mkdir(path.join(root, 'data')); await waitForChange(before);
    before = changes;
    await fs.mkdir(path.join(root, 'data/tasks')); await waitForChange(before);
    const task = path.join(root, 'data/tasks/0123456789abcdef.json');
    await replace(task, {status: 'working'});
    await replace(task, {status: 'awaiting'});
    // Let platform-specific duplicate rename events settle before the negative check.
    await new Promise(resolve => setTimeout(resolve, 200)); before = changes;
    await fs.writeFile(path.join(root, 'data/menubar-health.json'), '{"state":"running"}');
    await new Promise(resolve => setTimeout(resolve, 200));
    assert.equal(changes, before, 'desktop health triggered its own refresh');
  } finally { watcher.close(); await fs.rm(root, {recursive: true, force: true}); }
});
