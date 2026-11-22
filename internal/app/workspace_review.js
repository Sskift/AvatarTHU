'use strict';

const readerIDs = {
 prompt: {choice: 'prompt-choice', body: 'prompt-preview'},
 answer: {body: 'artifact-preview'}
};
let reading = null;

class ReviewReading {
 constructor(key) {
  this.key = key;
  try { this.state = JSON.parse(localStorage.getItem(this.key)); } catch {}
  if (!this.state || typeof this.state !== 'object') this.state = {};
  if (!['both', 'prompt', 'answer', 'changes'].includes(this.state.layout)) this.state.layout = 'both';
  if (!Array.isArray(this.state.bookmarks)) this.state.bookmarks = [];
  this.pairs = new Map();
 }
 changed() {
  clearTimeout(this.timer);
  this.timer = setTimeout(() => this.save(), 160);
 }
 save() {
  clearTimeout(this.timer);
  try { localStorage.setItem(this.key, JSON.stringify(this.state)); } catch {}
 }
}

function closeComparison() {
 if (!reading) return;
 reading.save();
 clearFileChanges();
 for (const {body} of Object.values(readerIDs)) closeFileSurface($(body));
 reading = null;
}

function taskPrompts(task) {
 const files = [...(task.attachments || [])];
 if (task.description) files.push({path: '', name: '学堂作业说明', text: task.description});
 return files;
}
function defaultPrompt(files) {
 const attachments = files.filter(f => f.path && !/^readme\.md$/i.test(f.name));
 return attachments.find(f => /\.pdf$/i.test(f.name)) || attachments[0] || files.find(f => !f.path) || files[0];
}
function selectReadingFile(side, path, member = '') {
 reading.state[side] = {path, member, page: 1, zoom: 'fit', top: 0, left: 0};
 reading.state.question = '';
 reading.changed();
}
function renderReader(side, root) {
 const body = $(readerIDs[side].body), back = $(side + '-back');
 const download = $(side === 'prompt' ? 'prompt-download' : 'artifact-download');
 back.hidden = true; download.hidden = true;
 if (!root) {
  closeFileSurface(body);
  body.innerHTML = empty(side === 'prompt' ? '学堂暂未提供原题或说明' : reading.ready ? '暂无可预览的文件' : '正在读取提交件…');
  return;
 }
 if (reading.state[side]?.path !== root.path) selectReadingFile(side, root.path, root.member);
 const position = reading.state[side];
 const file = position.member ? {...root, member:position.member, name:position.member.split('/').pop()} : root;
 if (file.member && (side === 'prompt' || reading.row?.group === 'extra')) {
  back.hidden = false;
  back.textContent = '‹ 返回压缩包 · ' + file.member;
  back.onclick = () => { selectReadingFile(side, root.path); renderComparison(); };
 }
 download.hidden = !file.path;
 if (file.path) download.href = fileURL(file.available === false ? {...file, member:''} : file);
 if (side === 'answer') {
  download.textContent = file.available === false ? '下载提交包 ↓' : '下载文件 ↓';
  $('answer-caption').textContent = file.member || file.name;
 }
 const session = previews.get(body);
 if (session?.position === position && session.file.text === file.text && session.file.size === file.size) return;
 const owner = reading;
 showFileSurface(body, file, {
  position, side, changed: () => owner.changed(),
  openMember: member => { selectReadingFile(side, root.path, member.member); renderComparison(); }
 });
}
function renderComparison() {
 const task = taskDetail, revision = latestDelivery(task);
 const key = 'assignment-reading:' + task.id + ':' + (revision?.revision || 0);
 if (reading?.key !== key) { closeComparison(); reading = new ReviewReading(key); }
 const owner = reading;
 loadReviewTree(owner, task, revision);
 const row = owner.row, layout = owner.state.layout, changes = layout === 'changes' && row && row.group !== 'extra';
 $('reading-pair').dataset.layout = layout;
 $('pane-delivery').dataset.layout = layout;
 $('reading-pair').hidden = !!changes; $('file-changes').hidden = !changes;
 for (const button of document.querySelectorAll('[data-layout]')) {
  button.setAttribute('aria-pressed', String(button.dataset.layout === (layout === 'prompt' ? 'both' : layout)));
  button.disabled = button.dataset.layout === 'changes' ? !row || row.group === 'extra' : row?.group === 'removed';
 }
 const prompts = taskPrompts(task), choice = $('prompt-choice');
 const options = prompts.map(f => '<option value="' + esc(f.path) + '">' + esc(f.title || f.name) + '</option>').join('');
 if (choice.optionsHTML !== options) { choice.innerHTML = options; choice.optionsHTML = options; }
 const prompt = prompts.find(f => f.path === owner.state.prompt?.path) || defaultPrompt(prompts);
 choice.closest('.viewer-toolbar').hidden = !prompt; if (prompt) choice.value = prompt.path;
 $('focus-prompt').textContent = layout === 'prompt' ? '返回对照' : '只看原题';
 $('answer-revision').textContent = row ? '第 ' + (row.group === 'removed' ? revision.revision-1 : revision.revision) + ' 版' : '';
 $('answer-caption').textContent = '';
 $('cite-position').disabled = !task.conversation?.can_ask && !task.conversation?.can_send;
 $('cite-position').hidden = !!changes || !row;
 renderReader('prompt', prompt);
 renderReader('answer', row?.file);
 renderFileHeader(owner, revision);
 if (changes) renderFileChanges(owner, revision);
 else clearFileChanges();
 updateQuestionNav();
}

// Use the prompt's numbering as the outline. Automatic links require a unique number in each file.
function questionNumber(value) {
 const digits = '零一二三四五六七八九';
 if (/^\d+$/.test(value)) return Number(value);
 if (value.includes('十')) {
  const [tens, ones] = value.split('十');
  return (tens ? digits.indexOf(tens) : 1) * 10 + (ones ? digits.indexOf(ones) : 0);
 }
 return digits.indexOf(value);
}
function questionMarker(line, parent) {
 const text = line.normalize('NFKC').trim().replace(/^#{1,6}\s+/, '');
 const compact = text.replace(/\s/g, '');
 const explicit = /^(?:(?:problem|question|exercise|q)([1-9]\d{0,2})(?!\d)|第([一二三四五六七八九十\d]+)[题問问])/i.exec(compact);
 const numbered = explicit || /^([1-9]\d{0,2})(?:[.、:)\s]+(?=[^\d\s])|(?=[\u3400-\u9fff(]))/.exec(text) || /^([一二三四五六七八九十]+)、/.exec(text);
 let rest = text;
 if (numbered) {
  parent = questionNumber(numbered[1] || numbered[2]);
  rest = (explicit ? compact : text).slice(numbered[0].length).trimStart();
 }
 const part = /^(?:\(([a-z]|\d{1,2})\)|([a-z])\))/i.exec(rest);
 if (!numbered && !part) return null;
 const suffix = part ? '(' + (part[1] || part[2]).toLowerCase() + ')' : '';
 return {parent, key: String(parent || '') + suffix, label: suffix ? (parent || '') + suffix : '第 ' + parent + ' 题'};
}
function pdfQuestionLines(page, items) {
 const viewport = page.getViewport({scale: 1});
 const words = items.filter(s => s.str?.trim()).map(s => {
  const [x, y] = viewport.convertToViewportPoint(s.transform[4], s.transform[5]);
  return {x, y, width: s.width, height: s.height, text: s.str};
 }).sort((a, b) => a.y - b.y || a.x - b.x);
 const lines = [];
 for (const word of words) {
  let line = lines.at(-1);
  if (!line || Math.abs(word.y - line.y) > 2) { line = {y: word.y, words: []}; lines.push(line); }
  line.words.push(word);
 }
 return lines.map(line => {
  let text = '', previous;
  for (const word of line.words.sort((a, b) => a.x - b.x)) {
   let skip = 0;
   if (previous) {
    const gap = word.x - previous.x - previous.width;
    if (gap > Math.max(1, word.height * 0.16)) text += ' ';
    // Simulated bold may repeat the end of one text run at the start of an overlapping run.
    if (-gap > Math.max(1, Math.min(previous.width, word.width) * 0.2)) {
     skip = Math.min(previous.text.length, word.text.length);
     while (skip && !text.endsWith(word.text.slice(0, skip))) skip--;
    }
   }
   text += word.text.slice(skip);
   previous = word;
  }
  const y = Math.max(0, (line.y - line.words[0].height) / viewport.height);
  return {text, y};
 });
}
async function pdfQuestions(pdf, current) {
 const markers = [];
 let parent = 0;
 for (let n = 1; n <= pdf.numPages && current(); n++) {
  const page = await pdf.getPage(n);
  if (!current()) break;
  const content = await page.getTextContent();
  for (const line of pdfQuestionLines(page, content.items)) {
   const mark = questionMarker(line.text, parent);
   if (!mark) continue;
   parent = mark.parent;
   markers.push({...mark, page: n, y: line.y});
  }
  page.cleanup();
  if (n < pdf.numPages) await new Promise(resolve => setTimeout(resolve, 0));
 }
 return markers;
}
function uniqueQuestions(markers) {
 const result = new Map();
 for (const mark of markers) result.set(mark.key, result.has(mark.key) ? null : mark);
 return result;
}
function readingFiles() {
 return JSON.stringify(Object.keys(readerIDs).map(side => {
  const pos = reading.state[side];
  return [pos?.path, pos?.member || ''];
 }));
}
function updateQuestionNav() {
 if (!reading?.ready || !$('question-nav')) return;
 const nav = $('question-nav');
 const prompt = previews.get($('prompt-preview')), answer = previews.get($('artifact-preview'));
 nav.hidden = !prompt;
 $('mark-position').hidden = !prompt?.canBookmark || !answer;
 const pairs = new Map();
 const indexed = Array.isArray(prompt?.marks) && (!answer || Array.isArray(answer.marks));
 const answers = uniqueQuestions(answer?.marks || []);
 for (const [key, mark] of uniqueQuestions(prompt?.marks || [])) {
  if (mark) pairs.set(key, {prompt: mark, answer: answers.get(key)});
 }
 const files = readingFiles();
 for (const bookmark of reading.state.bookmarks) {
  if (bookmark.files === files) pairs.set(bookmark.key, {prompt: bookmark.prompt, answer: bookmark.answer, bookmark});
 }
 if (indexed && !pairs.has(reading.state.question)) reading.state.question = '';
 reading.pairs = pairs;
 const selected = reading.state.question || '';
 const matched = [...pairs.values()].filter(pair => pair.answer).length;
 const html = pairs.size ? '<span>已对应 ' + matched + '/' + pairs.size + ' 题</span>'  + [...pairs].map(([key, pair]) => {
  const hint = !pair.answer && answer ? ' title="作答位置待定位" class="unpaired"' : '';
  return '<button type="button" data-question="' + esc(key) + '" aria-pressed="' + (key === selected) + '"' + hint + '>' + esc(pair.prompt.label) + '</button>';
 }).join('') : '<span>' + (indexed ? (answer ? '可分别翻阅原题与作答。' : '原题') : '正在查找题号…') + '</span>';
 if (nav.contents !== html) {
  const focused = nav.contains(document.activeElement) ? document.activeElement.dataset.question : '';
  nav.innerHTML = html; nav.contents = html;
  if (focused) [...nav.querySelectorAll('button')].find(button => button.dataset.question === focused)?.focus({preventScroll: true});
 }
 const pair = pairs.get(selected);
 const hint = $('question-hint');
 hint.hidden = !pair || !!pair.answer || !answer || !indexed;
 if (!hint.hidden) hint.textContent = '已定位到原题。作答中未识别到同一题号，可自行翻到对应位置。';
 prompt?.mark?.(pair?.prompt);
 answer?.mark?.(pair?.answer);
}
function jumpToQuestion(key) {
 const pair = reading?.pairs.get(key);
 if (!pair) return;
 reading.state.question = key;
 for (const [side, ids] of Object.entries(readerIDs)) {
  if (pair[side]) previews.get($(ids.body))?.jump?.(pair[side]);
 }
 updateQuestionNav();
 reading.changed();
}
function openReadingMark() {
 const old = reading.pairs.get(reading.state.question);
 const draft = {owner: reading, files: readingFiles(), old: old?.bookmark};
 const context = [];
 for (const [side, ids] of Object.entries(readerIDs)) {
  const session = previews.get($(ids.body));
  if (!session || (side === 'prompt' && !session.canBookmark)) return;
  const {page, zoom, top, left} = session.position;
  draft[side] = {manual: true, page, zoom, y: top, left};
  context.push((side === 'prompt' ? '原题' : '作答') + ' ' + (session.file.member || session.file.name) + (session.pdf ? ' 第 ' + page + ' 页' : ''));
 }
 const dialog = $('mark-dialog');
 dialog.draft = draft;
 $('mark-context').textContent = context.join(' · ');
 $('mark-name').value = old?.prompt.label || '';
 $('remove-mark').hidden = !draft.old;
 dialog.showModal(); $('mark-name').select();
}
function finishReadingMark(remove = false) {
 const dialog = $('mark-dialog'), draft = dialog.draft;
 const label = $('mark-name').value.trim();
 if (!remove && !label) { $('mark-name').focus(); return; }
 dialog.close();
 if (!reading || draft?.owner !== reading || draft.files !== readingFiles()) { toast('文件或版本已变化，请重新标记。'); return; }
 const entry = [...reading.pairs].find(([, pair]) => pair.prompt.label === label);
 const key = remove ? draft.old.key : entry?.[0] || 'mark-' + crypto.randomUUID();
 reading.state.bookmarks = reading.state.bookmarks.filter(mark => mark.files !== draft.files || mark.key !== key);
 if (!remove) reading.state.bookmarks.push({key, files: draft.files, prompt: {...draft.prompt, label}, answer: draft.answer});
 reading.state.question = remove ? '' : key;
 updateQuestionNav(); reading.save();
}
function citeReadingPosition(kind) {
 const task = taskDetail, draft = draftFor(task);
 if (!reading || draft.sending) return;
 const input = $('chat-text'), pinned = pinnedDelivery(task);
 if (((draft.text || draft.files.length) && draft.revision !== task.revision) ||
     (draft.files.length && draft.fileRevision !== pinned?.revision)) {
  draft.error = '请先核对草稿关联的版本，再引用当前位置。';
  renderComposer(task); input.focus(); return;
 }
 const pair = reading.pairs.get(reading.state.question);
 const locations = [];
 if (pair) locations.push(pair.prompt.label);
 for (const [side, ids] of Object.entries(readerIDs)) {
  const session = previews.get($(ids.body));
  if (!session) continue;
  const label = side === 'prompt' ? '原题' : '第 ' + latestDelivery(task).revision + ' 版作答';
  locations.push(label + ' ' + (session.file.member || session.file.name) + (session.pdf ? ' 第 ' + session.position.page + ' 页' : ''));
 }
 if (kind === 'change') draft.kind = 'change';
 else if (!draft.text && !draft.files.length) draft.kind = task.conversation?.can_ask ? 'ask' : 'change';
 const answer = pinned?.files?.find(f => f.path === reading.state.answer?.path) ||
  (reading.row?.group === 'file' && pinned?.files?.find(f => f.submission_member !== false && (f.source_path || f.name).replace(/^final\//,'') === reading.row.name));
 if (answer && !draft.files.includes(answer.name)) {
  draft.files.push(answer.name);
  draft.fileRevision = pinned.revision;
 }
 draft.revision = task.revision;
 draft.id = ''; draft.error = '';
 draft.text = [draft.text.trimEnd(), (kind === 'change' ? '请修改 ' : '') + locations.join(' · ')].filter(Boolean).join('\n') + '\n';
 input.value = draft.text;
 renderComposer(task);
 input.focus();
 input.setSelectionRange(input.value.length, input.value.length);
}

async function previewPDF(body, session, current) {
 const prefix = session.controlPrefix || (session.side === 'prompt' ? 'prompt-pdf-' : session.side === 'answer' ? 'artifact-pdf-' : 'pdf-');
 const control = key => body.querySelector('#' + prefix + key);
 body.innerHTML = '<div class="pdf-controls"><div class="pdf-pagination" id="' + prefix + 'pagination"><button id="' + prefix + 'prev" disabled>上一页</button><label><input id="' + prefix + 'page" type="number" min="1" value="1" aria-label="PDF 页码" disabled> / <span id="' + prefix + 'count">…</span></label><button id="' + prefix + 'next" disabled>下一页</button></div><select id="' + prefix + 'zoom" aria-label="PDF 缩放" disabled><option value="fit">适合宽度</option><option value="page">适合页面</option><option value="1">100%</option><option value="1.5">150%</option><option value="2">200%</option></select></div><div id="' + prefix + 'status" class="pdf-status" role="status">正在打开 PDF…</div><div class="pdf-page-wrap"><div class="pdf-sheet"><div class="pdf-location" hidden></div></div></div>';
 const lib = await import('/pdfjs/pdf.min.mjs');
 if (!current()) return;
 lib.GlobalWorkerOptions.workerSrc = '/pdfjs/pdf.worker.min.mjs';
 session.loading = lib.getDocument({url: fileURL(session.file, true), cMapUrl: '/pdfjs/cmaps/', cMapPacked: true,
  standardFontDataUrl: '/pdfjs/standard_fonts/', wasmUrl: '/pdfjs/wasm/', isEvalSupported: false, disableAutoFetch: true, disableStream: true});
 const pdf = await session.loading.promise;
 if (!current()) return;
 session.pdf = pdf;
 const pos = session.position, wrap = body.querySelector('.pdf-page-wrap'), sheet = body.querySelector('.pdf-sheet');
 const marker = sheet.querySelector('.pdf-location'), zoom = control('zoom'), pageInput = control('page');
 const prev = control('prev'), next = control('next'), status = control('status');
 control('pagination').hidden = pdf.numPages === 1;
 control('count').textContent = pdf.numPages;
 pageInput.max = pdf.numPages;
 pos.page = Math.max(1, Math.min(pdf.numPages, Number(pos.page) || 1));
 zoom.value = ['fit', 'page', '1', '1.5', '2'].includes(pos.zoom) ? pos.zoom : 'fit';
 pos.zoom = zoom.value;
 let generation = 0, drawing = false, drawnSize = '', location;
 const sizeKey = () => [body.clientWidth, body.clientHeight, pos.zoom, pos.page].join('/');
 const mark = anchor => {
  location = anchor;
  marker.hidden = !anchor || anchor.manual || anchor.page !== pos.page;
  if (!marker.hidden) marker.style.top = (anchor.y * 100) + '%';
  // Leave just enough room after the page to align a question near its foot with the opposite reader.
  const fraction = anchor?.page === pos.page ? anchor.y : session.side && reading?.state.question && !session.marks ? pos.top : 0;
  const tail = fraction ? Math.max(0, wrap.clientHeight - (1 - fraction) * sheet.clientHeight - 26) : 0;
  wrap.style.setProperty('--pdf-tail', Math.ceil(tail) + 'px');
 };
 const draw = async () => {
  if (!current()) return;
  if (!body.clientWidth || !body.clientHeight) { drawnSize = ''; return; }
  const id = ++generation, number = pos.page, size = sizeKey();
  const latest = () => current() && id === generation;
  session.render?.cancel();
  drawing = true;
  prev.disabled = number <= 1; next.disabled = number >= pdf.numPages;
  pageInput.disabled = false; zoom.disabled = false; pageInput.value = number;
  status.textContent = '正在显示第 ' + number + ' 页…';
  try {
   const page = await pdf.getPage(number);
   if (!latest()) return;
   const base = page.getViewport({scale: 1});
   const widthScale = (wrap.clientWidth - 28) / base.width;
   const heightScale = (body.clientHeight - body.querySelector('.pdf-controls').offsetHeight - 28) / base.height;
   const scale = pos.zoom === 'fit' ? widthScale : pos.zoom === 'page' ? Math.min(widthScale, heightScale) : Number(pos.zoom);
   const viewport = page.getViewport({scale: Math.max(0.1, scale)}), ratio = Math.min(window.devicePixelRatio || 1, 2);
   const canvas = document.createElement('canvas');
   canvas.id = prefix + 'canvas'; canvas.setAttribute('role', 'img');
   canvas.setAttribute('aria-label', 'PDF 第 ' + number + ' 页，共 ' + pdf.numPages + ' 页');
   canvas.width = Math.ceil(viewport.width * ratio); canvas.height = Math.ceil(viewport.height * ratio);
   canvas.style.width = Math.floor(viewport.width) + 'px'; canvas.style.height = Math.floor(viewport.height) + 'px';
   session.render = page.render({canvasContext: canvas.getContext('2d'), viewport, transform: ratio === 1 ? null : [ratio,0,0,ratio,0,0]});
   await session.render.promise;
   if (!latest()) return;
   sheet.replaceChildren(canvas, marker);
   status.textContent = '';
   mark(location);
   wrap.scrollTop = (pos.top || 0) * sheet.clientHeight;
   wrap.scrollLeft = (pos.left || 0) * sheet.clientWidth;
   drawnSize = size;
   page.cleanup();
  } catch (e) {
   if (latest() && e.name !== 'RenderingCancelledException') status.textContent = '此页未能显示。' + e.message;
  } finally { if (latest()) drawing = false; }
 };
 session.mark = mark;
 session.jump = anchor => {
  pos.page = anchor.page; pos.top = anchor.y; pos.left = anchor.left || 0;
  if (anchor.manual) { pos.zoom = anchor.zoom; zoom.value = pos.zoom; }
  mark(anchor); draw();
 };
 const manualPage = value => {
  pos.page = Math.max(1, Math.min(pdf.numPages, Math.trunc(Number(value)) || 1));
  pos.top = 0; pos.left = 0;
  if (session.side && reading) {
   if (session.side !== 'answer' || reading.pairs.get(reading.state.question)?.answer) reading.state.question = '';
   updateQuestionNav();
  }
  session.changed?.(); draw();
 };
 prev.onclick = () => manualPage(pos.page - 1);
 next.onclick = () => manualPage(pos.page + 1);
 pageInput.onchange = () => manualPage(pageInput.value);
 zoom.onchange = () => { pos.zoom = zoom.value; session.changed?.(); draw(); };
 wrap.onscroll = () => {
  if (!current() || drawing || !wrap.clientWidth || !sheet.clientHeight) return;
  pos.top = wrap.scrollTop / sheet.clientHeight;
  pos.left = wrap.scrollLeft / sheet.clientWidth;
  session.changed?.();
 };
 await draw();
 if (!current()) return;
 session.observer = new ResizeObserver(() => {
  if (body.clientWidth && body.clientHeight && sizeKey() !== drawnSize) draw();
 });
 session.observer.observe(body);
 if (session.side) {
  session.canBookmark = true; updateQuestionNav();
  try { session.marks = await pdfQuestions(pdf, current); } catch { session.marks = []; }
  if (current()) updateQuestionNav();
 }
}
