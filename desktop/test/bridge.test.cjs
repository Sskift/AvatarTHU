'use strict';
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const http = require('node:http');
const bridge = require('../bridge.cjs');

test('desktop routes never forward credentials or external navigation', () => {
  assert.equal(bridge.route(['--view', 'settings']), '/#view=settings');
  assert.equal(bridge.route(['--editor', '--task', '0123456789abcdef']), '/editor#task=0123456789abcdef');
  assert.equal(bridge.route(['--view', 'https://untrusted.example', '--task', '../private']), '/#view=overview');
  assert.equal(bridge.appPage('avatarthu://workspace/editor#task=0123456789abcdef'), true);
  for (const value of ['file:///private', 'https://workspace/', 'avatarthu://elsewhere/', 'avatarthu://secret@workspace/', 'avatarthu://workspace/api/task/id/report.html']) assert.equal(bridge.appPage(value), false);
  assert.equal(bridge.externalURL('https://example.org/report'), true);
  for (const value of ['javascript:alert(1)', 'file:///private', 'https://user:secret@example.org', 'http://example.org']) assert.equal(bridge.externalURL(value), false);
  assert.equal(Object.hasOwn(bridge.actions, 'submit'), false);
  assert.equal(Object.hasOwn(bridge.actions, 'run'), false);
});

test('private backend authentication stays outside renderer and rejects non-loopback endpoints', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'avatar-bridge-'));
  const token = 'a'.repeat(64); let received;
  const server = http.createServer((req, res) => { received = {cookie: req.headers.cookie, origin: req.headers.origin, path: req.url}; res.setHeader('Content-Type', 'application/json'); res.end('{"ok":true}'); });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    await fs.mkdir(path.join(root, 'data'));
    const host = '127.0.0.1:' + server.address().port;
    const file = path.join(root, 'data/editor-endpoint.json');
    await fs.writeFile(file, JSON.stringify({host, token}));
    assert.deepEqual(await (await bridge.backend(root, '/api/workspace')).json(), {ok: true});
    assert.equal(received.cookie, 'avatarthu_editor_' + host.replaceAll(':', '_') + '=' + token);
    assert.equal(received.origin, 'http://' + host);
    assert.equal(received.path, '/api/workspace');
    await assert.rejects(bridge.backend(root, '//external.example'));
    for (const bad of ['example.org:80', '127.0.0.1:0', '127.0.0.1:70000', 'user@127.0.0.1:80']) {
      await fs.writeFile(file, JSON.stringify({host: bad, token}));
      await assert.rejects(bridge.endpoint(root));
    }
  } finally { await new Promise(resolve => server.close(resolve)); await fs.rm(root, {recursive: true, force: true}); }
});
