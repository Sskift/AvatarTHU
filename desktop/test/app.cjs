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
  const task = {id: taskID, title: 'Lab 1 · 信号与频谱', course: '数字信号处理', course_id: 'course1', status: 'awaiting', revision: 1, deadline: '2026-11-20 23:59', can_submit: false, artifact_count: 2};
  const health = {daemon: {state: 'running', phase: '等待调度'}, learn: {state: 'valid'}, lark: {enabled: false}, cli: [{tool: 'codex', state: 'ready', required: true}, {tool: 'claude', state: 'unknown', required: false}], writer: 'codex', reviewer: 'codex', poll_hours: 12, scan: {}, version: 'desktop-fixture'};
  const overview = {courses: [{id: 'course1', name: '数字信号处理', semester: '2026-2027-1', tasks: 1}, {id: 'course2', name: '计算机图形学基础', semester: '2026-2027-1', tasks: 0}], tasks: [task], health};
  const draft = {version: 1, base_revision: 1, markdown: '# 实验报告\n\n这是独立的桌面测试材料。', base_markdown: '# 实验报告\n\n这是独立的桌面测试材料。'};
  let writes = 0, syncs = 0, unauthorized = 0, offline = false, unavailable = false, electron;
  let heldOverview, holdNextOverview = false, overviewWaiting;
  let heldDirectory;
  const holdDirectory = pathname => {
    let started, release;
    const requested = new Promise(resolve => { started = resolve; });
    const pending = new Promise(resolve => { release = resolve; });
    const request = {pathname, started, pending, release: () => { if (heldDirectory === request) heldDirectory = null; release(); }};
    heldDirectory = request;
    return {requested, release: request.release};
  };
  const signalState = async () => {
    const file = path.join(root, 'data/tasks', taskID + '.json');
    await fs.writeFile(file + '.tmp', JSON.stringify(task)); await fs.rename(file + '.tmp', file);
  };
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
      if (url.pathname === '/api/workspace') {
        if (unavailable) { res.statusCode = 503; return json({error: 'fixture temporarily unavailable'}); }
        if (holdNextOverview) {
          holdNextOverview = false;
          const captured = JSON.stringify(overview); overviewWaiting();
          await new Promise(resolve => { heldOverview = resolve; });
          res.setHeader('Content-Type', 'application/json'); res.end(captured); return;
        }
        return json(overview);
      }
      if (url.pathname === '/api/workspace/health') return json(health);
      if (url.pathname === '/api/workspace/scan') {
        syncs++; health.scan = {id: 'manual-sync', state: 'queued'};
        await signalState(); return json(health.scan);
      }
      if (url.pathname === '/api/workspace/files') return json([material]);
      if (url.pathname === '/api/workspace/directory') {
        const current = url.searchParams.get('path') || '';
        const dir = (name, title=name) => ({name, title, path: [current,name].filter(Boolean).join('/'), kind:'directory'});
        const folders = {
          '': [dir('courseware'),dir('homework'),dir('notices')],
          'courseware': [material],
          'homework': [dir('lab1',task.title)],
          'homework/lab1': [dir('submission'),{name:'submission.zip',path:'homework/lab1/submission.zip',kind:'submission',size:128,course_id:'course1'},dir('workspace'),dir('presentation')],
          'homework/lab1/submission': [{name:'report.pdf',path:'homework/lab1/submission/report.pdf',kind:'submission',size:pdf.length,course_id:'course1'}],
          'homework/lab1/workspace': [dir('source')]
        };
        const breadcrumbs = [{title:'数字信号处理',path:''}];
        if(current) current.split('/').forEach((name,i,parts)=>breadcrumbs.push({title:name==='lab1'?task.title:name,path:parts.slice(0,i+1).join('/')}));
        const captured = JSON.parse(JSON.stringify({path:current,breadcrumbs,entries:folders[current]||[]}));
        if (heldDirectory?.pathname === current) { const held = heldDirectory; held.started(); await held.pending; }
        return json(captured);
      }
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
    await fs.mkdir(path.join(root, 'data/tasks'));
    await fs.writeFile(path.join(root, 'config.json'), JSON.stringify({menubar_enabled: false}));
    await fs.writeFile(path.join(root, 'data/editor-endpoint.json'), JSON.stringify({host: '127.0.0.1:' + server.address().port, token}));
    const env = {...process.env}; delete env.ELECTRON_RUN_AS_NODE;
    const executablePath = process.env.AVATARTHU_DESKTOP_BINARY;
    const baseArgs = executablePath ? [] : [path.resolve(__dirname, '..')];
    electron = await _electron.launch({...(executablePath ? {executablePath} : {}), args: [...baseArgs, '--home', root], env, timeout: 60000});
    const page = await electron.firstWindow();
    page.setDefaultTimeout(15000);
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
    const stateLatencies = []; let changedAt = performance.now();
    task.status = 'working'; await signalState();
    await page.getByRole('link', {name: /Lab 1.*正在撰写/}).waitFor({timeout: 2500});
    stateLatencies.push(Math.round(performance.now() - changedAt)); changedAt = performance.now();
    assert.equal(await page.getByRole('button', {name: '刷新', exact: true}).count(), 0);
    await page.getByRole('button', {name: '同步课程', exact: true}).click();
    await page.getByRole('button', {name: '正在连接学堂…', exact: true}).waitFor();
    assert.equal(await page.locator('#scan').isDisabled(), true);
    assert.equal(syncs, 1);
    health.scan.state = 'running'; await signalState();
    await page.getByRole('button', {name: '正在同步…', exact: true}).waitFor({timeout: 2500});
    assert.equal(task.status, 'working');
    health.scan.state = 'completed'; health.scan.completed_at = new Date().toISOString();
    overview.courses.push({id: 'synced-course', name: '同步发现的课程', semester: '2026-2027-1', tasks: 0});
    await signalState();
    await page.getByRole('button', {name: '同步课程', exact: true}).waitFor({timeout: 2500});
    await page.getByRole('heading', {name: '同步发现的课程', exact: true}).waitFor({timeout: 2500});
    changedAt = performance.now();
    task.status = 'reviewing'; await signalState();
    await page.getByRole('link', {name: /Lab 1.*独立复审中/}).waitFor({timeout: 2500});
    stateLatencies.push(Math.round(performance.now() - changedAt));
    // A change during an older request must be fetched again, not dropped.
    const requestWaiting = new Promise(resolve => { overviewWaiting = resolve; });
    holdNextOverview = true; task.status = 'working'; await signalState();
    await requestWaiting;
    task.status = 'awaiting'; await signalState();
    await new Promise(resolve => setTimeout(resolve, 200)); heldOverview();
    await page.getByRole('heading', {name: '有 1 份作业等你审阅', exact: true}).waitFor({timeout: 2500});
    await page.locator('[data-nav="files"]').click();
    await page.getByRole('link', {name:'打开目录 数字信号处理',exact:true}).waitFor();
    assert.equal(await page.locator('#directory-results .file-row').count(), 0);
    await page.evaluate(() => {
      window.directorySearchNode = document.getElementById('directory-search');
      window.blankDirectoryFrames = 0; window.sampleDirectoryFrames = true;
      const sample = () => {
        if (!window.sampleDirectoryFrames) return;
        if (!document.querySelector('#directory-results .directory-row, #directory-results .file-row')) window.blankDirectoryFrames++;
        requestAnimationFrame(sample);
      };
      requestAnimationFrame(sample);
    });
    const openingCourse = holdDirectory('');
    await page.getByRole('link', {name: '打开目录 数字信号处理', exact: true}).click();
    await openingCourse.requested;
    await page.locator('#directory-loading').filter({hasText:'正在打开目录…'}).waitFor({timeout:1500});
    assert.equal(await page.getByRole('link', {name:'打开目录 计算机图形学基础',exact:true}).count(), 1);
    assert.equal(await page.evaluate(() => window.directorySearchNode === document.getElementById('directory-search')), true);
    openingCourse.release();
    await page.getByRole('link', {name: '打开目录 homework', exact: true}).click();
    await page.getByRole('link', {name: '打开目录 '+task.title, exact: true}).click();
    await page.getByRole('button', {name: 'submission.zip', exact: true}).waitFor();
    assert.equal(await page.locator('#directory-results .directory-row').count(), 3);
    assert.equal(await page.locator('#directory-results .file-row').count(), 1);
    if (screenshotDir) await page.screenshot({path: path.join(screenshotDir, 'desktop-directories.png')});
    await page.getByRole('link', {name:'打开目录 submission',exact:true}).click();
    await page.getByRole('button', {name:'report.pdf',exact:true}).waitFor();
    await page.locator('#directory-crumbs').getByRole('link', {name:task.title,exact:true}).click();
    await page.getByRole('link', {name:'打开目录 workspace',exact:true}).click();
    await page.getByRole('link', {name:'打开目录 source',exact:true}).waitFor();
    const cachedCourse = holdDirectory('');
    await page.locator('#directory-crumbs').getByRole('link', {name:'数字信号处理',exact:true}).click();
    await cachedCourse.requested;
    // Returning to a visited directory is immediate, even with revalidation held.
    await page.getByRole('link', {name:'打开目录 courseware',exact:true}).waitFor({timeout:1000});
    await page.getByRole('link', {name:'打开目录 courseware',exact:true}).click();
    await page.getByRole('button', {name:'lecture.pdf',exact:true}).waitFor();
    cachedCourse.release();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert.equal(new URLSearchParams(new URL(page.url()).hash.slice(1)).get('path'), 'courseware');
    assert.equal(await page.getByRole('button', {name:'lecture.pdf',exact:true}).count(), 1);
    assert.equal(await page.evaluate(() => window.directorySearchNode === document.getElementById('directory-search')), true);
    assert.equal(await page.evaluate(() => { window.sampleDirectoryFrames = false; return window.blankDirectoryFrames; }), 0);
    await page.locator('#directory-search').fill('lecture');
    material.title = '更新后的课件'; task.updated_at = '2026-11-03T00:00:00+08:00'; await signalState();
    await page.getByRole('button', {name: '更新后的课件', exact: true}).waitFor({timeout: 2500});
    assert.equal(await page.locator('#directory-search').inputValue(), 'lecture');
    assert.equal(new URLSearchParams(new URL(page.url()).hash.slice(1)).get('path'), 'courseware');
    await page.evaluate(() => { window.directoryRowNode = document.querySelector('#directory-results .file-row'); document.getElementById('directory-search').focus(); });
    await page.evaluate(() => refresh());
    assert.equal(await page.evaluate(() => window.directoryRowNode === document.querySelector('#directory-results .file-row')), true);
    assert.equal(await page.locator('#directory-search').evaluate(input => input === document.activeElement), true);
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
    await page.locator('[data-nav="artifacts"]').click();
    await page.locator('#artifact-tasks').getByRole('link', {name:/Lab 1/}).waitFor();
    assert.equal(await page.getByRole('heading', {name:'产物与源文件',exact:true}).count(), 0);
    assert.equal(await page.locator('#content .file-list, #file-search').count(), 0);
    if (screenshotDir) await page.screenshot({path:path.join(screenshotDir,'desktop-artifacts.png')});
    await page.locator('[data-nav="courses"]').click();
    await page.locator('.course-card').first().click();
    await page.locator('.task-row').first().click();
    await page.getByRole('link', {name: '编辑与审阅报告', exact: true}).click();
    await page.locator('#paper .report-block').first().waitFor();
    assert.equal(electron.windows().length, 1);
    await page.getByRole('tab', {name: 'Markdown', exact: true}).click();
    await page.locator('#markdown').fill('# 实验报告\n\n已保存的桌面测试修改。');
    task.status = 'working'; await signalState();
    await page.locator('#task-status').filter({hasText: '更新产物中'}).waitFor({timeout: 2500});
    assert.match(await page.locator('#markdown').inputValue(), /已保存的桌面测试修改/);
    await page.waitForFunction(() => document.getElementById('save-state').textContent === '已保存到本机');
    await new Promise(resolve => setTimeout(resolve, 1400));
    assert.equal(writes > 0, true);
    assert.match(draft.markdown, /已保存的桌面测试修改/);
    if (screenshotDir) await page.screenshot({path: path.join(screenshotDir, 'desktop-editor.png')});
    await page.locator('.brand').click();
    await page.locator('[data-nav="settings"]').click();
    await page.getByRole('heading', {name: '后台与连接', exact: true}).waitFor();
    await page.locator('[name="poll_hours"]').fill('17');
    health.learn.state = 'expired'; health.poll_hours = 24; await signalState();
    await page.getByText('需要登录', {exact: true}).waitFor({timeout: 2500});
    assert.equal(await page.locator('[name="poll_hours"]').inputValue(), '17');
    health.learn.state = 'valid'; await signalState();
    await page.getByText('登录有效', {exact: true}).waitFor({timeout: 2500});
    if (screenshotDir) await page.screenshot({path: path.join(screenshotDir, 'desktop-settings.png')});
    assert.equal(await page.evaluate(() => window.avatarDesktop.control('submit').then(() => false, () => true)), true);
    health.cli[0].state = 'unavailable';
    const diagnostic = await page.evaluate(() => window.avatarDesktop.control('doctor').then(() => '', error => error.message));
    assert.match(diagnostic, /ENOENT/);
    await page.getByText('不可用', {exact: true}).waitFor({timeout: 2500});
    await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].close());
    assert.equal(await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].isVisible()), false);
    health.learn.state = 'expired'; await signalState();
    await page.getByText('需要登录', {exact: true}).waitFor({timeout: 2500});
    health.learn.state = 'valid'; await signalState();
    await page.getByText('登录有效', {exact: true}).waitFor({timeout: 2500});
    await promisify(execFile)(executablePath || require('electron'), [...baseArgs, '--home', root, '--view', 'courses'], {env, timeout: 15000});
    await page.waitForURL('**/#view=courses');
    assert.equal(await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows().length), 1);
    assert.equal(await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].isVisible()), true);
    await page.getByRole('heading', {name: '数字信号处理', exact: true}).waitFor();
    // No file notification: returning to a hidden window must still refresh immediately.
    await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].hide());
    overview.courses[0].name = '课程状态已更新';
    await promisify(execFile)(executablePath || require('electron'), [...baseArgs, '--home', root, '--view', 'courses'], {env, timeout: 15000});
    await page.getByRole('heading', {name: '课程状态已更新', exact: true}).waitFor({timeout: 2500});
    unavailable = true; await signalState();
    await page.locator('#connection').filter({hasText: '连接中断'}).waitFor({timeout: 2500});
    unavailable = false; await signalState();
    await page.locator('#connection').filter({hasText: '本机后台运行中'}).waitFor({timeout: 2500});
    offline = true;
    await signalState();
    // A broken transport may consume the main process's three-second timeout.
    await page.locator('#connection').filter({hasText: '连接中断'}).waitFor({timeout: 4500});
    offline = false;
    await signalState();
    await page.locator('#connection').filter({hasText: '本机后台运行中'}).waitFor({timeout: 2500});
    assert.equal(new URL(page.url()).hash, '#view=courses');
    assert.equal(unauthorized, 0);
    assert.deepEqual(errors, []);
    console.log('Electron: flicker-free directory navigation, cached return, stale response protection, unchanged refresh preservation, course sync, live state updates, drafts, reconnection, single window, PDF preview/download, and report save passed.');
    console.log('Observed state update latency (ms): ' + stateLatencies.join(', '));
  } finally {
    heldDirectory?.release(); heldOverview?.();
    if (electron) await electron.close();
    await new Promise(resolve => server.close(resolve));
    await fs.rm(root, {recursive: true, force: true});
  }
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
