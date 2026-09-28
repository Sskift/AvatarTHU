'use strict';
const $ = id => document.getElementById(id);
let editingRange = null;
let taskID = '', view, markdown = '', blocks = [], activeBlock = -1, chosen = null, mode = 'paper';
let dirty = false, saveTimer, renderTimer, rendering = 0, savePromise = null, conflict = false, submitting = false;
let submitID = '', rewriteID = '', pollBusy = false, lastReady = '';
const phaseNames = {awaiting:'待你审阅',needs_student:'需要修改',revision_ready:'已排队',queued:'已排队',working:'更新产物中',rewriting:'按复审意见修改中',reviewing:'独立复审中',delivery_pending:'发布产物中',failed:'处理遇到问题',submitted:'已提交学堂',approval_invalid:'需要重新确认',closed_remote:'课程作业已关闭',submission_unknown:'提交结果待确认'};
const escape = text => String(text ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const requestID = () => crypto.randomUUID().replaceAll('-','');
function error(message) { $('error').textContent = message; $('error').hidden = !message; }
function toast(message) { $('toast').textContent=message; $('toast').hidden=false; setTimeout(()=>{$('toast').hidden=true;},3500); }
async function api(path, body) {
 const response=await fetch(path,{method:body===undefined?'GET':'POST',headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
 const text=await response.text(); let data; try{data=JSON.parse(text);}catch{data={error:text};}
 if(!response.ok) throw new Error(data.error || '后台暂时不可用'); return data;
}
const endpoint = action => '/api/task/'+taskID+(action?'/'+action:'');
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
 $('paper').innerHTML=blocks.length?rendered.blocks.map((html,i)=>'<section class="report-block'+(i===activeBlock?' selected':'')+'" tabindex="0" data-block="'+i+'">'+html+'</section>').join(''):'<p class="muted">这版还没有 Markdown 报告。切换到 Markdown 粘贴正文，或等待下一版产出可编辑源文件。</p>';
 const headings=blocks.map((b,i)=>({i,match:b.text.match(/^#{1,3}\s+(.+)/)})).filter(h=>h.match);
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
 markdown=next;dirty=true;$('save-state').textContent='正在保存…';error('');
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
 const b=blocks[index];if(!b)return;
 activeBlock=index;editingRange={start:b.start,end:b.end,ending:b.text.match(/\s*$/)?.[0]||''};chosen=null;$('block-text').value=b.text.trimEnd();$('block-tools').hidden=false;$('quote-wrap').hidden=true;
 $('selection-hint').textContent='直接修改下面的文字，正文预览与草稿自动同步。';$('rewrite').disabled=false;
 document.querySelectorAll('.report-block').forEach(el=>el.classList.toggle('selected',Number(el.dataset.block)===index));
}
function updateBlock() {
 const b=editingRange;if(!b)return;
 const ending=b.ending;
 const replacement=$('block-text').value+ending;
 const next=markdown.slice(0,b.start)+replacement+markdown.slice(b.end);
 b.end=b.start+replacement.length;chosen=null;changed(next);blocks=splitBlocks(next);
}
function captureSelection() {
 if(mode==='markdown') {
  const el=$('markdown'); if(el.selectionEnd<=el.selectionStart)throw new Error('请先在 Markdown 中选中要修改的文字');
  chosen={start:el.selectionStart,end:el.selectionEnd};
 }else if(activeBlock>=0){
  const el=$('block-text'),b=editingRange;if(!b)throw new Error('请重新选择段落');
  chosen={start:b.start+el.selectionStart,end:b.start+(el.selectionEnd>el.selectionStart?el.selectionEnd:el.value.length)};
  if(el.selectionStart===el.selectionEnd)chosen.start=b.start;
 }
 if(!chosen)throw new Error('请先点击一段正文，或在 Markdown 中选择文字');
 $('selected-quote').textContent=markdown.slice(chosen.start,chosen.end);$('quote-wrap').hidden=false;
 return chosen;
}
function updateStatus() {
 const st=view.task;$('task-title').textContent=st.title||'报告';$('course').textContent=st.course||'';
 $('edition').textContent='基于第 '+view.draft.base_revision+' 版 · 可编辑稿';$('task-status').textContent=phaseNames[st.status]||st.status;
 $('pairing').textContent='主写 '+view.writer+' · 复审 '+view.reviewer;
 const stale=view.draft.base_revision!==st.revision;$('stale').hidden=!stale;$('reload').disabled=!st.can_submit;$('stale-message').textContent=st.can_submit?'已有新版产物。当前草稿仍然保留。':'新版正在处理中，当前草稿继续保存。';
 $('submit-open').disabled=!st.can_submit||stale||submitting||!markdown.trim();
 $('cloud-link').hidden=!st.document_url;
 if(st.document_url&&/^https:\/\//.test(st.document_url))$('cloud-link').href=st.document_url;
 const handoff=view.requests.find(r=>r.kind==='submit');
 $('handoff-status').hidden=!handoff;
 if(handoff){
  const phase=handoff.task_status;let title='定稿已接收，等待后台处理',detail='你可以继续编辑；后续修改留在草稿中，本次同步使用提交时的正文。';
  if(handoff.state==='error'){title='定稿未接收';detail=handoff.error;}
  else if(phase==='awaiting'||phase==='needs_student'){title=phase==='awaiting'?'新版产物已完成，等待你确认':'新版已生成，需要处理复审意见';detail=stale?'请载入新版查看最新正文，PDF、代码包和历次复审在同一份审阅文档中。':'已载入最新正文；可继续修改，或打开审阅文档查看产物和复审意见。';}
  else if(phase){title=phaseNames[phase]||'同步处理中';if(handoff.task_error)detail=handoff.task_error;}
  $('handoff-status').innerHTML='<strong>'+escape(title)+'</strong>'+escape(detail);
 }
 showSuggestions();
}
function showSuggestions() {
 const requests=view.requests.filter(r=>r.kind==='rewrite');
 $('suggestions').innerHTML=requests.slice(0,4).map(r=>{
  const titles={queued:'已排队，等待 Agent',running:'Agent 正在修改这一段',ready:'建议已就绪',applied:'已采用到草稿',dismissed:'已保留原文',error:'这次修改未完成'};
  let body='<div class="suggestion"><h3>'+escape(titles[r.state]||r.state)+'</h3><p>'+escape(r.instruction)+'</p>';
  if(r.state==='ready')body+='<details open><summary>原文</summary><pre>'+escape(r.selected)+'</pre></details><pre>'+escape(r.replacement)+'</pre><p class="micro">'+escape(r.summary)+'</p><div class="buttons"><button data-decide="dismiss" data-id="'+r.id+'">保留原文</button><button class="primary" data-decide="accept" data-id="'+r.id+'">采用建议</button></div>';
  if(r.error)body+='<p class="error">'+escape(r.error)+'</p>';
  return body+'</div>';
 }).join('');
 const ready=requests.find(r=>r.state==='ready');if(ready&&ready.id!==lastReady){lastReady=ready.id;document.querySelector('.inspector').scrollTop=0;}
 const running=requests.some(r=>r.state==='queued'||r.state==='running');
 $('rewrite').disabled=running||(!chosen&&activeBlock<0);$('rewrite').textContent=running?'等待 Agent 返回建议…':'让 Agent 改这一段';
}
async function loadTask(id) {
 if(taskID&&dirty)await save();
 const result=await api('/api/task/'+id);taskID=id;view=result;markdown=result.draft.markdown;dirty=false;conflict=false;activeBlock=-1;chosen=null;submitID='';rewriteID='';
 $('markdown').value=markdown;$('block-tools').hidden=true;$('quote-wrap').hidden=true;$('rewrite').disabled=true;
 $('save-state').textContent='已保存到本机';$('task-select').value=id;
 const params=new URLSearchParams(location.hash.slice(1));params.set('task',id);history.replaceState(null,'','#'+params.toString());
 updateStatus();await render();
}
function setMode(next) {
 mode=next;document.querySelectorAll('[data-mode]').forEach(el=>el.setAttribute('aria-selected',String(el.dataset.mode===mode)));
 $('paper').hidden=mode!=='paper';$('markdown-pane').hidden=mode!=='markdown';$('diff-pane').hidden=mode!=='diff';
 $('view-hint').textContent=mode==='paper'?'点击一段文字即可修改':mode==='markdown'?'支持精确选中文字调用 Agent':'红色为原文 · 绿色为修改';
 if(mode==='markdown')$('markdown').value=markdown;if(mode==='diff')renderDiff();
}
async function poll() {
 if(!taskID||pollBusy||savePromise)return;pollBusy=true;const polledTask=taskID;
 try{
  const result=await api(endpoint());if(polledTask!==taskID)return;
  if(result.draft.version!==view.draft.version&&!dirty){view=result;markdown=result.draft.markdown;activeBlock=-1;chosen=null;$('block-tools').hidden=true;$('markdown').value=markdown;await render();}
  else {view.task=result.task;view.requests=result.requests;view.writer=result.writer;view.reviewer=result.reviewer;view.artifacts=result.artifacts;}
  updateStatus();
 }catch(e){error(e.message);}finally{pollBusy=false;}
}
$('paper').addEventListener('click',e=>{if(e.target.closest('a'))return;const el=e.target.closest('[data-block]');if(el)showBlock(Number(el.dataset.block));});
$('paper').addEventListener('keydown',e=>{if(e.key==='Enter'&&e.target.matches('[data-block]'))showBlock(Number(e.target.dataset.block));});
$('outline').addEventListener('click',e=>{const el=e.target.closest('[data-jump]');if(el){setMode('paper');document.querySelector('[data-block="'+el.dataset.jump+'"]').scrollIntoView({behavior:'smooth',block:'center'});}});
$('block-text').addEventListener('input',updateBlock);
$('markdown').addEventListener('input',e=>{activeBlock=-1;chosen=null;changed(e.target.value);});
$('select-source').onclick=()=>{try{captureSelection();$('rewrite').disabled=false;$('instruction').focus();}catch(e){error(e.message);}};
document.querySelectorAll('[data-mode]').forEach(el=>el.onclick=()=>setMode(el.dataset.mode));
document.querySelectorAll('[data-prompt]').forEach(el=>el.onclick=()=>{$('instruction').value=el.dataset.prompt;});
$('task-select').onchange=e=>loadTask(e.target.value).catch(err=>error(err.message));
$('download').onclick=()=>{const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([markdown],{type:'text/markdown;charset=utf-8'}));a.download='report.md';a.click();setTimeout(()=>URL.revokeObjectURL(a.href),1000);};
$('rewrite').onclick=async()=>{
 try{const selection=captureSelection();await save();rewriteID=rewriteID||requestID();$('rewrite').disabled=true;
  await api(endpoint('rewrite'),{id:rewriteID,version:view.draft.version,instruction:$('instruction').value,...selection});rewriteID='';await poll();toast('局部修改已排队，建议完成后由你决定是否采用。');
 }catch(e){error(e.message);$('rewrite').disabled=false;}
};
$('suggestions').onclick=async e=>{
 const b=e.target.closest('[data-decide]');if(!b)return;
 try{await save();b.disabled=true;const result=await api(endpoint('decide'),{id:b.dataset.id,action:b.dataset.decide});view=result;markdown=result.draft.markdown;activeBlock=-1;chosen=null;$('block-tools').hidden=true;$('markdown').value=markdown;await render();updateStatus();toast(b.dataset.decide==='accept'?'已采用建议并保存草稿':'已保留原文');}catch(err){error(err.message);b.disabled=false;}
};
$('reload').onclick=async()=>{
 try{await save();await api(endpoint('reload'),{version:view.draft.version});await loadTask(taskID);toast('旧草稿已备份，已载入新版正文。');}catch(e){error(e.message);}
};
$('preview-html').onclick=async()=>{try{await save();location.href=endpoint('report.html');}catch(e){error(e.message);}};
$('download-html').onclick=async()=>{try{await save();const a=document.createElement('a');a.href=endpoint('report.html')+'?download=1';a.download='report.html';a.click();}catch(e){error(e.message);}};
$('download-source').onclick=async()=>{try{await save();const a=document.createElement('a');a.href=endpoint('source.zip');a.download='report-source.zip';a.click();}catch(e){error(e.message);}};
$('submit-open').onclick=async()=>{
 try{await save();$('related-artifacts').textContent=view.artifacts.join(' · ');$('submit-error').textContent='';$('submit-dialog').showModal();}catch(e){error(e.message);}
};
$('submit-final').onclick=async()=>{
 if(submitting)return;submitting=true;$('submit-final').disabled=true;
 try{await save();submitID=submitID||requestID();await api(endpoint('submit'),{id:submitID,version:view.draft.version,instruction:$('submit-note').value});submitID='';$('submit-dialog').close();await poll();toast('定稿已交给后台，开始同步相关产物。');}
 catch(e){$('submit-error').textContent=e.message;}finally{submitting=false;$('submit-final').disabled=false;updateStatus();}
};
window.addEventListener('beforeunload',e=>{if(dirty){e.preventDefault();e.returnValue='';}});
(async()=>{
 try{
  const params=new URLSearchParams(location.hash.slice(1)),token=params.get('token');
  if(token){const r=await fetch('/session',{method:'POST',headers:{'X-Editor-Token':token}});if(!r.ok)throw new Error(await r.text());}
  const tasks=await api('/api/tasks');$('task-select').innerHTML=tasks.map(t=>'<option value="'+t.id+'">'+escape(t.title)+'</option>').join('');
  if(!tasks.length){$('task-title').textContent='还没有作业';$('save-state').textContent='后台已连接';$('submit-open').disabled=true;return;}
  await loadTask(tasks.some(t=>t.id===params.get('task'))?params.get('task'):tasks[0].id);setInterval(poll,2500);
 }catch(e){error(e.message);$('save-state').textContent='连接未完成';$('submit-open').disabled=true;}
})();
