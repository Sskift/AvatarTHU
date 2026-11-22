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
  const task = {id: taskID, title: 'Lab 1 · 信号与频谱', course: '数字信号处理', course_id: 'course1', status: 'awaiting', revision: 1, deadline: '2026-11-20 23:59', can_submit: true, artifact_count: 2};
  const health = {daemon: {state: 'running', phase: '等待调度'}, learn: {state: 'valid'}, lark: {enabled: false}, cli: [{tool: 'codex', state: 'ready', required: true}, {tool: 'claude', state: 'unknown', required: false}], writer: 'codex', reviewer: 'codex', poll_hours: 12, scan: {}, version: 'desktop-fixture'};
  const overview = {courses: [{id: 'course1', name: '数字信号处理', semester: '2026-2027-1', tasks: 1}, {id: 'course2', name: '计算机图形学基础', semester: '2026-2027-1', tasks: 1}], tasks: [task,{id:'0123456789abcdee',course_id:'course2',course:'计算机图形学基础',title:'Lab 2 · 曲面细分',status:'reviewing',revision:2,deadline:'2026-11-24 23:59',artifact_count:0}], health};
  const draft = {version: 1, base_revision: 1, markdown: '# 实验报告\n\n这是独立的桌面测试材料。', base_markdown: '# 实验报告\n\n这是独立的桌面测试材料。'};
  const editorRequests = [];
  const editorReview = {revision: 1, round: 1, approved: false, reviewer: 'codex', summary: '请补充测量单位。',
    comments: [{location: '结果', comment: '频谱缺少单位', suggestion: '请注明频率单位'}]};
  const editorView = () => ({task: {...task, can_submit: task.status === 'awaiting'}, draft, review: editorReview,
    requests: editorRequests, writer: 'claude', reviewer: 'codex', artifacts: ['report.pdf','analysis.zip']});
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
  // A small single-page sample also exercises text, vector plots and the PDF font assets in the reader.
  const plot = [
    '0.16 0.14 0.18 rg BT /F1 11 Tf 38 352 Td (Signal processing / Lab 01) Tj ET',
    'BT /F1 24 Tf 38 314 Td (Frequency spectrum) Tj ET',
    '0.35 g BT /F1 12 Tf 38 285 Td (Single-sided amplitude spectrum of two sampled tones.) Tj ET',
    '0.85 G 0.5 w 57 116 m 476 116 l 57 147 m 476 147 l 57 178 m 476 178 l 57 209 m 476 209 l S',
    '0.45 G 1 w 57 234 m 57 88 l 476 88 l S',
    '0.41 0.26 0.51 RG 2 w 58 90 m 122 90 l 126 97 l 130 212 l 134 98 l 138 90 l 289 90 l 294 96 l 298 172 l 302 96 l 306 90 l 475 90 l S',
    '0.35 g BT /F1 10 Tf 57 72 Td (0) Tj 65 0 Td (440) Tj 170 0 Td (1440) Tj 143 0 Td (Hz) Tj ET',
    'BT /F1 11 Tf 57 248 Td (Normalized amplitude) Tj ET',
    'BT /F1 10 Tf 38 30 Td (Figure 1. Sample peaks at 440 Hz and 1440 Hz.) Tj ET'
  ].join('\n');
  function samplePDF(pages) {
    const objects = [
      '<</Type/Catalog/Pages 2 0 R>>',
      '<</Type/Pages/Kids[' + pages.map((_, i) => (4 + 2*i) + ' 0 R').join(' ') + ']/Count ' + pages.length + '>>',
      '<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>'
    ];
    pages.forEach((page, i) => objects.push(
      '<</Type/Page/Parent 2 0 R/MediaBox[0 0 520 380]/Contents ' + (5 + 2*i) + ' 0 R/Resources<</Font<</F1 3 0 R>>>>>>',
      '<</Length ' + Buffer.byteLength(page) + '>>stream\n' + page + '\nendstream'
    ));
    return Buffer.from('%PDF-1.4\n' + objects.map((value, i) => (i + 1) + ' 0 obj\n' + value + '\nendobj').join('\n') + '\ntrailer<</Root 1 0 R>>\n%%EOF');
  }
  const title = (y, text) => 'BT /F1 14 Tf 38 ' + y + ' Td (' + text + ') Tj ET';
  const pdf = samplePDF([plot]);
  const promptPDF = samplePDF([[title(300,'Problem 1'), title(170,'Problem 2'), title(100,'Problem 3'), title(70,'4. Source code (2 points)'), title(45,'Use src/main.go for the analysis.')].join('\n')]);
  const answerPDF = samplePDF([plot+'\n'+title(265,'1. Spectrum'), title(300,'2. Sampling')]);
  const material = {course_id: 'course1', course: '数字信号处理', name: 'lecture.pdf', path: 'courseware/lecture.pdf', kind: 'courseware', size: pdf.length};
  const prompt = {...material, name:'problems.pdf', path:'homework/lab1/workspace/source/problems.pdf', kind:'assignment', size:promptPDF.length};
  const promptArchive = {...prompt, name:'materials.zip', path:prompt.path.replace('problems.pdf','materials.zip')};
  const insidePrompt = {...prompt, path:promptArchive.path, member:'problems.pdf'};
  const report = {...material, name: 'report.pdf', path: 'homework/lab1/presentation/outputs/r1/report.pdf', kind: 'artifact', submission_member: true, size:answerPDF.length};
  const project = {...report, submission_member: false, name: 'analysis.zip', path: 'homework/lab1/presentation/outputs/r1/analysis.zip'};
  const member = {...project,name: 'main.go', member: 'src/main.go', size: 82};
  const bundle = {...report, name:'submission.zip', path:'homework/lab1/presentation/outputs/r1/submission.zip', sha256:'f'.repeat(64)};
  let lastRevision, fileEdit, published = 1;
  const bundleFor = n => ({...bundle, path:bundle.path.replace('/r1/', '/r' + n + '/')});
  const entry = (name,bytes) => ({name,size:bytes,available:true});
  const manifest = n => ({revision:n, submission:bundleFor(n), previous:n>1?bundleFor(n-1):null, baseline:n>1?'available':'first',
    directories:['examples'], files:[entry('report.pdf',report.size),entry('src/main.go',82),entry('tools/main.go',90)].map(current=>({
      name:current.name,current,previous:n>1?current:undefined,change:n>1&&current.name!=='tools/main.go'?'modified':n>1?'unchanged':'first'}))});
  const messages = [], timeline = [
    {revision:1,round:1,role:'writer',harness:'codex',title:'已准备候选产物',detail:'包含报告和分析程序。',state:'done'},
    {revision:1,round:1,role:'reviewer',harness:'claude',title:'独立复审已完成',detail:'频谱和实验记录相符。',state:'done'}
  ];
  const conversation = () => ({file_revision:1,can_ask:true,can_send:['working','revision_ready','awaiting','reviewing'].includes(task.status),messages});
  const detail = () => ({...task, can_submit: task.status==='awaiting', can_revise: task.status==='awaiting', has_report_source: true,
    workflow: {writer:'codex',reviewer:'claude',feedback:lastRevision?.instruction,round:1},
    conversation:conversation(),timeline,description: '分析声音信号的频谱。', reviews: [{revision:1,round:1,reviewer:'claude',approved:true,summary:'频谱和实验记录相符。',
      comments:[{artifact:'report.pdf',location:'第 1 页',issue:'图中两处峰值与记录一致。'},{artifact:'analysis.zip',location:'src/main.go',issue:'空输入没有提示。',suggestion:'说明可用的输入格式。'}]}],
    revisions: [{revision:published,files:[report,project],submission:bundleFor(published)}], attachments: [promptArchive]});
  const ui = path.resolve(__dirname, '../../internal/app');
  const files = {'/': 'workspace_ui.html', '/workspace.js': 'workspace_ui.js', '/workspace-files.js': 'workspace_files.js', '/workspace-review.js': 'workspace_review.js', '/workspace.css': 'workspace_ui.css', '/desktop.js': 'desktop_ui.js', '/desktop.css': 'desktop_ui.css', '/editor': 'editor_ui.html', '/editor.js': 'editor_ui.js', '/editor.css': 'editor_ui.css'};
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
      if (url.pathname === '/api/workspace/preview') {
        if(url.searchParams.get('path')===promptArchive.path && !url.searchParams.has('member'))
          return json({kind:'zip',entries:[{name:'signal.wav',size:500},{name:insidePrompt.member,size:insidePrompt.size,file:insidePrompt}]});
        if(url.searchParams.get('member')==='tools/main.go')return json({kind:'text',text:'package tools'});
        if(url.searchParams.get('member')===member.member)return json({kind:'text',text:'package main'});
        if(url.searchParams.get('path')===project.path)return json({kind:'zip',entries:[{name:member.member,size:member.size,file:member}]});
        return json({kind:'pdf'});
      }
      if (url.pathname === '/api/workspace/file') {
        res.setHeader('Content-Type', 'application/pdf');
        if (!url.searchParams.has('inline')) res.setHeader('Content-Disposition', 'attachment; filename="lecture.pdf"');
        const filename = url.searchParams.get('path');
        res.end(filename === promptArchive.path ? promptPDF : filename === report.path || url.searchParams.get('member')==='report.pdf' ? answerPDF : pdf); return;
      }
      if (url.pathname === '/api/workspace/task/' + taskID) return json(detail());
      if (url.pathname === '/api/workspace/task/' + taskID + '/submission-tree') return json(manifest(Number(url.searchParams.get('revision'))));
      if (url.pathname === '/api/workspace/task/' + taskID + '/file-change')
        return json({lines:'@ 上版 1 行 / 本版 1 行\n- package old\n+ package main'});
      if (url.pathname === '/api/workspace/task/' + taskID + '/file-edit') {
        fileEdit = input; task.status='revision_ready'; await signalState(); return json(detail());
      }
      if (url.pathname === '/api/workspace/task/' + taskID + '/activity') {
        return json({id:task.id,revision:task.revision,status:task.status,updated_at:task.updated_at,timeline,conversation:conversation()});
      }
      if (url.pathname === '/api/workspace/task/' + taskID + '/conversation') {
        if (input.revision!==task.revision) {res.statusCode=409;return json({error:'版本已更新'});}
        if (!/^[a-f0-9]{32}$/.test(input.id)) throw Error('Missing idempotency key');
        messages.push({...input,role:'user',target_revision:input.kind==='change'&&task.status==='awaiting'?task.revision+1:task.revision,text:input.instruction,state:'queued',created_at:'2026-11-19T10:00:00+08:00'});
        if(input.kind==='change'){
          lastRevision=input;if(task.status==='awaiting')task.status='revision_ready';
          await signalState();
        }
        return json(detail());
      }
      if (url.pathname === '/api/tasks') return json([task]);
      if (url.pathname === '/api/task/' + taskID) return json(editorView());
      if (url.pathname.endsWith('/render')) return json({blocks: input.blocks.map((text, i) => i === 0 ? '<h1>实验报告</h1>' : '<p>' + text.replaceAll('<', '&lt;') + '</p>')});
      if (url.pathname.endsWith('/draft')) { writes++; draft.version++; draft.markdown = input.markdown; return json(editorView()); }
      if (url.pathname.endsWith('/rewrite')) {
        assert.equal(input.version, draft.version);
        editorRequests.unshift({...input, base_revision: draft.base_revision, kind: 'rewrite', state: 'ready', writer: 'claude',
          selected: draft.markdown.slice(input.start, input.end), replacement: '频率单位为 Hz。'});
        return json(editorRequests[0]);
      }
      if (url.pathname.endsWith('/decide')) {
        const request = editorRequests.find(r => r.id === input.id);
        assert.equal(input.action, 'accept');
        draft.markdown = draft.markdown.slice(0, request.start) + request.replacement + draft.markdown.slice(request.end);
        draft.version++;
        request.state = 'applied';
        return json(editorView());
      }
      if (url.pathname.endsWith('/submit')) {
        assert.equal(input.version, draft.version);
        editorRequests.unshift({...input, kind: 'submit', state: 'accepted', target_revision: task.revision+1, task_status: 'revision_ready'});
        task.status = 'revision_ready';
        return json(editorRequests[0]);
      }
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
    await page.getByRole('heading', {name: '等你处理', exact: true}).waitFor();
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
    await page.locator('[data-nav="courses"]').click();
    await page.getByRole('heading', {name: '同步发现的课程', exact: true}).waitFor({timeout: 2500});
    await page.locator('[data-nav="overview"]').click();
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
    await page.getByRole('heading', {name: '等你处理', exact: true}).waitFor({timeout: 2500});
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
    await page.locator('[data-nav="overview"]').click();
    await page.getByRole('link',{name:/已有产物/}).click();
    await page.locator('#queue-results').getByRole('link', {name:/Lab 1/}).waitFor();
    assert.equal(await page.getByRole('heading', {name:'产物与源文件',exact:true}).count(), 0);
    assert.equal(await page.locator('#content .file-list, #file-search').count(), 0);
    if (screenshotDir) await page.screenshot({path:path.join(screenshotDir,'desktop-artifacts.png')});
    await page.locator('[data-nav="courses"]').click();
    await page.locator('.course-card').first().click();
    await page.locator('.task-row').first().click();
    await page.getByRole('button',{name:'第 2 题',exact:true}).click();
    await page.locator('#artifact-preview [aria-label="PDF 第 2 页，共 2 页"]').waitFor();
    await page.locator('#prompt-preview [aria-label="PDF 第 1 页，共 1 页"]').waitFor();
    assert.equal(await page.locator('#prompt-preview .pdf-location').isVisible(),true);
    assert.equal(await page.locator('#artifact-preview .pdf-location').isVisible(),true);
    await page.locator('#artifact-pdf-zoom').selectOption('page');
    await page.waitForFunction(()=>document.getElementById('artifact-pdf-status')?.textContent==='');
    await page.getByRole('button',{name:'只看原题',exact:true}).click();
    assert.equal(await page.locator('#answer-reader').isHidden(),true);
    await page.getByRole('button',{name:'题目对照',exact:true}).click();
    await page.getByRole('button',{name:'引用到对话',exact:true}).click();
    assert.match(await page.locator('#chat-text').inputValue(),/第 2 题.*problems.pdf 第 1 页.*report.pdf 第 2 页/);
    assert.equal(messages.length,0);
    await page.locator('#chat-context summary').click();
    assert.equal(await page.getByRole('checkbox',{name:'report.pdf'}).isChecked(),true);
    await page.getByRole('checkbox',{name:'report.pdf'}).uncheck();
    await page.locator('#chat-context summary').click();
    await page.locator('#chat-text').fill('');
    await page.getByRole('button',{name:'返回对照位置'}).click();
    await page.locator('[data-nav=overview]').click();
    await page.locator('.task-row').filter({hasText:task.title}).click();
    await page.locator('#artifact-preview [aria-label="PDF 第 2 页，共 2 页"]').waitFor();
    await page.waitForFunction(()=>document.querySelector('#question-nav [aria-pressed=true]')?.textContent==='第 2 题');
    assert.equal(await page.locator('#artifact-pdf-zoom').inputValue(),'page');
    await page.getByRole('button',{name:'第 3 题',exact:true}).click();
    await page.locator('#question-hint').waitFor();
    await page.locator('#mark-position').click();
    assert.equal(await page.locator('#mark-name').inputValue(),'第 3 题');
    await page.getByRole('button',{name:'保存位置',exact:true}).click();
    assert.equal(await page.locator('#question-hint').isHidden(),true);
    await page.getByRole('button',{name:'第 1 题',exact:true}).click();
    await page.locator('#artifact-preview [aria-label="PDF 第 1 页，共 2 页"]').waitFor();
    await page.getByRole('button',{name:'第 3 题',exact:true}).click();
    await page.locator('#artifact-preview [aria-label="PDF 第 2 页，共 2 页"]').waitFor();
    await page.locator('#mark-position').click();
    await page.getByRole('button',{name:'移除此标记'}).click();
    assert.equal(await page.getByRole('button',{name:'第 3 题',exact:true}).getAttribute('title'),'作答位置待定位');
    await page.locator('#prompt-choice').selectOption('');
    await page.locator('#prompt-preview pre').getByText('分析声音信号的频谱。',{exact:true}).waitFor();
    await page.locator('#prompt-choice').selectOption(promptArchive.path);
    await page.locator('#prompt-back').click();
    await page.locator('#prompt-preview').getByRole('button',{name:'problems.pdf',exact:true}).click();
    await page.getByRole('button',{name:'第 1 题',exact:true}).click();
    await page.waitForFunction(()=>document.getElementById('artifact-pdf-canvas')?.getAttribute('aria-label')==='PDF 第 1 页，共 2 页');
    if(screenshotDir)await page.screenshot({path:path.join(screenshotDir,'desktop-review.png')});
    assert.equal(await page.locator('#submission-tree [data-review-file]').count(),3);
    assert.match(await page.locator('#tree-count').innerText(), /3 个文件/);
    await page.locator('#submission-tree').getByRole('button',{name:'tools/main.go',exact:true}).click();
    await page.locator('#artifact-preview pre').getByText('package tools',{exact:true}).waitFor();
    await page.locator('#question-nav [aria-pressed=true]').filter({hasText:'Source code'}).waitFor();
    assert.equal(await page.locator('#question-nav button[title*="Use src/main.go"]').count(),0);
    await page.getByRole('button',{name:'文件内容',exact:true}).click();
    assert.equal(await page.locator('#prompt-reader').isHidden(),true);
    await page.locator('#edit-text').click();
    await page.locator('#text-edit-content').fill('package student');
    await page.getByRole('button',{name:'关闭草稿'}).click();
    await page.locator('#edit-text').click();
    assert.equal(await page.locator('#text-edit-content').inputValue(),'package student');
    await page.getByRole('button',{name:'关闭草稿'}).click();
    await page.locator('#submission-tree').getByRole('button',{name:'src/main.go',exact:true}).click();
    await page.locator('#artifact-preview pre').getByText('package main',{exact:true}).waitFor();
    await page.getByRole('button',{name:'题目对照',exact:true}).click();
    await page.locator('#question-nav button[title*="Use src/main.go"]').click();
    assert.equal(await page.locator('#prompt-preview .pdf-location').isVisible(),true);
    await page.getByRole('button',{name:'版本改动',exact:true}).click();
    await page.locator('#file-changes').getByText('这是第一个提交版本。').waitFor();
    await page.locator('#submission-tree').getByRole('button',{name:'report.pdf',exact:true}).click();
    await page.getByRole('button',{name:'题目对照',exact:true}).click();
    await page.locator('#artifact-pdf-canvas').waitFor();
    assert.equal(await page.locator('#artifact-pdf-zoom').inputValue(),'page');
    await page.getByRole('tab',{name:'独立复审'}).click();
    await page.getByRole('button',{name:'report.pdf',exact:true}).click();
    await page.getByRole('button',{name:'关闭预览'}).click();
    await page.getByRole('tab',{name:'文件审阅'}).click();
    await page.locator('#related-files summary').click();
    await page.locator('#related-files').getByRole('button',{name:project.name,exact:true}).click();
    await page.locator('#artifact-preview').getByRole('button',{name:'src/main.go',exact:true}).click();
    await page.locator('#artifact-preview pre').getByText('package main',{exact:true}).waitFor();
    assert.equal(new URL(await page.locator('#artifact-download').evaluate(a=>a.href)).searchParams.get('member'),member.member);
    await page.locator('#answer-back').click();
    await page.locator('#artifact-preview').getByRole('button',{name:'src/main.go',exact:true}).waitFor();
    assert.equal(await page.getByRole('button',{name:'问主写',exact:true}).getAttribute('aria-pressed'),'true');
    await page.locator('#chat-text').fill('两个峰值的频率是如何确定的？');
    await page.getByRole('button',{name:'发送',exact:true}).click();
    await page.locator('#conversation-log').getByText('两个峰值的频率是如何确定的？',{exact:true}).waitFor();
    assert.equal(task.status,'awaiting');
    assert.equal(messages[0].kind,'ask');
    const answer='读取采样数据后计算 FFT，对应峰值位于 440 Hz 和 1440 Hz。';
    messages[0].state='answered';
    messages.push({id:'reply',role:'writer',kind:'ask',text:answer,revision:1,writer:'codex',reply_to:[messages[0].id]});
    await page.getByRole('tab',{name:'工作流'}).click();
    await page.locator('#activity-log').getByText('独立复审已完成',{exact:true}).waitFor();
    await page.locator('#chat-text').fill('为源代码补充输入检查。');
    await page.evaluate(()=>{window.activeComposer=document.getElementById('chat-text');});
    timeline.push({revision:1,round:1,role:'writer',harness:'codex',title:'已回答频谱问题',state:'done'});
    await page.locator('#activity-log').getByText('已回答频谱问题',{exact:true}).waitFor({timeout:8000});
    await page.locator('#conversation-log').getByText(answer,{exact:true}).waitFor();
    assert.equal(await page.locator('#chat-text').inputValue(),'为源代码补充输入检查。');
    assert.equal(await page.evaluate(()=>window.activeComposer===document.activeElement),true);
    await page.getByRole('tab',{name:'独立复审'}).click();
    await page.getByRole('button',{name:'引用到对话'}).nth(1).click();
    assert.equal(await page.getByRole('button',{name:'调整产物',exact:true}).getAttribute('aria-pressed'),'true');
    const quoted='analysis.zip · src/main.go — 空输入没有提示。 说明可用的输入格式。';
    await page.locator('#chat-context summary').click();
    assert.equal(await page.getByRole('checkbox',{name:'analysis.zip'}).isChecked(),true);
    assert.equal(await page.getByRole('checkbox',{name:'report.pdf'}).isChecked(),false);
    assert.equal(await page.locator('#chat-text').inputValue(),'为源代码补充输入检查。\n'+quoted);
    await page.getByRole('button',{name:'发送',exact:true}).click();
    await page.locator('#task-meta').getByText('修改已排队',{exact:true}).waitFor();
    assert.deepEqual(lastRevision.files,['analysis.zip']);
    assert.equal(lastRevision.file_revision,1);
    assert.equal(lastRevision.instruction,'为源代码补充输入检查。\n'+quoted);
    // A follow-up written during a revision keeps the old published file's identity.
    await page.getByRole('tab',{name:'工作流'}).click();
    await page.locator('#chat-text').fill('保持已有测试数据。');
    task.revision=2;task.status='working';
    messages.filter(m=>m.kind==='change'&&m.role==='user').forEach(m=>{m.state='running';m.target_revision=2;});
    timeline.push(
      {revision:2,role:'system',title:'已收到修改要求',detail:'已关联第 1 版的 analysis.zip',state:'done',at:'2026-11-19T10:00:00+08:00'},
      {revision:2,round:1,role:'writer',harness:'codex',title:'读取作业要求与上版交付文件',state:'done',at:'2026-11-19T10:01:00+08:00'},
      {revision:2,round:1,role:'writer',harness:'codex',title:'核对当前版源码',detail:'src/main.go',state:'active',at:'2026-11-19T10:02:00+08:00'});
    await signalState();
    await page.locator('#chat-rebase').waitFor();
    assert.equal(await page.locator('#chat-send').isDisabled(),true);
    assert.equal(await page.locator('#chat-text').inputValue(),'保持已有测试数据。');
    await page.locator('#chat-rebase').click();
    await page.getByRole('checkbox',{name:'analysis.zip'}).check();
    await page.locator('#chat-context summary').click();
    if(screenshotDir) await page.screenshot({path:path.join(screenshotDir,'desktop-workflow-dialogue.png')});
    await page.locator('#chat-send').click();
    await page.locator('#conversation-log').getByText('保持已有测试数据。',{exact:true}).waitFor();
    assert.equal(lastRevision.revision,2);
    assert.equal(lastRevision.file_revision,1);
    const failed={id:'failed-question',role:'user',kind:'ask',text:'计算使用的采样率是多少？',files:['report.pdf'],file_revision:1,revision:2,state:'error',error:'主写暂时不可用'};
    messages.push(failed);
    await page.locator('#conversation-log').getByText(failed.error,{exact:true}).waitFor({timeout:8000});
    await page.getByRole('button',{name:'编辑并重试'}).click();
    assert.equal(await page.locator('#chat-text').inputValue(),failed.text);
    assert.equal(await page.locator('[data-kind=ask]').getAttribute('aria-pressed'),'true');
    await page.locator('#chat-send').click();
    await page.waitForFunction(()=>document.getElementById('chat-text').value==='');
    assert.notEqual(messages.at(-1).id,failed.id);
    assert.deepEqual(messages.at(-1).files,failed.files);
    task.revision=1;
    task.status='awaiting';await signalState();
    await page.locator('#tab-delivery').click();
    await page.locator('#submission-tree').getByRole('button',{name:'report.pdf',exact:true}).click();
    await page.locator('#report-edit').click();
    await page.locator('#paper .report-block').first().waitFor();
    assert.equal(electron.windows().length, 1);
    assert.equal(await page.locator('#return-task').getAttribute('href'), '/#view=task&id='+taskID);
    assert.equal(await page.locator('#agent-tools').isHidden(), true);
    await page.getByRole('button', {name: '引用意见'}).click();
    await page.locator('#paper .report-block').nth(1).click();
    assert.match(await page.locator('#instruction').inputValue(), /请注明频率单位/);
    await page.locator('#rewrite').click();
    await page.getByRole('button', {name: '采用建议', exact: true}).click();
    await page.locator('#paper').getByText('频率单位为 Hz。', {exact: true}).waitFor();
    assert.equal(editorRequests[0].selected, '这是独立的桌面测试材料。');
    await page.getByRole('tab', {name: '修改对照', exact: true}).click();
    assert.match(await page.locator('#diff-pane ins').innerText(), /Hz/);
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
    task.status = 'awaiting'; await signalState();
    await page.getByRole('button', {name: '交给主写更新产物'}).click();
    // A different editor saves while the version-specific handoff is being confirmed.
    draft.version++; draft.markdown += '\n\n另一页面的修改。';
    task.updated_at = new Date().toISOString(); await signalState();
    await page.locator('#submit-error').filter({hasText: '草稿已更新'}).waitFor();
    assert.equal(await page.locator('#submit-final').isDisabled(), true);
    await page.getByRole('button', {name: '继续编辑'}).click();
    await page.getByRole('button', {name: '交给主写更新产物'}).click();
    await page.getByRole('button', {name: '开始更新', exact: true}).click();
    await page.locator('#handoff-status').filter({hasText: '第 2 版'}).waitFor();
    assert.equal(await page.locator('#submit-open').isHidden(), true);
    task.revision = 2; task.status = 'awaiting'; await signalState();
    await page.locator('#stale').filter({hasText: '第 2 版'}).waitFor();
    assert.equal(await page.locator('#submit-open').isHidden(), true);
    published = 2;
    await page.locator('#return-task').click();
    await page.locator('#submission-tree').getByRole('button',{name:'src/main.go',exact:true}).click();
    await page.getByRole('button',{name:'版本改动',exact:true}).click();
    await page.locator('#file-changes .added-line').getByText('+ package main',{exact:true}).waitFor();
    await page.locator('#submission-tree').getByRole('button',{name:'report.pdf',exact:true}).click();
    await page.locator('#before-pdf-canvas').waitFor(); await page.locator('#after-pdf-canvas').waitFor();
    await page.locator('#submission-tree').getByRole('button',{name:'tools/main.go',exact:true}).click();
    await page.locator('#file-changes').getByText('两版文件内容相同。').waitFor();
    await page.getByRole('button',{name:'文件内容',exact:true}).click();
    await page.locator('#edit-text').click();
    assert.equal(await page.locator('#text-edit-content').inputValue(),'package tools');
    await page.locator('#text-edit-content').fill('package revised');
    await page.locator('#text-edit-send').click();
    await page.locator('#text-edit-dialog').waitFor({state:'hidden'});
    assert.equal(fileEdit.name,'tools/main.go'); assert.equal(fileEdit.revision,2); assert.equal(fileEdit.text,'package revised');
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
