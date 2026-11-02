'use strict';
const {app, BrowserWindow, Menu, Tray, nativeImage, ipcMain, protocol, session, shell, dialog} = require('electron');
const fs = require('node:fs/promises');
const fsSync = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const {execFile} = require('node:child_process');
const {promisify} = require('node:util');
const run = promisify(execFile);
const bridge = require('./bridge.cjs');
const arg = name => process.argv.includes(name) ? process.argv[process.argv.indexOf(name) + 1] : '';
function defaultRoot() {
  const legacy = path.join(os.homedir(), '.avatarthu');
  if (process.platform === 'win32') return legacy;
  try { if (fsSync.lstatSync(legacy).isDirectory()) return legacy; } catch {}
  return path.join(os.homedir(), '.local/share/avatarthu');
}
const selectedRoot = path.resolve(arg('--home') || process.env.AVATARTHU_HOME || defaultRoot());
const root = fsSync.existsSync(selectedRoot) ? fsSync.realpathSync(selectedRoot) : selectedRoot;
const binary = path.join(root, 'bin', process.platform === 'win32' ? 'avatarthu.exe' : 'avatarthu');
let window, tray, quitting = false, overview, refreshTimer, doctorAt = 0, refreshing = false;
let lastRoute = bridge.route(process.argv);
const controls = new Map();
app.setName('AvatarTHU');
app.setPath('userData', path.join(root, 'desktop'));
protocol.registerSchemesAsPrivileged([{scheme: 'avatarthu', privileges: {standard: true, secure: true, supportFetchAPI: true, corsEnabled: true, stream: true}}]);

async function cli(args, executable = binary) {
  const {stdout, stderr} = await run(executable, args, {env: {...process.env, AVATARTHU_HOME: root}, timeout: args[0] === 'login' ? 300000 : 90000, maxBuffer: 1024 * 1024, windowsHide: true});
  return bridge.safeOutput(stdout || stderr || '已完成');
}
async function control(action) {
  if (!Object.hasOwn(bridge.actions, action)) throw new Error('不支持的操作');
  if (controls.has(action)) return controls.get(action);
  const pending = cli(bridge.actions[action]).then(async text => { await refresh(); return text; }).catch(e => { throw new Error(bridge.safeOutput(e.stderr || e.message)); }).finally(() => controls.delete(action));
  controls.set(action, pending);
  return pending;
}
async function ensureBackend() {
  try { const res = await bridge.backend(root, '/api/workspace/health'); if (res.ok) return; } catch {}
  try { await fs.access(binary); } catch {
    const bundled = path.join(process.resourcesPath, 'backend', path.basename(binary));
    await cli(['init', '--no-login', '--no-start'], bundled);
  }
  await cli(['service', 'start']);
}
async function syncLoginItem() {
  if (process.platform !== 'win32' || !app.isPackaged) return;
  let enabled = true;
  try { enabled = JSON.parse(await fs.readFile(path.join(root, 'config.json'), 'utf8')).menubar_enabled !== false; } catch {}
  const installed = path.join(root, 'apps', 'AvatarTHU', 'AvatarTHU.exe');
  app.setLoginItemSettings({openAtLogin: enabled, path: fsSync.existsSync(installed) ? installed : process.execPath, args: ['--hidden', '--home', root]});
}
function createWindow() {
  window = new BrowserWindow({
    width: 1280, height: 840, minWidth: 960, minHeight: 640, show: false,
    title: 'AvatarTHU', backgroundColor: '#f8f7f5', titleBarStyle: 'hiddenInset',
    ...(process.platform !== 'darwin' ? {titleBarOverlay: {color: '#f8f7f5', symbolColor: '#62576d', height: 40}} : {trafficLightPosition: {x: 20, y: 20}}),
    webPreferences: {preload: path.join(__dirname, 'preload.cjs'), nodeIntegration: false, contextIsolation: true, sandbox: true, webSecurity: true, spellcheck: false}
  });
  window.on('close', event => { if (!quitting) { event.preventDefault(); window.hide(); } });
  window.webContents.on('will-navigate', (event, url) => { if (!bridge.internalURL(url)) { event.preventDefault(); openExternal(url); } });
  window.webContents.on('will-redirect', (event, url) => { if (!bridge.internalURL(url)) event.preventDefault(); });
  window.webContents.setWindowOpenHandler(({url}) => {
    if (bridge.internalURL(url)) { window.loadURL(url).catch(showOffline); }
    else openExternal(url);
    return {action: 'deny'};
  });
  window.webContents.on('will-attach-webview', event => event.preventDefault());
  window.webContents.on('will-prevent-unload', event => {
    const choice = dialog.showMessageBoxSync(window, {type: 'question', message: '报告还有未保存的修改', detail: '留在当前页面，等待保存完成后再切换。', buttons: ['留在此处', '离开页面'], defaultId: 0, cancelId: 0});
    if (choice === 1) event.preventDefault();
  });
  const rememberRoute = url => { if (bridge.appPage(url) && new URL(url).pathname !== '/offline') lastRoute = new URL(url).pathname + new URL(url).hash; };
  window.webContents.on('did-navigate', (_event, url) => rememberRoute(url));
  window.webContents.on('did-navigate-in-page', (_event, url, mainFrame) => { if (mainFrame) rememberRoute(url); });
  window.webContents.on('did-fail-load', (_event, code, _description, url, mainFrame) => { if (mainFrame && code !== -3 && !url.endsWith('/offline')) showOffline(); });
  window.loadURL(bridge.origin + lastRoute).catch(showOffline);
}
function show(route) {
  if (!window || window.isDestroyed()) createWindow();
  if (route && bridge.origin + route !== window.webContents.getURL()) window.loadURL(bridge.origin + route).catch(showOffline);
  if (window.isMinimized()) window.restore();
  app.dock?.show();
  window.show(); window.focus();
}
async function showOffline() { if (window && !window.isDestroyed()) await window.loadURL(bridge.origin + '/offline'); }
async function openExternal(raw) { if (bridge.externalURL(raw)) await shell.openExternal(raw); }
function trayMenu() {
  const tasks = overview?.tasks || [];
  const attention = tasks.filter(t => ['awaiting', 'needs_student', 'failed', 'approval_invalid', 'submission_unknown'].includes(t.status));
  const reviewing = tasks.filter(t => ['awaiting', 'approval_invalid'].includes(t.status)).length;
  if (process.platform === 'darwin') tray.setTitle(reviewing ? String(reviewing) : '');
  tray.setToolTip('AvatarTHU' + (reviewing ? ' · ' + reviewing + ' 份待审阅' : ' · 课程工作台'));
  const entries = [
    {label: '打开 AvatarTHU', click: () => show()},
    {label: overview?.health?.daemon?.phase || '正在连接后台', enabled: false},
    {type: 'separator'},
    ...attention.slice(0, 6).map(t => ({label: t.title, click: () => show('/#' + new URLSearchParams({view: 'task', id: t.id}))})),
    ...(attention.length ? [{type: 'separator'}] : []),
    {label: '课程', click: () => show('/#view=courses')},
    {label: '设置与状态', click: () => show('/#view=settings')},
    {type: 'separator'},
    {label: '退出 AvatarTHU', sublabel: '课程后台继续运行', click: () => app.quit()}
  ];
  const menu = Menu.buildFromTemplate(entries);
  if (process.platform === 'linux') tray.setContextMenu(menu);
  return menu;
}
async function health(state) {
  await fs.mkdir(path.join(root, 'data'), {recursive: true});
  const file = path.join(root, 'data', 'menubar-health.json');
  const tmp = file + '.' + process.pid + '.tmp';
  await fs.writeFile(tmp, JSON.stringify({state, pid: process.pid, time: new Date().toISOString(), app: 'electron', version: app.getVersion()}), {mode: 0o600});
  await fs.rename(tmp, file);
}
async function refresh() {
  if (refreshing) return;
  refreshing = true;
  try {
    const res = await bridge.backend(root, '/api/workspace');
    if (res.ok) overview = await res.json();
    if (Date.now() - doctorAt > 300000) { doctorAt = Date.now(); control('doctor').catch(() => {}); }
  } catch { overview = null; }
  finally { refreshing = false; if (tray && !quitting) trayMenu(); if (!quitting) await health('running').catch(() => {}); }
}
function validateSender(event) {
  if (!window || event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame || !bridge.appPage(event.senderFrame.url)) throw new Error('无效的桌面操作来源');
}

if (!app.requestSingleInstanceLock({route: lastRoute, hidden: process.argv.includes('--hidden')})) {
  app.quit();
} else if (process.argv.includes('--quit')) {
  app.whenReady().then(() => Promise.allSettled([health('stopped'), syncLoginItem()])).finally(() => app.quit());
} else {
  app.on('second-instance', (_event, argv, _cwd, data) => {
    if (argv.includes('--quit')) { app.quit(); return; }
    if (data?.hidden || argv.includes('--hidden')) return;
    const route = data?.route && bridge.internalURL(bridge.origin + data.route) ? data.route : bridge.route(argv);
    if (app.isReady()) show(route); else lastRoute = route;
  });
  app.on('activate', () => { if (app.isReady()) show(); });
  app.on('window-all-closed', () => {});
  app.on('before-quit', event => {
    if (quitting) return;
    event.preventDefault(); quitting = true; clearInterval(refreshTimer);
    Promise.allSettled([health('stopped'), syncLoginItem()]).finally(() => app.quit());
  });
  app.whenReady().then(async () => {
    session.defaultSession.setPermissionRequestHandler((_web, _permission, callback) => callback(false));
    session.defaultSession.setPermissionCheckHandler(() => false);
    session.defaultSession.webRequest.onBeforeRequest({urls: ['http://*/*', 'https://*/*', 'file://*/*', 'ws://*/*', 'wss://*/*']}, (_details, callback) => callback({cancel: true}));
    protocol.handle('avatarthu', async request => {
      if (!bridge.internalURL(request.url) || !['GET', 'POST', 'HEAD'].includes(request.method)) return new Response('Forbidden', {status: 403});
      const url = new URL(request.url);
      if (url.pathname === '/desktop/seal.svg') return new Response(await fs.readFile(path.join(__dirname, 'assets/seal.svg')), {headers: {'Content-Type': 'image/svg+xml'}});
      if (url.pathname === '/offline' || url.pathname === '/offline.js') {
        const name = url.pathname === '/offline' ? 'offline.html' : 'offline.js';
        return new Response(await fs.readFile(path.join(__dirname, name)), {headers: {'Content-Type': name.endsWith('.js') ? 'text/javascript' : 'text/html', 'Content-Security-Policy': "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'"}});
      }
      try {
        const headers = {};
        for (const name of ['content-type', 'range', 'if-range']) if (request.headers.has(name)) headers[name] = request.headers.get(name);
        const result = await bridge.backend(root, url.pathname + url.search, {method: request.method, headers, ...(request.method === 'POST' ? {body: Buffer.from(await request.arrayBuffer())} : {})});
        const outputHeaders = new Headers(result.headers);
        for (const name of ['set-cookie', 'content-encoding', 'content-length']) outputHeaders.delete(name);
        return new Response(result.body, {status: result.status, headers: outputHeaders});
      } catch {
        if (['/', '/editor'].includes(url.pathname)) return Response.redirect(bridge.origin + '/offline');
        return new Response(JSON.stringify({error: '暂时连不上后台，请在设置中启动或重连。'}), {status: 503, headers: {'Content-Type': 'application/json'}});
      }
    });
    ipcMain.handle('avatar:control', (event, action) => { validateSender(event); return control(action); });
    ipcMain.handle('avatar:reveal', async (event, place) => {
      validateSender(event);
      if (!['courses', 'logs'].includes(place)) throw new Error('无效目录');
      const directory = place === 'courses' ? path.join(root, 'data', 'courses') : path.join(root, 'logs');
      await fs.mkdir(directory, {recursive: true});
      const result = await shell.openPath(directory);
      if (result) throw new Error(result);
    });
    ipcMain.handle('avatar:reconnect', async event => { validateSender(event); await ensureBackend(); await window.loadURL(bridge.origin + lastRoute); });
    session.defaultSession.on('will-download', (_event, item) => { item.setSaveDialogOptions({title: '保存文件', defaultPath: path.join(app.getPath('downloads'), path.basename(item.getFilename()))}); });
    Menu.setApplicationMenu(Menu.buildFromTemplate([
      ...(process.platform === 'darwin' ? [{label: 'AvatarTHU', submenu: [{role: 'about'}, {type: 'separator'}, {label: '设置…', accelerator: 'Cmd+,', click: () => show('/#view=settings')}, {type: 'separator'}, {role: 'hide'}, {role: 'hideOthers'}, {role: 'unhide'}, {type: 'separator'}, {role: 'quit'}]}] : []),
      {label: '文件', submenu: [{label: '工作台', accelerator: 'CmdOrCtrl+1', click: () => show('/#view=overview')}, {label: '课程', accelerator: 'CmdOrCtrl+2', click: () => show('/#view=courses')}, {type: 'separator'}, {role: 'close'}]},
      {label: '编辑', submenu: [{role: 'undo'}, {role: 'redo'}, {type: 'separator'}, {role: 'cut'}, {role: 'copy'}, {role: 'paste'}, {role: 'selectAll'}]},
      {label: '显示', submenu: [{label: '后退', accelerator: 'CmdOrCtrl+[', click: () => window?.webContents.navigationHistory.goBack()}, {label: '前进', accelerator: 'CmdOrCtrl+]', click: () => window?.webContents.navigationHistory.goForward()}, {role: 'reload'}, {type: 'separator'}, {role: 'resetZoom'}, {role: 'zoomIn'}, {role: 'zoomOut'}, {role: 'togglefullscreen'}]},
      {label: '窗口', submenu: [{role: 'minimize'}, {role: 'zoom'}, {label: '显示工作台', click: () => show()}]}
    ]));
    const icon = nativeImage.createFromPath(path.join(__dirname, 'assets', process.platform === 'darwin' ? 'trayTemplate.png' : 'icon.png'));
    if (process.platform === 'darwin') icon.setTemplateImage(true);
    tray = new Tray(icon);
    tray.on('click', () => show());
    tray.on('right-click', () => tray.popUpContextMenu(trayMenu()));
    trayMenu();
    createWindow();
    if (!process.argv.includes('--hidden')) show(); else app.dock?.hide();
    try { await ensureBackend(); if (window.webContents.getURL().endsWith('/offline')) await window.loadURL(bridge.origin + lastRoute); } catch { await showOffline(); }
    await syncLoginItem();
    await refresh(); refreshTimer = setInterval(refresh, 15000);
  }).catch(error => { dialog.showErrorBox('AvatarTHU 未能启动', bridge.safeOutput(error.message)); app.quit(); });
}
