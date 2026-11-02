'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');

const origin = 'avatarthu://workspace';
const actions = Object.freeze({
  start: ['service', 'start'], stop: ['service', 'stop'],
  doctor: ['doctor'], keepalive: ['keepalive', 'run'],
  'retry-codex': ['retry', '--tool', 'codex'], 'retry-claude': ['retry', '--tool', 'claude'],
  'reconnect-lark': ['reconnect', 'lark'], login: ['login', 'thu']
});
function internalURL(raw) {
  try { const u = new URL(raw); return u.protocol === 'avatarthu:' && u.hostname === 'workspace' && !u.username && !u.password && !u.port; } catch { return false; }
}
function appPage(raw) {
  if (!internalURL(raw)) return false;
  return ['/', '/editor', '/offline'].includes(new URL(raw).pathname);
}
function externalURL(raw) {
  try { const u = new URL(raw); return u.protocol === 'https:' && !u.username && !u.password; } catch { return false; }
}
function route(args) {
  const view = args[args.indexOf('--view') + 1];
  const task = args[args.indexOf('--task') + 1];
  const id = args.includes('--task') && /^[a-f0-9]{16}$/.test(task) ? task : '';
  if (args.includes('--editor')) return '/editor' + (id ? '#task=' + id : '');
  const selected = ['overview', 'courses', 'files', 'artifacts', 'settings'].includes(view) ? view : 'overview';
  return '/#' + new URLSearchParams(id ? {view: 'task', id} : {view: selected});
}
async function endpoint(root) {
  const value = JSON.parse(await fs.readFile(path.join(root, 'data', 'editor-endpoint.json'), 'utf8'));
  const match = /^127\.0\.0\.1:(\d+)$/.exec(value.host || '');
  if (!match || Number(match[1]) < 1 || Number(match[1]) > 65535 || !/^[a-f0-9]{64}$/.test(value.token || '')) throw new Error('后台连接尚未就绪');
  return value;
}
async function backend(root, pathname, options = {}) {
  if (!pathname.startsWith('/') || pathname.startsWith('//')) throw new Error('无效请求');
  const ep = await endpoint(root);
  const base = 'http://' + ep.host;
  const headers = new Headers(options.headers);
  headers.set('Cookie', 'avatarthu_editor_' + ep.host.replaceAll(':', '_') + '=' + ep.token);
  headers.set('Origin', base);
  return fetch(base + pathname, {...options, headers, cache: 'no-store', redirect: 'error', signal: options.signal || AbortSignal.timeout(25000)});
}
function safeOutput(value) {
  return String(value).replace(/https?:\/\/\S+/g, '[链接已隐藏]').replace(/(?:Bearer\s+|sk-)[A-Za-z0-9_.-]+/g, '[凭证已隐藏]').slice(-4000);
}
module.exports = {origin, actions, internalURL, appPage, externalURL, route, endpoint, backend, safeOutput};
