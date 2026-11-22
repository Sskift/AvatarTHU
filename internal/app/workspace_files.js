'use strict';

const changeLabels = {first:'首版', added:'新增', modified:'已修改', removed:'已移除', unchanged:'无改动', unavailable:'上版不可用'};
const fileGlyph = '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 1.5h5l3 3v10H4z M9 1.5V5h3"/></svg>';

function fileInSubmission(root, entry) {
 if (!root || !entry) return null;
 const member = /\.zip$/i.test(root.name) ? entry.name : '';
 return {...root, name:entry.name.split('/').pop(), member, size:entry.size, mode:entry.mode, available:entry.available};
}
function reviewRows(revision, manifest) {
 const rows = new Map(), included = new Set();
 for (const entry of manifest?.files || []) {
  const file = fileInSubmission(manifest.submission, entry.current);
  const previous = fileInSubmission(manifest.previous, entry.previous);
  const group = file ? 'file' : 'removed', key = group + '/' + entry.name;
  rows.set(key, {...entry, key, group, file:file || previous, previous});
  if (file) included.add(entry.name);
 }
 for (const f of revision?.files || []) {
  const relative = (f.source_path || f.name).replace(/^final\//, '');
  if (f.path === revision?.submission?.path || (f.submission_member !== false && included.has(relative))) continue;
  const key = 'extra/' + f.path;
  rows.set(key, {key, name:f.name, file:f, group:'extra'});
 }
 return rows;
}
function defaultReviewRow(rows) {
 const actual = [...rows.values()].filter(row => row.group === 'file');
 return actual.find(r => /^(?:.*\/)?report\.pdf$/i.test(r.name)) || actual.find(r => /\.pdf$/i.test(r.name)) ||
  actual.find(r => /^(?:.*\/)?readme\.md$/i.test(r.name)) || actual[0] || rows.values().next().value;
}
function selectReviewRow(owner, key) {
 const state = owner.state, row = owner.rows.get(key);
 if (!row || owner.row === row) return;
 state.filePositions ||= {};
 if (owner.row) state.filePositions[owner.row.key] = structuredClone({prompt:state.prompt, answer:state.answer, question:state.question});
 const first = !owner.row, saved = first && state.selected === key ? null : state.filePositions[key];
 if (saved) Object.assign(state, saved);
 else if (!first || state.answer?.path !== row.file.path || (row.file.member && state.answer?.member !== row.file.member)) {
  state.answer = null; state.question = '';
 }
 if (state.answer?.path !== row.file.path || (row.group !== 'extra' && (state.answer?.member || '') !== row.file.member))
  selectReadingFile('answer', row.file.path, row.file.member);
 owner.row = row; state.selected = key;
 if (row.group === 'removed') state.layout = 'changes';
 else if (row.group === 'extra' && state.layout === 'changes') state.layout = 'both';
 for (const button of document.querySelectorAll('[data-review-file]')) {
  const selected = button.dataset.reviewFile === key;
  if (selected) {
   button.setAttribute('aria-current', 'true');
   for (let dir = button.closest('details'); dir; dir = dir.parentNode.closest('details')) dir.open = true;
  } else button.removeAttribute('aria-current');
 }
 owner.changed();
}
function treeFile(row) {
 const label = row.name.split('/').pop();
 const mark = {modified:'改', added:'新', removed:'删'}[row.change];
 return '<button type="button" class="tree-file" data-review-file="' + esc(row.key) + '" title="' + esc(row.name) +
  '" aria-label="' + esc(row.name) + '">' + fileGlyph + '<span>' + esc(label) + '</span>' +
  (mark ? '<small class="change-' + row.change + '" title="' + changeLabels[row.change] + '">' + mark + '</small>' : '') + '</button>';
}
function directoryTree(rows, directories, owner) {
 const root = {dirs:new Map(), files:[]};
 function folder(parts) {
  let here = root;
  for (const part of parts) {
   if (!here.dirs.has(part)) here.dirs.set(part, {dirs:new Map(), files:[]});
   here = here.dirs.get(part);
  }
  return here;
 }
 for (const path of directories || []) folder(path.split('/'));
 for (const row of rows) folder(row.name.split('/').slice(0,-1)).files.push(row);
 const sort = (a,b) => a.localeCompare(b, 'en', {numeric:true});
 function children(node, parent) {
  const dirs = [...node.dirs.keys()].sort(sort).map(name => {
   const full = parent ? parent + '/' + name : name;
   const open = owner.state.folders?.['dir/' + full] ?? (rows.length <= 25 && !parent);
   return '<details class="tree-dir" data-folder="' + esc(full) + '"' + (open?' open':'') + '><summary>' + folderIcon + '<span>' +
    esc(name) + '</span></summary><div class="tree-children">' + children(node.dirs.get(name), full) + '</div></details>';
  });
  return dirs.join('') + node.files.sort((a,b) => sort(a.name,b.name)).map(treeFile).join('');
 }
 return children(root, '');
}
function renderReviewTree(owner, revision) {
 const rows = [...owner.rows.values()], actual = rows.filter(r => r.group === 'file');
 $('tree-count').textContent = owner.manifest ? '第 ' + revision.revision + ' 版 · ' + actual.length + ' 个文件' : '';
 $('submission-tree').innerHTML = owner.treeError ? '<p class="tree-message">' + esc(owner.treeError) +
  '</p><button type="button" id="retry-tree">重新载入目录</button>' :
  directoryTree(actual, owner.manifest?.directories, owner) || '<p class="tree-message">暂无已发布的提交文件</p>';
 const group = (key, title) => {
  const items = rows.filter(row => row.group === key);
  return items.length ? '<details class="tree-group"><summary>' + title + '<small>' + items.length + '</small></summary>' +
   items.map(row => treeFile(row)).join('') + '</details>' : '';
 };
 $('related-files').innerHTML = group('removed','上版已移除') + group('extra', owner.treeError ? '可用产物' : '编辑与展示文件');
 const download = $('submission-download');
 download.hidden = !owner.manifest?.submission;
 if (!download.hidden) download.href = fileURL(owner.manifest.submission);
}
function loadReviewTree(owner, task, revision) {
 const key = JSON.stringify([revision?.revision, revision?.submission?.path, revision?.submission?.sha256]);
 if (owner.treeKey === key) return;
 owner.treeKey = key; owner.ready = false; owner.rows = new Map(); owner.row = null; owner.treeError = '';
 const complete = (manifest, message) => {
  if (reading !== owner || owner.treeKey !== key) return;
  owner.manifest = manifest; owner.treeError = message; owner.ready = true;
  owner.rows = reviewRows(revision, manifest);
  renderReviewTree(owner, revision);
  const row = owner.rows.get(owner.state.selected) || defaultReviewRow(owner.rows);
  if (row) selectReviewRow(owner, row.key);
  renderComparison();
 };
 if (!revision?.submission) {
  complete(null, revision ? '该版未保留可核实的提交件。' : '');
  return;
 }
 $('tree-count').textContent = '正在读取…'; $('submission-tree').textContent = '';
 $('related-files').replaceChildren(); $('submission-download').hidden = true;
 api('/task/' + encodeURIComponent(task.id) + '/submission-tree?revision=' + revision.revision)
  .then(data => complete(data, ''), err => complete(null, err.message));
}
function showTree(show) {
 $('review-file-list').hidden = !show;
 $('toggle-tree').setAttribute('aria-expanded', String(show));
 $('toggle-tree').textContent = show ? '收起目录' : '显示目录';
}
function editReportFor(row, task) {
 if (!row || !task.has_report_source || !task.can_submit || row.group === 'removed') return false;
 if (/^(?:report|报告)\.(?:pdf|md|html?)$/i.test(row.file.name)) return true;
 // Older reports may use the assignment title as the PDF name.
 return row.group === 'file' && /\.pdf$/i.test(row.name) && reading.manifest?.files.filter(f=>f.current).length === 1;
}
function renderFileHeader(owner, revision) {
 const row = owner.row, path = row?.name || '', name = path.split('/').pop();
 const parent = path.slice(0, Math.max(0,path.length - name.length - 1));
 $('selected-parent').hidden = !parent; $('selected-parent').textContent = parent + ' /';
 $('selected-name').textContent = name || (owner.ready ? '作业原题' : '正在读取提交目录…');
 $('selected-meta').textContent = row ? [row.group === 'extra' ? '编辑与展示文件' : '第 ' + (row.group === 'removed' ? revision.revision-1 : revision.revision) + ' 版',
  size(row.file.size), changeLabels[row.change]].filter(Boolean).join(' · ') : '';
 const edit = $('report-edit');
 edit.hidden = !editReportFor(row, taskDetail);
 if (!edit.hidden) edit.href = '/editor#task=' + encodeURIComponent(taskDetail.id);
 updateTextEditButton();
}
function updateTextEditButton() {
 const row = reading?.row, session = previews.get($('artifact-preview'));
 if (!$('edit-text')) return;
 $('edit-text').hidden = !taskDetail?.can_submit || reading?.manifest?.revision !== taskDetail.revision || row?.group !== 'file' || row.file.path !== session?.file.path ||
  (row.file.member || '') !== (session.file.member || '') || session.editText === undefined || !$('report-edit').hidden;
 $('ask-file-change').hidden = !row || row.group === 'removed' || !taskDetail?.conversation?.can_send || !$('report-edit').hidden || !$('edit-text').hidden;
}
function clearFileChanges() {
 const view = $('file-changes');
 if (!view) return;
 for (const el of view.querySelectorAll('.preview-surface')) closeFileSurface(el);
 view.replaceChildren(); view.viewKey = null;
}
function fileNotes(row, revision) {
 const names = new Set(['final/' + row.name, row.name]);
 const outer = revision.files?.find(f => f.path === row.file.path && !row.file.member);
 if (outer?.source_path) names.add(outer.source_path);
 return (revision.revision_notes || []).filter(note => note.files?.some(name => names.has(name)));
}
function changeReader(view, label, file, prefix) {
 const box = document.createElement('section'); box.className = 'reader';
 const heading = document.createElement('div'); heading.className = 'reader-heading';
 const title = document.createElement('h3'); title.textContent = label;
 const link = document.createElement('a'); link.textContent = '下载'; link.download = '';
 const available = file.available !== false;
 link.href = fileURL(available ? file : {...file, member:''}); if (!available) link.textContent = '下载提交包';
 heading.append(title,link);
 const body = document.createElement('div'); body.className = 'preview-surface viewer';
 box.append(heading,body); view.append(box);
 if (available) showFileSurface(body, file, {controlPrefix:prefix});
 else body.innerHTML = empty('下载该版提交包可查看此文件');
}
async function renderFileChanges(owner, revision) {
 const view = $('file-changes'), row = owner.row;
 if (!row || view.viewKey === row) return;
 clearFileChanges(); view.viewKey = row;
 const current = () => reading === owner && view.viewKey === row && view.isConnected;
 const n = revision.revision, notes = fileNotes(row, revision);
 const messages = {first:'这是第一个提交版本。', unavailable:'上一版提交件缺失或与原记录不符，无法核对改动。',
  unchanged:'两版文件内容相同。', added:'该路径首次出现在本版提交目录。', removed:'本版提交目录已移除该路径。'};
 const heading = row.change === 'first' ? '第 ' + n + ' 版' : '第 ' + (n-1) + ' 版 → 第 ' + n + ' 版';
 view.innerHTML = '<div class="change-heading"><h3>' + heading + '</h3><span>' + esc(changeLabels[row.change] || '') + '</span></div>';
 if (notes.length) view.insertAdjacentHTML('beforeend', '<div class="file-notes"><h4>主写对这个文件的修改说明</h4><ul>' +
  notes.map(note => '<li>' + esc(note.detail || note.request) + '</li>').join('') + '</ul></div>');
 if (messages[row.change]) view.insertAdjacentHTML('beforeend', '<p class="change-message">' + messages[row.change] + '</p>');
 if (row.change !== 'modified' && row.change !== 'removed' && row.change !== 'added') return;
 if (row.previous && row.current) {
  const details = [size(row.previous.size) + ' → ' + size(row.current.size)];
  if (row.previous.mode && row.current.mode && row.previous.mode !== row.current.mode) details.push('权限 ' + row.previous.mode + ' → ' + row.current.mode);
  view.insertAdjacentHTML('beforeend','<p class="change-message">' + esc(details.join(' · ')) + '</p>');
 }
 if (row.change === 'modified' && row.file.available && row.previous.available && row.file.size <= 256*1024 && row.previous.size <= 256*1024 &&
     !/\.(pdf|png|jpe?g|gif|webp|zip)$/i.test(row.file.name)) {
  try {
   const query = new URLSearchParams({revision:n, name:row.name});
   const diff = await api('/task/' + encodeURIComponent(taskDetail.id) + '/file-change?' + query);
   if (!current()) return;
   if (diff.lines) {
    const pre = document.createElement('pre'); pre.className = 'line-diff'; pre.setAttribute('aria-label','逐行改动');
    for (const line of diff.lines.split('\n')) {
     const span = document.createElement('span'); span.textContent = line;
     if (line.startsWith('+ ')) span.className = 'added-line';
     if (line.startsWith('- ') && !line.startsWith('- 上版删除的行')) span.className = 'removed-line';
     pre.append(span);
    }
    view.append(pre);
    return;
   }
  } catch(e) { if (!current()) return; const p = document.createElement('p'); p.textContent = '逐行对照暂不可用。' + e.message; view.append(p); }
 }
 const pair = document.createElement('div'); pair.className = 'change-pair'; view.append(pair);
 if (row.previous) changeReader(pair, '上版 · 第 ' + (n-1) + ' 版', row.previous, 'before-pdf-');
 if (row.current) changeReader(pair, '本版 · 第 ' + n + ' 版', row.file, 'after-pdf-');
}

function openTextEdit() {
 const owner = reading, row = owner?.row, session = previews.get($('artifact-preview'));
 if (!row || $('edit-text').hidden || session?.editText === undefined) return;
 const dialog = $('text-edit-dialog'), revision = owner.manifest.revision;
 const base = session.editText, key = 'file-draft:' + JSON.stringify([taskDetail.id,revision,owner.manifest.submission.sha256,row.name]);
 let saved;
 try { saved = JSON.parse(localStorage.getItem(key)); } catch {}
 const draft = {key, base, task:taskDetail.id, revision, name:row.name, hash:owner.manifest.submission.sha256,
  id:saved?.id || crypto.randomUUID().replaceAll('-','')};
 dialog.draft = draft;
 $('text-edit-file').textContent = '第 ' + revision + ' 版 · ' + row.name;
 $('text-edit-content').value = saved?.text ?? base;
 $('text-edit-error').hidden = true;
 updateTextDraft(false);
 dialog.showModal();
 $('text-edit-content').focus();
}
function updateTextDraft(changed) {
 const draft = $('text-edit-dialog').draft;
 if (!draft) return;
 const value = $('text-edit-content').value;
 const dirty = value !== draft.base.replace(/\r\n?/g,'\n');
 if (changed) draft.id = crypto.randomUUID().replaceAll('-','');
 let saved = false;
 try {
  if (dirty) { localStorage.setItem(draft.key, JSON.stringify({text:value,id:draft.id})); saved = true; }
  else localStorage.removeItem(draft.key);
 } catch {}
 $('text-edit-status').textContent = dirty ? (saved ? '草稿已保存在本机。' : '草稿目前仅在此窗口。') + '交给主写后将生成新版并独立复审。' : '修改后可交给主写生成新版并独立复审。';
 $('text-edit-send').disabled = !dirty || draft.sending;
}
async function sendTextEdit(event) {
 event.preventDefault();
 const dialog = $('text-edit-dialog'), draft = dialog.draft;
 if (!draft || $('text-edit-send').disabled) return;
 const input = $('text-edit-content'), error = $('text-edit-error');
 // Textareas normalize newlines. Keep the source convention, including a trailing newline.
 const endings = draft.base.match(/\r\n|\r|\n/g) || [];
 let index = 0;
 const content = input.value.replace(/\n/g, () => endings[index++] || endings.at(-1) || '\n');
 draft.sending = true; input.readOnly = true;
 $('text-edit-send').disabled = true; dialog.querySelector('[data-close]').disabled = true; error.hidden = true;
 try {
  const updated = await api('/task/' + encodeURIComponent(draft.task) + '/file-edit',
   {id:draft.id, revision:draft.revision, name:draft.name, base_hash:draft.hash, text:content});
  try { localStorage.removeItem(draft.key); } catch {}
  if (dialog.open) dialog.close();
  if (taskDetail?.id === updated.id && currentView === 'task') renderTask(updated);
  toast('文件修改已交给主写，等待生成新版。');
 } catch(e) { error.textContent = e.message; error.hidden = false; }
 finally { draft.sending = false; input.readOnly = false; dialog.querySelector('[data-close]').disabled = false; if (dialog.open) updateTextDraft(false); }
}
document.addEventListener('click', e => {
 const button = e.target.closest('button');
 if (!button || !reading) return;
 if (button.dataset.reviewFile) {
  selectReviewRow(reading, button.dataset.reviewFile);
  if (window.matchMedia('(max-width:760px)').matches) showTree(false);
  renderComparison();
 } else if (button.id === 'toggle-tree') {
  showTree($('review-file-list').hidden);
 } else if (button.id === 'retry-tree') {
  reading.treeKey = ''; renderComparison();
 } else if (button.id === 'focus-prompt') {
  reading.state.layout = reading.state.layout === 'prompt' ? 'both' : 'prompt';
  reading.changed(); renderComparison();
 } else if (button.id === 'edit-text') openTextEdit();
 else if (button.id === 'ask-file-change') citeReadingPosition('change');
});
document.addEventListener('toggle', e => {
 if (reading && e.target.matches('details[data-folder]')) {
  reading.state.folders ||= {};
  reading.state.folders['dir/' + e.target.dataset.folder] = e.target.open;
  reading.changed();
 }
}, true);
document.getElementById('text-edit-content').addEventListener('input', () => updateTextDraft(true));
document.getElementById('text-edit-form').addEventListener('submit', sendTextEdit);
document.getElementById('text-edit-dialog').addEventListener('cancel', e => { if (e.target.draft?.sending) e.preventDefault(); });
