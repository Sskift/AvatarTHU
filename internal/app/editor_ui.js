'use strict';
const $ = id => document.getElementById(id);
let editingRange = null;
let multiple = false, selectedBlocks = new Set(), selectionAnchor = -1;
let taskID = '', view, markdown = '', blocks = [], activeBlock = -1, chosen = null, mode = 'paper';
let dirty = false, saveTimer, renderTimer, rendering = 0, savePromise = null, conflict = false, submitting = false;
let submitID = null, rewriteID = null, handoffVersion = null, requesting = false, pollBusy = false, pollError = '';
let reviewKey = '', suggestionsKey = '';
const harness = name => ({claude: 'Claude', codex: 'Codex'}[String(name).toLowerCase()] || name || 'Agent');
const phaseNames = {awaiting:'待你审阅',needs_student:'需要你处理',submitting:'正在提交学堂',revision_requested:'等待修改意见',revision_ready:'已排队',queued:'已排队',working:'更新产物中',rewriting:'按复审意见修改中',reviewing:'独立复审中',delivery_pending:'发布产物中',failed:'处理遇到问题',submitted:'已提交学堂',approval_invalid:'需要重新确认',ignored:'已忽略',closed_remote:'课程作业已关闭',submission_unknown:'提交结果待确认'};
const escape = text => String(text ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const requestID = () => crypto.randomUUID().replaceAll('-','');
function error(message) { $('error').textContent = message; $('error').hidden = !message; }
function toast(message) { $('toast').textContent=message; $('toast').hidden=false; setTimeout(()=>{$('toast').hidden=true;},3500); }
async function api(path, body) {
 const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),15000);
 try {
  const response=await fetch(path,{signal:controller.signal,method:body===undefined?'GET':'POST',headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
  const text=await response.text(); let data; try{data=JSON.parse(text);}catch{data={error:text};}
  if(!response.ok) throw new Error(data.error || '后台暂时不可用'); return data;
 } catch(e) {
  if(e.name==='AbortError'||e instanceof TypeError)throw new Error('暂时连不上后台，正在尝试重新连接。后台恢复后此页会重新连接；尚未保存的文字仍保留在页面中。');
  throw e;
 } finally {clearTimeout(timer);}
}
const endpoint = action => '/api/task/'+encodeURIComponent(taskID)+(action?'/'+action:'');
new ResizeObserver(entries=>document.documentElement.style.setProperty('--topbar-height',entries[0].target.offsetHeight+'px')).observe(document.querySelector('.topbar'));
function splitBlocks(text) {
 const list=[]; let start=0,pos=0,fence='';
 for(const line of text.match(/[^\n]*\n|[^\n]+$/g)||[]) {
  const mark=line.match(/^\s{0,3}(`{3,}|~{3,})/);
  if(mark) {if(!fence)fence=mark[1];else if(mark[1][0]===fence[0]&&mark[1].length>=fence.length)fence='';}
  pos+=line.length;
  if(!fence && !line.trim()) {if(text.slice(start,pos).trim())list.push({start,end:pos,text:text.slice(start,pos)});start=pos;}
 }
 if(start<text.length && text.slice(start).trim())list.push({start,end:text.length,text:text.slice(start)});
 return list;
}
async function render() {
 const id=++rendering, snapshot=markdown, currentTask=taskID; blocks=splitBlocks(snapshot);
 const rendered=await api(endpoint('render'),{markdown:snapshot,blocks:blocks.map(b=>b.text)});
 if(id!==rendering || currentTask!==taskID)return;
 $('paper').innerHTML=blocks.length?rendered.blocks.map((html,i)=>'<section class="report-block" tabindex="0" data-block="'+i+'"><button class="block-picker" data-toggle-block="'+i+'" aria-label="选择第 '+(i+1)+' 段" aria-pressed="false">✓</button>'+html+'</section>').join(''):'<p class="muted">这版还没有 Markdown 报告。切换到 Markdown 粘贴正文，或等待下一版产出可编辑源文件。</p>';
 updateSelectionUI();
 const headings=blocks.map((b,i)=>({i,match:b.text.match(/^#{1,3}\s+(.+)/)})).filter(h=>h.match);
 $('outline-menu').hidden=!headings.length;
 $('outline').innerHTML=headings.map(h=>'<button data-jump="'+h.i+'">'+escape(h.match[1])+'</button>').join('');
 renderDiff();
}
function renderDiff() {
 const old=view?.draft.base_markdown||'', now=markdown;
 if(old===now){$('diff-pane').innerHTML='<p class="muted">还没有修改。你修改的文字会显示在这里。</p>';return;}
 let a=0,z=0;while(a<old.length&&a<now.length&&old[a]===now[a])a++;
 while(z<old.length-a&&z<now.length-a&&old[old.length-1-z]===now[now.length-1-z])z++;
 const marked=(s,tag)=>escape(s.slice(0,a))+'<'+tag+'>'+escape(s.slice(a,z?s.length-z:s.length))+'</'+tag+'>'+escape(z?s.slice(-z):'');
 $('diff-pane').innerHTML='<div class="diff-label">发布的原文</div><div class="diff-text">'+marked(old,'del')+'</div><div class="diff-label">你的草稿</div><div class="diff-text">'+marked(now,'ins')+'</div>';
}
function changed(next) {
 markdown=next;dirty=true;$('save-state').textContent='正在保存…';error('');updateHandoffButton();
 clearTimeout(saveTimer);saveTimer=setTimeout(()=>save().catch(e=>error(e.message)),650);
 clearTimeout(renderTimer);renderTimer=setTimeout(()=>render().catch(e=>error(e.message)),300);
}
async function save() {
 clearTimeout(saveTimer);
 if(savePromise){await savePromise;if(dirty)return save();return;}
 if(!dirty)return;
 if(conflict)throw new Error('草稿存在保存冲突。先下载当前文字，再载入服务器草稿。');
 const snapshot=markdown;
 savePromise=(async()=>{
  try{
   const result=await api(endpoint('draft'),{version:view.draft.version,markdown:snapshot});
   view.draft=result.draft;dirty=markdown!==snapshot;
   $('save-state').textContent=dirty?'正在保存…':'已保存到本机';
  }catch(e){$('save-state').textContent='尚未保存';if(e.message.includes('另一页面'))conflict=true;throw e;}
 })();
 try{await savePromise;}finally{savePromise=null;}
 if(dirty)return save();
}
function showBlock(index) {
 const block = blocks[index];
 if (!block) return;
 multiple = false;
 selectedBlocks.clear();
 selectionAnchor = activeBlock = index;
 editingRange = {start:block.start, end:block.end, ending:block.text.match(/\s*$/)?.[0] || ''};
 chosen = null;
 $('block-text').value = block.text.trimEnd();
 updateSelectionUI();
}
function resetSelection() {
 multiple = false;
 activeBlock = selectionAnchor = -1;
 chosen = editingRange = null;
 selectedBlocks.clear();
 updateSelectionUI();
}
function updateSelectionUI() {
 const count = selectedBlocks.size;
 const hasBlock = mode === 'paper' && activeBlock >= 0;
 const hasMulti = mode === 'paper' && multiple && count > 0;
 const hasQuote = mode === 'markdown' && chosen !== null;
 const selected = hasBlock || hasMulti || hasQuote;

 $('paper').classList.toggle('multi-select', multiple);
 $('multi-select').setAttribute('aria-pressed', String(multiple));
 $('multi-select').textContent = multiple ? '结束多选' : '多选';
 $('selection-toolbar').hidden = mode !== 'paper' || !multiple;
 $('selection-count').textContent = count ? '已选 '+count+' 段' : '选择多个段落，按住 Shift 可连续选择';
 $('selection-clear').hidden = $('selection-discuss').hidden = !count;
 $('multi-tools').hidden = !hasMulti;
 $('block-tools').hidden = !hasBlock;
 $('quote-wrap').hidden = !hasQuote;
 $('agent-tools').hidden = !selected;
 $('clear-block').hidden = !selected;
 $('selection-title').textContent = hasBlock ? '编辑第 '+(activeBlock+1)+' 段' : hasMulti ? '修改 '+count+' 个段落' : hasQuote ? '修改选中文字' : '段落修改';
 $('selection-hint').hidden = selected;
 $('selection-hint').textContent = mode === 'diff' ? '切换到正文或 Markdown 可以继续编辑。'
   : mode === 'markdown' ? '先在 Markdown 中选中文字，再点「修改选中文字」。'
   : '点选正文中的段落即可编辑，或请主写提出修改建议。';
 if (hasQuote) $('selected-quote').textContent = markdown.slice(chosen.start, chosen.end);
 $('selected-blocks').innerHTML = [...selectedBlocks].sort((a,b)=>a-b).filter(i=>blocks[i]).map(i=>
   '<li><span>第 '+(i+1)+' 段</span><p>'+escape(blocks[i].text.trim())+'</p><button data-unselect="'+i+'" aria-label="取消第 '+(i+1)+' 段">×</button></li>'
 ).join('');
 document.querySelectorAll('.report-block').forEach(el=>{
  const i=Number(el.dataset.block);
  el.classList.toggle('selected', multiple ? selectedBlocks.has(i) : i===activeBlock);
  el.querySelector('.block-picker').setAttribute('aria-pressed',String(selectedBlocks.has(i)));
 });
 updateRewriteButton();
}
function toggleBlock(index, extend=false) {
 if (!multiple && activeBlock >= 0) selectedBlocks.add(activeBlock);
 multiple = true;
 if (extend && selectionAnchor >= 0) {
  for(let i=Math.min(selectionAnchor,index);i<=Math.max(selectionAnchor,index);i++) selectedBlocks.add(i);
 } else {
  if (selectedBlocks.has(index)) selectedBlocks.delete(index);
  else selectedBlocks.add(index);
  selectionAnchor=index;
 }
 showMultipleSelection();
}
function showMultipleSelection() {
 activeBlock = -1;
 editingRange = chosen = null;
 updateSelectionUI();
}
function capturePaperSelection() {
 const selection=window.getSelection();
 if(!selection||selection.isCollapsed||!selection.rangeCount||!$('paper').contains(selection.anchorNode)||!$('paper').contains(selection.focusNode))return false;
 const range=selection.getRangeAt(0),indices=[...$('paper').querySelectorAll('[data-block]')].filter(el=>range.intersectsNode(el)).map(el=>Number(el.dataset.block));
 if(indices.length<2)return false;
 multiple=true;selectedBlocks=new Set(indices);selectionAnchor=indices[0];selection.removeAllRanges();showMultipleSelection();return true;
}
function updateRewriteButton() {
 const pending = view?.requests.some(r=>r.kind==='rewrite' && ['queued','running'].includes(r.state));
 const ready = view && view.draft.base_revision === view.task.revision && view.task.status !== 'ignored';
 $('rewrite').disabled = !ready || !$('instruction').value.trim() || pending || requesting || submitting;
 $('rewrite').textContent = pending || requesting ? '等待主写返回建议…' : '请'+harness(view?.writer)+'生成建议';
}
function updateHandoffButton() {
 const canHandoff = view && view.task.can_submit && view.draft.base_revision === view.task.revision;
 $('submit-open').hidden = !canHandoff;
 $('submit-open').disabled = !markdown.trim() || submitting || conflict;
 if ($('submit-dialog').open && (!canHandoff || handoffVersion !== view.draft.version)) {
  $('submit-final').disabled = true;
  $('submit-error').textContent = '作业或草稿已更新。返回草稿，核对当前版本后再交接。';
 }
}
function updateBlock() {
 const b=editingRange;if(!b)return;
 const ending=b.ending;
 const replacement=$('block-text').value+ending;
 const next=markdown.slice(0,b.start)+replacement+markdown.slice(b.end);
 b.end=b.start+replacement.length;chosen=null;changed(next);blocks=splitBlocks(next);
}
function captureSelection() {
 if(mode==='paper'&&multiple){
  if(!selectedBlocks.size)throw new Error('请先选择要一起修改的段落');
  return {ranges:[...selectedBlocks].sort((a,b)=>a-b).map(i=>({start:blocks[i].start,end:blocks[i].start+blocks[i].text.trimEnd().length}))};
 }
 if(mode==='paper' && activeBlock>=0){
  const el=$('block-text'),b=editingRange;if(!b)throw new Error('请重新选择段落');
  chosen={start:b.start+el.selectionStart,end:b.start+(el.selectionEnd>el.selectionStart?el.selectionEnd:el.value.length)};
  if(el.selectionStart===el.selectionEnd)chosen.start=b.start;
 }
 if(!chosen)throw new Error('请先点击一段正文，或在 Markdown 中选择文字');
 return chosen;
}
function issueText(comment) {
 if (typeof comment === 'string') return comment;
 return [comment?.artifact, comment?.location || comment?.file, comment?.comment || comment?.issue || comment?.description,
   comment?.suggestion || comment?.requested_change].filter(Boolean).join(' · ');
}
function showReview() {
 const review = view.review;
 const key = JSON.stringify([view.draft.base_revision,review || null]);
 if (key === reviewKey) return;
 reviewKey = key;
 const matches = review && review.revision === view.draft.base_revision;
 $('review-context').hidden = !matches;
 if (!matches) return;
 const issues = (review.comments || []).map(issueText).filter(Boolean);
 $('review-outcome').textContent = review.approved ? '通过' : '要求修改';
 $('review-outcome').dataset.approved = String(!!review.approved);
 $('review-byline').textContent = [harness(review.reviewer), '第 '+review.revision+' 版', '第 '+(review.round || 1)+' 轮'].join(' · ');
 $('review-summary').textContent = [review.summary, review.packaging_error && '提交件打包失败 · '+review.packaging_error].filter(Boolean).join('\n');
 $('review-issues').hidden = !issues.length;
 $('issue-count').textContent = issues.length+' 条复审意见';
 $('issues').innerHTML = issues.map(text=>'<li><p>'+escape(text)+'</p><button type="button" data-use-issue>引用意见</button></li>').join('');
 $('review-issues').open = !review.approved && issues.length > 0;
}
function updateStatus() {
 const task = view.task;
 $('task-title').textContent = task.title || '报告草稿';
 $('course').textContent = task.course || '';
 $('edition').textContent = '基于第 '+view.draft.base_revision+' 版的草稿';
 $('task-status').textContent = phaseNames[task.status] || task.status || '';
 $('return-task').href = '/#'+new URLSearchParams({view:'task',id:taskID});
 const stale = view.draft.base_revision !== task.revision;
 $('stale').hidden = !stale;
 $('reload').hidden = !task.can_submit;
 $('stale-message').textContent = task.can_submit ? '当前产物已更新至第 '+task.revision+' 版。'
   : '后台正在处理第 '+task.revision+' 版。此处仍可编辑原草稿。';
 updateHandoffButton();
 const handoff = view.requests.find(r=>r.kind==='submit' && (r.task_status || r.state==='error'));
 const tracking = handoff && (handoff.state==='error' || !['awaiting','needs_student'].includes(handoff.task_status) || stale);
 $('handoff-status').hidden = !tracking;
 if (tracking) {
  const revision = handoff.target_revision || handoff.task_revision;
  const progress = handoff.state==='error' ? '交接失败' : (phaseNames[handoff.task_status] || '等待主写');
  $('handoff-status').textContent = [(handoff.state==='error' ? '草稿交接' : '草稿已交接')+(revision ? ' · 第 '+revision+' 版' : ''), progress,
    handoff.error || handoff.task_error].filter(Boolean).join(' · ');
 }
 showReview();
 showSuggestions();
}
function suggestionHTML(r) {
 const status = rewriteStatus(r);
 const current = r.base_revision === view.draft.base_revision;
 const live = current && view.draft.base_revision === view.task.revision && view.task.status !== 'ignored';
 let body = '<article class="suggestion" data-request="'+escape(r.id)+'"><h3>'+escape(status.title)+'</h3>';
 const context = [r.writer && harness(r.writer), r.base_revision && '第 '+r.base_revision+' 版草稿'].filter(Boolean);
 if (context.length) body += '<div class="micro">'+escape(context.join(' · '))+'</div>';
 body += '<p>'+escape(r.instruction)+'</p><p class="micro" data-progress>'+escape(status.detail)+'</p>';
 if (r.state === 'ready') {
  const segments = r.segments || [r];
  body += segments.map((s,i)=>
   (segments.length > 1 ? '<div class="segment-label">片段 '+(i+1)+' / '+segments.length+'</div>' : '')+
   '<details><summary>原文</summary><pre>'+escape(s.selected)+'</pre></details><pre>'+escape(s.replacement)+'</pre>'
  ).join('')+'<p class="micro">'+escape(r.summary)+'</p>';
  body += '<div class="buttons"><button data-decide="dismiss" data-id="'+escape(r.id)+'">保留原文</button>'+
   (current ? '<button class="accent" data-decide="accept" data-id="'+escape(r.id)+'">'+(segments.length > 1 ? '采用全部修改' : '采用建议')+'</button>' : '')+'</div>';
 }
 if (r.state==='error' && live) body += '<button data-retry="'+escape(r.id)+'">重新尝试</button>';
 return body+'</article>';
}
function showSuggestions() {
 const requests = view.requests.filter(r=>r.kind==='rewrite');
 $('suggestion-section').hidden = !requests.length;
 const key = JSON.stringify([view.draft.base_revision,view.task.revision,view.task.status,requests]);
 if (suggestionsKey !== key) {
  const historyOpen = $('suggestions').querySelector('.suggestion-history')?.open;
  suggestionsKey = key;
  const recent = [], history = [];
  for (const r of requests) {
   const active = r.base_revision === view.draft.base_revision && ['queued','running','ready','error'].includes(r.state);
   (active ? recent : history).push(suggestionHTML(r));
  }
  $('suggestions').innerHTML = recent.join('') + (history.length ? '<details class="suggestion-history"'+(historyOpen?' open':'')+'><summary>历史建议 · '+history.length+'</summary>'+history.join('')+'</details>' : '');
 } else {
  for (const r of requests.filter(r=>['running','queued'].includes(r.state))) {
   const el = $('suggestions').querySelector('[data-request="'+CSS.escape(r.id)+'"] [data-progress]');
   if (el) el.textContent = rewriteStatus(r).detail;
  }
 }
 updateRewriteButton();
}
function elapsed(since) {
 const seconds=Math.max(0,Math.floor((Date.now()-Date.parse(since))/1000));
 if(!Number.isFinite(seconds))return '';
 return seconds<60?seconds+' 秒':Math.floor(seconds/60)+' 分 '+seconds%60+' 秒';
}
function rewriteStatus(r) {
 const titles={queued:'已排队，等待 Agent',running:'Agent 正在修改所选内容',ready:'修改建议已就绪',applied:'已采用到草稿',dismissed:'已保留原文',error:'这次修改未完成'};
 let title=titles[r.state]||r.state,detail=r.notice||'';
 if(r.state==='running')detail=(r.stage||'正在修改文字')+' · 已运行 '+elapsed(r.started_at||r.created_at)+(r.attempt>1?' · 第 '+r.attempt+' 次尝试':'');
 if(r.state==='queued') {
  if(r.retry_at){title='暂时未完成，等待自动重试';detail='预计 '+new Date(r.retry_at).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'})+' 后重试（最多连续 3 次）';}
  else detail=detail||'等待现有作业处理结束 · 已等待 '+elapsed(r.retry_requested_at||r.created_at);
 }
 if(r.state==='ready')detail='查看原文和建议，采用后才会写入草稿。';
 if(r.state==='error')detail=r.error||'草稿和修改要求已保留，可重新尝试。';
 return {title,detail};
}
async function retryRewrite(id) {
 try{await api(endpoint('retry'),{id});await poll();toast('已重新排队，原来的修改要求已保留。');}catch(e){error(e.message);}
}
async function loadTask(id) {
 if (dirty) await save();
 const result = await api('/api/task/'+encodeURIComponent(id));
 taskID = id;
 view = result;
 submitID = rewriteID = handoffVersion = null;
 reviewKey = suggestionsKey = '';
 markdown = result.draft.markdown;
 dirty = conflict = false;
 $('markdown').value = markdown;
 $('submit-note').value = $('instruction').value = '';
 $('save-state').textContent = '已保存到本机';
 $('editor').hidden = $('export-menu').hidden = false;
 $('task-picker').hidden = true;
 const params = new URLSearchParams(location.hash.slice(1));
 params.delete('token');
 params.set('task',id);
 history.replaceState(null,'','#'+params);
 setMode('paper');
 updateStatus();
 await render();
}
function setMode(next) {
 mode = next;
 document.querySelectorAll('[data-mode]').forEach(el=>{
  const selected = el.dataset.mode === mode;
  el.setAttribute('aria-selected',String(selected));
  el.tabIndex = selected ? 0 : -1;
 });
 $('paper').hidden = $('paper-tools').hidden = mode !== 'paper';
 $('markdown-pane').hidden = mode !== 'markdown';
 $('diff-pane').hidden = mode !== 'diff';
 if (mode==='markdown') {
  $('markdown').value = markdown;
  $('select-source').disabled = true;
 } else if (mode==='diff') renderDiff();
 resetSelection();
}
async function poll() {
 if (!taskID || pollBusy || savePromise || applying) return;
 pollBusy = true;
 const task = taskID, draftVersion = view.draft.version;
 try {
  const result = await api(endpoint());
  if (task!==taskID || applying || savePromise || draftVersion!==view.draft.version) return;
  if (result.draft.version !== draftVersion && !dirty && !submitting) {
   view = result;
   markdown = result.draft.markdown;
   $('markdown').value = markdown;
   resetSelection();
   await render();
  } else {
   view = {...result,draft:view.draft};
  }
  updateStatus();
  if (pollError && $('error').textContent === pollError) error('');
  pollError = '';
 } catch(e) {
  pollError = e.message;
  error(e.message);
 } finally { pollBusy = false; }
}
function focusInstruction() {
 $('instruction').scrollIntoView({block:'nearest'});
 $('instruction').focus({preventScroll:true});
}
$('paper').addEventListener('click',e=>{
 if (e.target.closest('a') && !multiple) return;
 if (capturePaperSelection()) return;
 const el=e.target.closest('[data-block]');
 if (!el) return;
 if (multiple || e.shiftKey || e.ctrlKey || e.metaKey) {
  e.preventDefault();
  toggleBlock(Number(el.dataset.block),e.shiftKey);
 } else {
  showBlock(Number(el.dataset.block));
  if (matchMedia('(max-width:650px)').matches) $('block-text').focus();
 }
});
$('paper').addEventListener('keydown',e=>{
 if ((e.key==='Enter'||e.key===' ') && e.target.matches('[data-block]')) {
  e.preventDefault();
  const index = Number(e.target.dataset.block);
  if (multiple || e.shiftKey || e.ctrlKey || e.metaKey) toggleBlock(index,e.shiftKey);
  else { showBlock(index); $('block-text').focus(); }
 }
});
$('multi-select').onclick = ()=>{
 if (multiple) { resetSelection(); return; }
 multiple=true;
 if (activeBlock>=0) selectedBlocks.add(activeBlock);
 showMultipleSelection();
};
$('clear-block').onclick = resetSelection;
$('selection-clear').onclick = ()=>{selectedBlocks.clear();selectionAnchor=-1;showMultipleSelection();};
$('selection-discuss').onclick = focusInstruction;
$('selected-blocks').onclick = e=>{
 const button=e.target.closest('[data-unselect]');
 if (button) {selectedBlocks.delete(Number(button.dataset.unselect));showMultipleSelection();}
};
$('outline').addEventListener('click',e=>{
 const el=e.target.closest('[data-jump]');
 if (el) {
  $('outline-menu').open=false;
  const block=$('paper').querySelector('[data-block="'+el.dataset.jump+'"]');
  block?.scrollIntoView({block:'center'});
  block?.focus({preventScroll:true});
 }
});
$('issues').onclick = e=>{
 const button = e.target.closest('[data-use-issue]');
 if (!button) return;
 if (mode === 'diff') setMode('paper');
 const field = $('instruction');
 const issue = button.closest('li').querySelector('p').textContent;
 if (!field.value.split('\n').includes(issue)) {
  if ((field.value+'\n'+issue).trim().length > field.maxLength) {error('修改要求过长，请先缩短已有文字。');return;}
  field.value = [field.value,issue].filter(Boolean).join('\n');
 }
 updateRewriteButton();
 if (!$('agent-tools').hidden) focusInstruction();
 else {
  $('selection-hint').textContent = mode === 'markdown' ? '已引用意见。请选中文字，再点「修改选中文字」。' : '已引用意见。现在点选需要修改的段落。';
  (mode==='markdown' ? $('markdown') : $('paper').querySelector('.report-block'))?.focus();
 }
};
$('block-text').addEventListener('input',updateBlock);
$('markdown').addEventListener('input',e=>{resetSelection();changed(e.target.value);});
$('markdown').addEventListener('select',()=>{
 const el=$('markdown');
 $('select-source').disabled = !el.value.slice(el.selectionStart,el.selectionEnd).trim();
});
$('select-source').onclick = ()=>{
 const source = $('markdown');
 if (source.selectionEnd <= source.selectionStart) return;
 chosen = {start:source.selectionStart, end:source.selectionEnd};
 updateSelectionUI();
 focusInstruction();
};
$('instruction').addEventListener('input',updateRewriteButton);
document.querySelectorAll('[data-mode]').forEach(el=>el.onclick=()=>setMode(el.dataset.mode));
document.querySelector('.tabs').onkeydown = event=>{
 const tabs=[...document.querySelectorAll('[data-mode]')];
 const i=tabs.indexOf(document.activeElement);
 const target={ArrowLeft:(i+tabs.length-1)%tabs.length,ArrowRight:(i+1)%tabs.length,Home:0,End:tabs.length-1}[event.key];
 if (target===undefined || i===-1) return;
 event.preventDefault();tabs[target].focus();setMode(tabs[target].dataset.mode);
};
document.addEventListener('click',e=>{
 for (const menu of document.querySelectorAll('.menu[open]')) {
  if (!menu.contains(e.target) || e.target.closest('button')) menu.open=false;
 }
});
document.addEventListener('keydown',e=>{
 if (e.key!=='Escape') return;
 const menu = document.querySelector('.menu[open]');
 if (menu) {menu.open=false;menu.querySelector('summary').focus();}
});
$('task-select').onchange=e=>{if(e.target.value)loadTask(e.target.value).catch(err=>error(err.message));};
$('return-task').onclick=async e=>{
 if (!dirty) return;
 e.preventDefault();
 try {await save();location.assign($('return-task').href);} catch(err){error(err.message);}
};
$('download').onclick=()=>{
 const a=document.createElement('a');
 a.href=URL.createObjectURL(new Blob([markdown],{type:'text/markdown;charset=utf-8'}));
 a.download='report.md';a.click();setTimeout(()=>URL.revokeObjectURL(a.href),1000);
};
$('rewrite').onclick=async()=>{
 if (requesting || $('rewrite').disabled) return;
 requesting=true;
 updateRewriteButton();
 const instruction=$('instruction').value;
 try {
  const selection=captureSelection();
  await save();
  const input={version:view.draft.version,instruction,...selection};
  const signature=JSON.stringify(input);
  if (rewriteID?.signature!==signature) rewriteID={signature,id:requestID()};
  await api(endpoint('rewrite'),{...input,id:rewriteID.id});
  rewriteID=null;
  if ($('instruction').value===instruction) $('instruction').value='';
  await poll();
  toast('要求已交给主写。建议会显示在「主写建议」。');
 } catch(e) {error(e.message);}
 finally {requesting=false;updateRewriteButton();}
};
let applying = false;
$('suggestions').onclick = async e=>{
 const retry=e.target.closest('[data-retry]');
 if (retry) {retry.disabled=true;await retryRewrite(retry.dataset.retry);retry.disabled=false;return;}
 const button=e.target.closest('[data-decide]');
 if (!button || applying) return;
 applying = true;
 button.disabled = true;
 // Keep a local keystroke from being replaced by the result of a pending decision.
 $('markdown').readOnly = $('block-text').readOnly = true;
 try {
  await save();
  const result=await api(endpoint('decide'),{id:button.dataset.id,action:button.dataset.decide});
  view=result;
  markdown=result.draft.markdown;
  $('markdown').value=markdown;
  resetSelection();await render();updateStatus();
  toast(button.dataset.decide==='accept'?'已采用建议并保存草稿':'已保留原文');
 } catch(err) {error(err.message);}
 finally {applying=false;button.disabled=false;$('markdown').readOnly=$('block-text').readOnly=false;}
};
$('reload').onclick = async()=>{
 const button=$('reload');
 button.disabled=true;
 $('markdown').readOnly=$('block-text').readOnly=true;
 applying=true;
 try {
  await save();
  await api(endpoint('reload'),{version:view.draft.version});
  await loadTask(taskID);
  toast('旧草稿已备份，已载入新版正文。');
 } catch(e) {error(e.message);}
 finally {applying=false;button.disabled=false;$('markdown').readOnly=$('block-text').readOnly=false;}
};
$('preview-html').onclick=async()=>{try{await save();location.assign(endpoint('report.html'));}catch(e){error(e.message);}};
async function downloadSaved(action,filename) {
 try {
  await save();
  const a=document.createElement('a');
  a.href=endpoint(action)+(action==='report.html'?'?download=1':'');a.download=filename;a.click();
 } catch(e) {error(e.message);}
}
$('download-html').onclick=()=>downloadSaved('report.html','report.html');
$('download-source').onclick=()=>downloadSaved('source.zip','report-source.zip');
$('submit-open').onclick=async()=>{
 try {
  await save();
  const files=view.artifacts||[];
  $('related-artifacts').innerHTML=files.map(f=>'<li>'+escape(f)+'</li>').join('');
  $('artifact-list').hidden=!files.length;
  handoffVersion = view.draft.version;
  $('handoff-plan').textContent='按当前设置，主写 '+harness(view.writer)+' 将按当前正文更新第 '+(Number(view.task.revision)+1)+' 版相关产物，再交给 '+harness(view.reviewer)+' 独立复审。';
  $('submit-error').textContent='';
  $('submit-final').disabled=false;
  $('submit-dialog').showModal();
 } catch(e) {error(e.message);}
};
$('submit-cancel').onclick=()=>$('submit-dialog').close();
$('submit-final').onclick=async()=>{
 if (submitting) return;
 submitting=true;
 $('submit-final').disabled=true;
 try {
  await save();
  const input = {version:handoffVersion,instruction:$('submit-note').value};
  const signature = JSON.stringify(input);
  if (submitID?.signature!==signature) submitID={signature,id:requestID()};
  const receipt=await api(endpoint('submit'),{id:submitID.id,...input});
  submitID=null;$('submit-dialog').close();
  if (receipt.target_revision) {
   view.task.can_submit=false;
   view.task.status='revision_ready';
   updateStatus();
  }
  await poll();toast('草稿已交给主写，等待更新产物。');
 } catch(e) {$('submit-error').textContent=e.message;}
 finally {submitting=false;$('submit-final').disabled=false;updateStatus();}
};
window.addEventListener('beforeunload',e=>{if(dirty){e.preventDefault();e.returnValue='';}});
window.avatarDesktop?.onState?.(()=>poll());
window.addEventListener('focus',()=>poll());
document.addEventListener('visibilitychange',()=>{if(!document.hidden)poll();});
(async()=>{
 try {
  const params=new URLSearchParams(location.hash.slice(1)), token=params.get('token');
  if (token) {
   const res=await fetch('/session',{method:'POST',headers:{'X-Editor-Token':token}});
   if (!res.ok) throw new Error(await res.text());
   params.delete('token');
   history.replaceState(null,'',location.pathname+(params.size?'#'+params:''));
  }
  const id=params.get('task');
  if (id) await loadTask(id);
  else {
   const tasks=await api('/api/tasks');
   $('task-title').textContent=tasks.length?'选择报告草稿':'还没有作业';
   $('save-state').textContent='';
   $('task-picker').hidden=!tasks.length;
   $('task-select').innerHTML='<option value="">请选择作业</option>'+tasks.map(t=>'<option value="'+escape(t.id)+'">'+escape(t.title)+'</option>').join('');
  }
  setInterval(poll,2500);
 } catch(e) {
  error(e.message);$('task-title').textContent='无法载入报告';$('save-state').textContent='';
 }
})();
