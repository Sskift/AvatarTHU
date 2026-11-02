'use strict';
const {_electron} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const http = require('node:http');
const {execFile} = require('node:child_process');
const {promisify} = require('node:util');

(async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'avatar-desktop-test-'));
  const token = 'b'.repeat(64), taskID = '0123456789abcdef';
  const task = {id: taskID, title: 'Lab 1 · 信号与频谱', course: '数字信号处理', course_id: 'course1', status: 'awaiting', revision: 1, deadline: '2026-11-20 23:59', can_submit: false};
  const health = {daemon: {state: 'running', phase: '等待调度'}, learn: {state: 'valid'}, lark: {enabled: false}, cli: [{tool: 'codex', state: 'ready', required: true}, {tool: 'claude', state: 'unknown', required: false}], writer: 'codex', reviewer: 'codex', poll_hours: 12, scan: {}, version: 'desktop-fixture'};
  const overview = {courses: [{id: 'course1', name: '数字信号处理', semester: '2026-2027-1', tasks: 1}, {id: 'course2', name: '计算机图形学基础', semester: '2026-2027-1', tasks: 0}], tasks: [task], health};
  const draft = {version: 1, base_revision: 1, markdown: '# 实验报告\n\n这是独立的桌面测试材料。', base_markdown: '# 实验报告\n\n这是独立的桌面测试材料。'};
  let writes = 0, unauthorized = 0, offline = false, electron;
  const pdf = Buffer.from('%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 200]/Contents 4 0 R/Resources<<>>>>endobj\n4 0 obj<</Length 26>>stream\n0.5 g 30 30 240 140 re f\nendstream\nendobj\ntrailer<</Root 1 0 R>>\n%%EOF');
  const material = {course_id: 'course1', course: '数字信号处理', name: 'lecture.pdf', path: 'courseware/lecture.pdf', kind: 'courseware', size: pdf.length};
  const ui = path.resolve(__dirname, '../../internal/app');
  const files = {'/': 'workspace_ui.html', '/workspace.js': 'workspace_ui.js', '/workspace.css': 'workspace_ui.css', '/desktop.js': 'desktop_ui.js', '/desktop.css': 'desktop_ui.css', '/editor': 'editor_ui.html', '/editor.js': 'editor_ui.js', '/editor.css': 'editor_ui.css'};
  const server = http.createServer(async (req, res) => {
    try {
      if (offline) { req.socket.destroy(); return; }
      const url = new URL(req.url, 'http://localhost');
      const json = value => { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(value)); };
      if (files[url.pathname]) {
        res.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; worker-src 'self' blob:; frame-src 'self' blob:");
        res.setHeader('Content-Type', url.pathname.endsWith('.js') ? 'text/javascript' : url.pathname.endsWith('.css') ? 'text/css' : 'text/html');
        res.end(await fs.readFile(path.join(ui, files[url.pathname]))); return;
      }
      if (/^\/pdfjs\/[a-zA-Z0-9_.\/-]+$/.test(url.pathname) && !url.pathname.includes('..')) {
        res.setHeader('Content-Type', url.pathname.endsWith('.mjs') ? 'text/javascript' : 'application/octet-stream');
        res.end(await fs.readFile(path.join(ui, url.pathname))); return;
      }
      if (!req.headers.cookie?.includes(token)) { unauthorized++; res.statusCode = 401; json({error: 'unauthorized'}); return; }
      let body = ''; for await (const chunk of req) body += chunk;
      const input = body ? JSON.parse(body) : {};
      if (url.pathname === '/api/workspace') return json(overview);
      if (url.pathname === '/api/workspace/health') return json(health);
      if (url.pathname === '/api/workspace/files') return json([material]);
      if (url.pathname === '/api/workspace/preview') return json({kind: 'pdf'});
      if (url.pathname === '/api/workspace/file') {
        res.setHeader('Content-Type', 'application/pdf');
        if (!url.searchParams.has('inline')) res.setHeader('Content-Disposition', 'attachment; filename="lecture.pdf"');
        res.end(pdf); return;
      }
      if (url.pathname === '/api/workspace/task/' + taskID) return json({...task, description: '测试材料：分析声音信号的频谱。', reviews: [], revisions: [], attachments: []});
      if (url.pathname === '/api/tasks') return json([task]);
      if (url.pathname === '/api/task/' + taskID) return json({task, draft, requests: [], writer: 'Codex', reviewer: 'Codex', artifacts: []});
      if (url.pathname.endsWith('/render')) return json({blocks: input.blocks.map((text, i) => i === 0 ? '<h1>实验报告</h1>' : '<p>' + text.replaceAll('<', '&lt;') + '</p>')});
      if (url.pathname.endsWith('/draft')) { writes++; draft.version++; draft.markdown = input.markdown; return json({draft}); }
      res.statusCode = 404; json({error: 'missing fixture route ' + url.pathname});
    } catch (error) { res.statusCode = 500; res.end(JSON.stringify({error: error.message})); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    await fs.mkdir(path.join(root, 'data'), {recursive: true});
    await fs.writeFile(path.join(root, 'config.json'), JSON.stringify({menubar_enabled: false}));
    await fs.writeFile(path.join(root, 'data/editor-endpoint.json'), JSON.stringify({host: '127.0.0.1:' + server.address().port, token}));
    const env = {...process.env}; delete env.ELECTRON_RUN_AS_NODE;
    const executablePath = process.env.AVATARTHU_DESKTOP_BINARY;
    const baseArgs = executablePath ? [] : [path.resolve(__dirname, '..')];
    electron = await _electron.launch({...(executablePath ? {executablePath} : {}), args: [...baseArgs, '--home', root], env, timeout: 60000});
    const page = await electron.firstWindow();
    page.on('console', message => { if (message.type() === 'error') console.error('Renderer:', message.text()); });
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    await page.getByRole('heading', {name: '现在需要关注', exact: true}).waitFor();
    assert.equal(new URL(page.url()).protocol, 'avatarthu:');
    assert.equal(page.url().includes(token), false);
    assert.equal(await page.evaluate(() => typeof require), 'undefined');
    assert.equal(await page.evaluate(() => document.cookie), '');
    assert.equal(await page.locator('[data-desktop]').count(), 1);
    const screenshotDir = process.env.AVATARTHU_SCREENSHOTS;
    if (screenshotDir) { await fs.mkdir(screenshotDir, {recursive: true}); await page.screenshot({path: path.join(screenshotDir, 'desktop-overview.png')}); }
    await page.locator('[data-nav="files"]').click();
    await page.getByRole('button', {name: '预览', exact: true}).click();
    try { await page.waitForFunction(() => document.getElementById('pdf-count')?.textContent === '1' && document.getElementById('pdf-status').textContent === ''); }
    catch (error) { throw new Error(await page.locator('#preview-body').innerText(), {cause: error}); }
    assert.equal(await page.locator('#pdf-canvas').evaluate(canvas => canvas.width > 0 && canvas.height > 0), true);
    await page.getByRole('button', {name: '关闭预览', exact: true}).click();
    const savedPDF = path.join(root, 'downloaded.pdf');
    await electron.evaluate(({session}, target) => { session.defaultSession.once('will-download', (_event, item) => item.setSavePath(target)); }, savedPDF);
    await page.getByRole('link', {name: '下载', exact: true}).click();
    for (let n = 0; n < 50; n++) { try { if ((await fs.stat(savedPDF)).size === pdf.length) break; } catch {} await new Promise(resolve => setTimeout(resolve, 100)); }
    assert.deepEqual(await fs.readFile(savedPDF), pdf);
    await page.locator('[data-nav="courses"]').click();
    await page.locator('.course-card').first().click();
    await page.locator('.task-row').first().click();
    await page.getByRole('link', {name: '编辑与审阅报告', exact: true}).click();
    await page.locator('#paper .report-block').first().waitFor();
    assert.equal(electron.windows().length, 1);
    await page.getByRole('tab', {name: 'Markdown', exact: true}).click();
    await page.locator('#markdown').fill('# 实验报告\n\n已保存的桌面测试修改。');
    await page.waitForFunction(() => document.getElementById('save-state').textContent === '已保存到本机');
    await new Promise(resolve => setTimeout(resolve, 1400));
    assert.equal(writes > 0, true);
    assert.match(draft.markdown, /已保存的桌面测试修改/);
    if (screenshotDir) await page.screenshot({path: path.join(screenshotDir, 'desktop-editor.png')});
    await page.locator('.brand').click();
    await page.locator('[data-nav="settings"]').click();
    await page.getByRole('heading', {name: '后台与连接', exact: true}).waitFor();
    if (screenshotDir) await page.screenshot({path: path.join(screenshotDir, 'desktop-settings.png')});
    assert.equal(await page.evaluate(() => window.avatarDesktop.control('submit').then(() => false, () => true)), true);
    const diagnostic = await page.evaluate(() => window.avatarDesktop.control('doctor').then(() => '', error => error.message));
    assert.match(diagnostic, /ENOENT/);
    await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].close());
    assert.equal(await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].isVisible()), false);
    await promisify(execFile)(executablePath || require('electron'), [...baseArgs, '--home', root, '--view', 'courses'], {env, timeout: 15000});
    await page.waitForURL('**/#view=courses');
    assert.equal(await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows().length), 1);
    assert.equal(await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].isVisible()), true);
    offline = true;
    await page.reload();
    await page.waitForURL(url => url.pathname === '/offline');
    offline = false;
    await page.locator('#reconnect').click();
    await page.waitForURL('**/#view=courses');
    assert.equal(unauthorized, 0);
    assert.deepEqual(errors, []);
    console.log('Electron: window, internal navigation, authenticated bridge, PDF preview/download, report save, window recovery, single instance, and offline reconnection passed.');
  } finally {
    if (electron) await electron.close();
    await new Promise(resolve => server.close(resolve));
    await fs.rm(root, {recursive: true, force: true});
  }
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
