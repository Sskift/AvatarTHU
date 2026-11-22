'use strict';
const $=id=>document.getElementById(id), esc=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const phaseNames={awaiting:'待你审阅',needs_student:'需要你处理',revision_ready:'修改已排队',queued:'已排队',working:'正在撰写',rewriting:'按意见修改中',reviewing:'独立复审中',delivery_pending:'发布产物中',failed:'处理遇到问题',submitted:'已提交学堂',approval_invalid:'需要重新确认',ignored:'已忽略',closed_remote:'学堂作业已关闭',submission_unknown:'提交结果待确认',submitting:'正在提交学堂',revision_requested:'等待修改意见'};
const kinds={courseware:'课件 / 补充资料',assignment:'作业附件',notice:'公告',artifact:'产物',source:'报告源文件'};
const harness=name=>name==='claude'?'Claude':name==='codex'?'Codex':'待配置';
let overview={courses:[],tasks:[],health:{}}, files=[], listed=[], currentView='overview',currentID='',courseTab='tasks',renderID=0,ready=false,uploadCourse='',fileAction=null,taskAction=null,taskFilter='visible',toastTimer;
let applyingState=null, pendingState=null, stateSequence=0, connectionError='', connectionEpoch=0, settingsDirty=false, routeFailed=false, syncRequest=false, directoryPath='', directoryData=null;
let directoryCourse='', directoryShownKey=null, directoryLoadingTimer;
let taskDetail=null, queueFilter='current', selectedPane='', activityBusy=false;
const chatDrafts=new Map();
const queueFilters=[['current','当前待办'],['delivered','已有产物'],['all','全部作业']];
const directoryCache=new Map();
function error(message){$('error').textContent=message;$('error').hidden=!message;}
function toast(message){clearTimeout(toastTimer);$('toast').textContent=message;$('toast').hidden=false;toastTimer=setTimeout(()=>{$('toast').hidden=true;},4000);}
async function api(path='',body){const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),20000);try{const multipart=body instanceof FormData,res=await fetch('/api/workspace'+path,{method:body===undefined?'GET':'POST',cache:'no-store',headers:body===undefined||multipart?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:multipart?body:JSON.stringify(body),signal:controller.signal});const raw=await res.text();let data;try{data=JSON.parse(raw);}catch{data={error:raw};}if(!res.ok)throw new Error(res.status===401?'请从菜单栏重新打开 AvatarTHU。':data.error||'请求未完成');return data;}catch(e){if(e.name==='AbortError'||e instanceof TypeError)throw new Error('暂时连不上本机后台。后台恢复后会自动重新连接。');throw e;}finally{clearTimeout(timer);}}
const accessToken=new URLSearchParams(location.hash.slice(1)).get('token');
const href=(view,id)=>'#'+new URLSearchParams({... (accessToken?{token:accessToken}:{}),view,...(id?{id}: {})});
const fileQuery=f=>({course:f.course_id,path:f.path,...(f.member?{member:f.member}:{})});
const fileURL=(f,inline=false)=>'/api/workspace/file?'+new URLSearchParams({...fileQuery(f),...(inline?{inline:'1'}:{})});
const date=value=>{if(!value)return '暂无';const d=new Date(value);return Number.isNaN(d.getTime())?String(value):d.toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false});};
const size=n=>n<1024?n+' B':n<1048576?(n/1024).toFixed(1)+' KB':n<1073741824?(n/1048576).toFixed(1)+' MB':(n/1073741824).toFixed(2)+' GB';
const empty=(title,detail='')=>'<div class="empty">'+esc(title)+(detail?'<p>'+esc(detail)+'</p>':'')+'</div>';
const badge=(text,tone='')=>'<span class="badge '+tone+'">'+esc(text)+'</span>';
const needsReview=t=>['awaiting','approval_invalid'].includes(t.status);
const needsHelp=t=>['needs_student','failed','submission_unknown','revision_requested'].includes(t.status);
const active=t=>['working','rewriting','reviewing','queued','revision_ready','delivery_pending'].includes(t.status);
function taskTone(t){
 if(needsHelp(t))return 'bad';
 if(needsReview(t))return 'warn';
 return t.status==='submitted'?'good':'';
}
function taskRow(t){
 const meta=[t.course,'第 '+(t.revision||1)+' 版'];
 if(t.deadline)meta.push('截止 '+date(t.deadline));
 return '<a class="task-row" href="'+esc(href('task',t.id))+'"><div class="task-text"><h3>'+esc(t.title)+'</h3><small>'+meta.map(esc).join(' · ')+'</small></div>'+
  badge(phaseNames[t.status]||t.status,taskTone(t))+'<span class="row-arrow" aria-hidden="true">›</span></a>';
}
function courseCard(c){const tasks=overview.tasks.filter(t=>t.course_id===c.id),pending=tasks.filter(needsReview).length;return '<a class="course-card" href="'+href('course',c.id)+'"><div class="course-icon">▦</div><h3>'+esc(c.name)+'</h3><p>'+esc(c.semester)+' 学期</p><footer><span>'+c.tasks+' 份作业'+(pending?' · '+pending+' 份待审阅':'')+'</span><span>进入课程 →</span></footer></a>';}
function healthHTML(h){const learn=h.learn||{},lark=h.lark||{},daemon=h.daemon||{};const rows=[{title:'网络学堂',state:learn.state==='valid'?'登录有效':learn.state==='expired'?'需要登录':learn.state==='unavailable'?'暂时无法连接':learn.state==='stale'?'保活记录待更新':learn.state==='error'?'检查未完成':'等待检查',tone:learn.state==='valid'?'good':learn.state==='expired'?'bad':'warn',detail:learn.message||'每 10 分钟保活'},{title:'飞书',state:!lark.enabled?'未启用':lark.actions==='ready'&&lark.messages==='ready'?'已连接':'连接待检查',tone:!lark.enabled?'':lark.actions==='ready'&&lark.messages==='ready'?'good':'warn',detail:!lark.enabled?'可直接在本机审阅':'卡片回调 '+(lark.actions||'未知')+' · 消息 '+(lark.messages||'未知')},...(h.cli||[]).map(c=>({title:harness(c.tool),state:{ready:'可用',unavailable:'不可用',failed:'调用失败',running:'运行中',stale:'记录待更新',unknown:'尚未检查'}[c.state]||c.state,tone:c.state==='ready'?'good':c.state==='failed'||c.state==='unavailable'?'bad':'',detail:c.reason||(c.required?'当前使用':'未用于当前分工')}))];return '<div class="health-grid">'+rows.map(r=>'<div class="health-item"><strong>'+esc(r.title)+'</strong>'+badge(r.state,r.tone)+'<p>'+esc(r.detail)+'</p></div>').join('')+'</div><div class="meta-line"><span>主写 '+harness(h.writer)+' / 复审 '+harness(h.reviewer)+'</span><span>每 '+esc(h.poll_hours||12)+' 小时同步</span><span>下次同步 '+esc(date(h.next_scan))+'</span><span>最近保活 '+esc(date(learn.last_success))+'</span></div>'+(daemon.problem?'<p class="error">'+esc(daemon.problem)+'</p>':'');}
function updateHealth(h){overview.health=h;const daemon=h.daemon||{},connected=daemon.state==='running'&&!daemon.problem;$('connection').textContent=connected?'本机后台运行中':'后台状态待检查';$('connection-dot').className='dot'+(connected?' ready':'');const scan=h.scan||{},busy=['queued','running'].includes(scan.state);$('scan').disabled=busy||syncRequest;$('scan').textContent=scan.state==='running'?'正在同步…':busy||syncRequest?'正在连接学堂…':'同步课程';if($('health'))$('health').innerHTML=healthHTML(h);if($('scan-result'))$('scan-result').textContent=scan.state==='error'?'最近同步失败：'+(scan.error||'未知错误'):scan.state==='completed'?'最近同步完成于 '+date(scan.completed_at):scan.state==='queued'?'正在连接网络学堂，同步独立进行。':scan.state==='running'?'正在同步课程和作业，完成后自动更新。':'';}
function isCurrentTask(t){return needsReview(t)||needsHelp(t)||active(t)||t.status==='submitting';}
function queueRows(tasks,order){
 const value=t=>order==='deadline'?(Date.parse(t.deadline)||Infinity):-(Date.parse(t.updated_at)||0);
 return tasks.slice().sort((a,b)=>value(a)-value(b)||String(a.title||'').localeCompare(String(b.title||''),'zh-CN')).map(taskRow).join('');
}
function queueSection(title,tasks,order){
 if(!tasks.length)return '';
 return '<section class="queue-section"><div class="section-heading"><h2>'+title+'</h2><span>'+tasks.length+' 份作业</span></div><div class="task-list">'+queueRows(tasks,order)+'</div></section>';
}
function renderOverview(){
 const tasks=overview.tasks, current=tasks.filter(isCurrentTask), delivered=tasks.filter(t=>t.has_delivery??!!t.artifact_count);
 const counts={current:current.length,delivered:delivered.length,all:tasks.length};
 const filters=queueFilters.map(([id,label])=>{
  const link=id==='delivered'?href('artifacts'):href('overview')+(id==='all'?'&filter=all':'');
  return '<a href="'+esc(link)+'" '+(queueFilter===id?'aria-current="page"':'')+'>'+label+' <span>'+counts[id]+'</span></a>';
 });
 let rows;
 if(queueFilter==='current'){
  rows=queueSection('等你处理',current.filter(t=>needsReview(t)||needsHelp(t)),'deadline')+
   queueSection('Agent 处理中',current.filter(active),'updated')+queueSection('学堂提交中',current.filter(t=>t.status==='submitting'),'updated');
 }else if(queueFilter==='delivered'){
  rows=queueSection('已交付的作业',delivered,'updated');
 }else{
  rows=queueSection('所有作业',tasks,'updated');
 }
 $('content').innerHTML='<nav class="queue-filters" aria-label="筛选作业">'+filters.join('')+'</nav><div id="queue-results">'+
  (rows||empty(queueFilter==='current'?'当前没有待办作业':queueFilter==='delivered'?'还没有交付产物':'还没有作业'))+'</div>';
}
function filterBar(includeKind=true){return '<div class="filters"><input id="file-search" type="search" placeholder="搜索文件名或路径" aria-label="搜索文件名或路径">'+(currentView==='course'?'':'<select id="course-filter" aria-label="筛选课程"><option value="">所有课程</option>'+overview.courses.map(c=>'<option value="'+c.id+'">'+esc(c.name)+'</option>').join('')+'</select>')+(includeKind?'<select id="kind-filter" aria-label="筛选类型"><option value="">全部类型</option>'+Object.entries(kinds).map(([k,v])=>'<option value="'+k+'">'+v+'</option>').join('')+'</select>':'')+'</div><div id="file-results"></div>';}
function fileRow(f,compact=false){
 const i=listed.push(f)-1,ext=f.name.includes('.')?f.name.split('.').pop().slice(0,5).toUpperCase():'FILE';
 return '<div class="file-row" data-editable="'+!!f.editable+'"><span class="file-icon">'+esc(ext)+'</span><div><button class="file-name" data-preview="'+i+'">'+esc(f.title||f.name)+'</button>'+(compact?'':'<span class="file-path">'+esc((f.course?f.course+' / ':'')+f.path)+'</span>')+'</div><span class="file-size">'+size(f.size)+'</span><span class="file-date">'+esc(date(f.modified_at))+'</span><div class="actions"><button data-preview="'+i+'">预览</button><a class="button" href="'+esc(fileURL(f))+'" download>下载</a>'+(f.editable?'<button data-rename="'+i+'">改名</button><button data-remove="'+i+'">移除</button>':'')+'</div></div>';
}
function fileRows(rows){return rows.length?'<div class="file-list">'+rows.map(f=>fileRow(f)).join('')+'</div>':empty('没有匹配的文件','更换筛选条件，或同步课程获取最新资料。');}
const directoryHref=(course='',path='')=>'#'+new URLSearchParams({...(accessToken?{token:accessToken}:{}),view:'files',...(course?{id:course}:{}),...(path?{path}: {})});
const folderIcon='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></svg>';
function folderRow(entry){
 const title=entry.title||entry.name,detail=title!==entry.name?entry.name+'/':entry.description||'文件夹';
 return '<a class="directory-row" href="'+esc(entry.href||directoryHref(directoryCourse,entry.path))+'" aria-label="打开目录 '+esc(title)+'"><span class="folder-icon">'+folderIcon+'</span><span class="directory-name"><strong>'+esc(title)+'</strong><small>'+esc(entry.detail||detail)+'</small></span><span class="directory-arrow" aria-hidden="true">›</span></a>';
}
function directoryShell(){return '<nav id="directory-crumbs" class="breadcrumbs" aria-label="文件目录"></nav><div class="filters directory-tools"><input id="directory-search" type="search" placeholder="搜索当前目录" aria-label="搜索当前目录"><span id="directory-loading" class="directory-loading" role="status"></span></div><div id="directory-results"></div>';}
function directoryHTML(id,html){const el=$(id);if(el.directoryHTML===html)return;el.innerHTML=html;el.directoryHTML=html;}
function directoryBusy(busy){
 clearTimeout(directoryLoadingTimer);
 if(!$('directory-results'))return;
 $('directory-results').setAttribute('aria-busy',String(busy));$('directory-loading').textContent='';
 if(busy)directoryLoadingTimer=setTimeout(()=>{if($('directory-loading'))$('directory-loading').textContent='正在打开目录…';},200);
}
function renderDirectory(){
 const q=($('directory-search')?.value||'').trim().toLocaleLowerCase();listed=[];
 const crumbs=[{title:'文件',url:directoryHref()},...(directoryData?.breadcrumbs||[]).map(c=>({title:c.title,url:directoryHref(directoryCourse,c.path)}))];
 directoryHTML('directory-crumbs',crumbs.map((c,i)=>i===crumbs.length-1?'<span aria-current="page">'+esc(c.title)+'</span>':'<a href="'+esc(c.url)+'">'+esc(c.title)+'</a><span class="crumb-separator" aria-hidden="true">/</span>').join(''));
 const entries=directoryCourse?(directoryData?.entries||[]):overview.courses.map(c=>({kind:'directory',name:c.id,title:c.name,detail:c.semester+' 学期',href:directoryHref(c.id)}));
 const shown=entries.filter(f=>!q||[f.name,f.title,f.description,f.detail].filter(Boolean).join(' ').toLocaleLowerCase().includes(q));
 directoryHTML('directory-results',shown.length?'<div class="file-count">'+shown.length+' 项</div><div class="file-list directory-list">'+shown.map(f=>f.kind==='directory'?folderRow(f):fileRow(f,true)).join('')+'</div>':empty(q?'当前目录没有匹配的项目':'这个目录还没有文件'));
}
async function loadDirectory(route,navigation=false){
 const course=currentID,path=directoryPath,key=JSON.stringify([course,path]);
 const current=()=>route===renderID&&currentView==='files'&&course===currentID&&path===directoryPath;
 const display=data=>{
  if(directoryShownKey!==key)$('directory-search').value='';
  directoryCourse=course;directoryData=data;directoryShownKey=key;renderDirectory();
 };
 if(!course){display(null);directoryBusy(false);return;}
 if(navigation&&directoryCache.has(key))display(directoryCache.get(key));
 directoryBusy(true);
 try{
  const data=await api('/directory?'+new URLSearchParams({course,path}));
  if(!current())return;
  directoryCache.delete(key);directoryCache.set(key,data);
  if(directoryCache.size>32)directoryCache.delete(directoryCache.keys().next().value);
  display(data);
 }catch(e){if(current())throw e;}
 finally{if(current())directoryBusy(false);}
}
function renderFiles(){const q=($('file-search')?.value||'').toLocaleLowerCase(),course=$('course-filter')?.value||'',kind=$('kind-filter')?.value||'';let rows=files.filter(f=>(!q||(f.name+' '+(f.title||'')+' '+f.path+' '+f.course).toLocaleLowerCase().includes(q))&&(!course||f.course_id===course)&&(!kind||f.kind===kind));if(currentView==='course'&&courseTab!=='all'){const allowed={materials:['courseware','assignment'],notices:['notice'],outputs:['artifact','source']}[courseTab];if(allowed)rows=rows.filter(f=>allowed.includes(f.kind));}listed=[];$('file-results').innerHTML='<div class="file-count">'+rows.length+' 个文件 · '+size(rows.reduce((n,f)=>n+f.size,0))+'</div>'+fileRows(rows);}
function renderCourseTasks(){
 const tasks=overview.tasks.filter(t=>t.course_id===currentID),shown=tasks.filter(t=>taskFilter==='all'||(taskFilter==='ignored'?t.status==='ignored':t.status!=='ignored'));
 $('task-results').innerHTML=shown.map(taskRow).join('')||empty(taskFilter==='ignored'?'没有已忽略的作业':'没有未忽略的作业','可以切换筛选查看其他作业。');
}
function renderCourse(c){$('content').innerHTML='<div class="actions"><span class="badge">'+esc(c.semester)+' 学期</span><button data-upload="'+c.id+'">＋ 上传补充资料</button></div><div class="tabs" role="tablist" aria-label="课程内容">'+[['tasks','作业'],['materials','课件与附件'],['notices','公告'],['outputs','产物']].map(([id,title])=>'<button role="tab" aria-selected="'+(courseTab===id)+'" class="'+(courseTab===id?'active':'')+'" data-tab="'+id+'">'+title+'</button>').join('')+'</div>'+(courseTab==='tasks'?'<div class="filters"><select id="task-filter" aria-label="筛选作业">'+[['visible','未忽略'],['ignored','已忽略'],['all','全部作业']].map(([value,label])=>'<option value="'+value+'" '+(taskFilter===value?'selected':'')+'>'+label+'</option>').join('')+'</select><span class="muted">'+overview.tasks.filter(t=>t.course_id===c.id&&t.status==='ignored').length+' 份已忽略</span></div><div class="task-list" id="task-results"></div>':filterBar(false));if(courseTab==='tasks')renderCourseTasks();else renderFiles();}
function textFrom(value){return typeof value==='string'?value:JSON.stringify(value,null,2);}
function safeLink(url){try{const u=new URL(url);return ['https:','http:'].includes(u.protocol)?esc(u.href):'';}catch{return '';}}
function taskControls(t){
 if(t.status==='ignored')return '<button data-task-action="restore" data-task-id="'+esc(t.id)+'">恢复作业</button>';
 if(['submitted','submitting','submission_unknown'].includes(t.status))return '';
 return '<button data-task-action="ignore" data-task-id="'+esc(t.id)+'">忽略作业</button>';
}
function editTask(id,action){
 const task=overview.tasks.find(t=>t.id===id);if(!task)return;
 taskAction={id,action};const ignoring=action==='ignore';
 $('task-dialog-title').textContent=(ignoring?'忽略作业：':'恢复作业：')+task.title;
 $('task-dialog-description').textContent=ignoring?'忽略后停止自动处理和提醒，历史文件与复审记录保留。可在课程作业的“已忽略”筛选中恢复。':'恢复后重新显示在作业列表，并继续原有处理流程；材料有变化时会重新核对。';
 $('ignore-reason-label').hidden=!ignoring;$('ignore-reason').value='';
 $('task-confirm').textContent=ignoring?'忽略作业':'恢复作业';$('task-error').hidden=true;$('task-dialog').showModal();
}
function disclosure(key,title,body){
 return '<details class="disclosure" data-section="'+esc(key)+'"><summary>'+esc(title)+'</summary><div class="disclosure-body">'+body+'</div></details>';
}
function currentDelivery(t){return (t.revisions||[]).find(r=>Number(r.revision)===Number(t.revision||1));}
function reviewText(value){return typeof value==='string'?value:textFrom(value);}
function reviewBody(review,files,canQuote=false){
 const comments=(review.comments||[]).map((c,index)=>{
  const cite=canQuote?'<button type="button" class="text-button cite-feedback" data-quote-comment="'+index+'">引用到对话</button>':'';
  if(typeof c==='string')return '<li>'+esc(c)+cite+'</li>';
  const issue=c.issue||c.comment||c.description,request=c.suggestion||c.requested_change;
  const details=[c.severity,c.location||c.file].filter(Boolean).map(esc).join(' · ');
  const file=files.find(f=>f.name===c.artifact);
  const reference=file?'<button class="text-button" data-preview="'+(listed.push(file)-1)+'">'+esc(file.name)+'</button>':esc(c.artifact||'');
  return '<li>'+(details||reference?'<small>'+[reference,details].filter(Boolean).join(' · ')+'</small>':'')+
   esc([issue,request].filter(Boolean).join('。')||(!reference&&!details?reviewText(c):''))+cite+'</li>';
 }).join('');
 const checks=(review.checks||[]).map(v=>'<li>'+esc(reviewText(v))+'</li>').join('');
 const limitations=(review.limitations||[]).map(v=>'<li>'+esc(reviewText(v))+'</li>').join('');
 return (review.packaging_error?'<p class="error">提交件打包失败 · '+esc(review.packaging_error)+'</p>':'')+
  (review.summary?'<p class="review-summary">'+esc(review.summary)+'</p>':'')+
  (comments?'<ul class="review-comments">'+comments+'</ul>':'')+
  (checks||limitations?disclosure('checks-'+review.revision+'-'+review.round,'检查记录', (checks?'<ul>'+checks+'</ul>':'')+(limitations?'<h4>检查范围与限制</h4><ul>'+limitations+'</ul>':'')):'');
}
function reviewHeading(r){
 return harness(r.reviewer)+' · 第 '+(r.round||1)+' 轮 · '+(r.approved?'通过':'要求修改');
}
function revisionNotes(r){
 const notes=r?.revision_notes||[];
 if(!notes.length)return '';
 const states={addressed:'已处理',partial:'部分处理',unresolved:'待处理'};
 return disclosure('changes-'+r.revision,'本版修改记录 · '+notes.length+' 项','<ul>'+notes.map(n=>
  '<li><strong>'+esc(n.request)+'</strong> · '+esc(states[n.status]||n.status)+'<p>'+esc(n.detail)+'</p>'+
  ((n.files||[]).length?'<small>'+n.files.map(esc).join('、')+'</small>':'')+'</li>').join('')+'</ul>');
}
function reviewPanel(t,revision,files,canQuote=false){
 const rounds=(t.reviews||[]).filter(r=>Number(r.revision)===Number(revision));
 const latest=rounds.at(-1);
 if(!latest)return '<p class="quiet">本版尚无已完成的复审。</p>';
 return '<div class="review-verdict">'+badge(latest.approved?'通过':'要求修改',latest.approved?'good':'bad')+
  '<span>'+esc(harness(latest.reviewer))+' · 第 '+esc(latest.round||1)+' 轮</span></div>'+reviewBody(latest,files,canQuote)+
  rounds.slice(0,-1).reverse().map(r=>disclosure('round-'+revision+'-'+r.round,reviewHeading(r),reviewBody(r,[]))).join('');
}
function deliveryFiles(r){
 if(!r)return [];
 const files=[...(r.files||[])];
 if(r.submission&&!files.some(f=>f.path===r.submission.path))files.push(r.submission);
 return files;
}
function workflowPanel(t){
 const w=t.workflow||{}, writer=w.writer?harness(w.writer):'',reviewer=w.reviewer?harness(w.reviewer):'';

 let next='';
 if(t.status==='awaiting')next=overview.health.lark?.enabled?'核对产物后，可在本版飞书卡片确认提交。':'核对产物后，请自行在网络学堂提交定稿。';
 if(t.status==='revision_ready')next='主写将在第 '+(w.target_revision||Number(t.revision||1)+1)+' 版处理你的修改意见。';
 if(t.status==='submission_unknown')next='请在网络学堂核对本次提交结果。';
 if(t.status==='approval_invalid')next='请核对本版产物与提交凭据。';
 const inputs=w.student_inputs||[], blockers=(w.blockers||[]).filter(item=>!inputs.includes(item));
 const list=(title,values)=>values.length?'<h3>'+title+'</h3><ul>'+values.map(v=>'<li>'+esc(v)+'</li>').join('')+'</ul>':'';
 return '<div class="desk-status"><div class="stage-line"><h2>第 '+esc(w.target_revision||t.revision||1)+' 版执行记录</h2>'+(w.round?' <span>第 '+esc(w.round)+' 轮</span>':'')+'</div>'+(next?'<p>'+esc(next)+'</p>':'')+
  (t.status==='ignored'?'<p>'+esc(t.ignored_reason||'已暂停自动处理')+(t.ignored_at?' · '+esc(date(t.ignored_at)):'')+'</p>':'')+
  (needsHelp(t)&&t.error?'<p class="error">'+esc(t.error)+'</p>':'')+
  list('需要你补充',inputs)+
  (inputs.length&&t.course_id?'<a class="text-link" href="'+esc(href('course',t.course_id))+'">去课程添加补充资料 ›</a>':'')+
  list('待解决',blockers)+
  (t.status==='failed'?'<a class="text-link" href="'+esc(href('settings'))+'">检查并恢复任务 ›</a>':'')+
  (writer||reviewer?'<dl class="agent-pair">'+(writer?'<div><dt>主写</dt><dd>'+esc(writer)+'</dd></div>':'')+
   (reviewer?'<div><dt>独立复审</dt><dd>'+esc(reviewer)+'</dd></div>':'')+'</dl>':'')+
  (w.feedback&&!t.conversation?.messages?.length?disclosure('feedback','本次修改意见','<p class="description">'+esc(w.feedback)+'</p>'):'')+'</div>';
}
function taskHistory(t){
 const past=(t.revisions||[]).filter(r=>Number(r.revision)!==Number(t.revision||1));
 // Reviews can survive removal of an old artifact directory.
 const versions=new Set([...past.map(r=>Number(r.revision)),...(t.reviews||[]).map(r=>Number(r.revision)).filter(n=>n&&n!==Number(t.revision||1))]);
 return Array.from(versions).sort((a,b)=>b-a).map(n=>{
  const delivery=past.find(r=>Number(r.revision)===n),files=deliveryFiles(delivery);
  const body=(files.length?'<div class="file-list historical-files">'+files.map(f=>fileRow(f,true)).join('')+'</div>':'')+
   revisionNotes(delivery)+reviewPanel(t,n,delivery?.files||[]);
  return disclosure('revision-'+n,'第 '+n+' 版',body);
 }).join('');
}
function latestDelivery(t){
 return (t.revisions||[]).reduce((latest,r)=>!latest||r.revision>latest.revision?r:latest,null);
}
function setPane(name){
 const changed=selectedPane!==name;
 selectedPane=name;
 for(const button of document.querySelectorAll('[data-pane]')){
  const selected=button.dataset.pane===name;
  button.setAttribute('aria-selected',String(selected));button.tabIndex=selected?0:-1;
  $('pane-'+button.dataset.pane).hidden=!selected;
 }
 if(changed&&name==='workflow'&&$('activity-log'))$('activity-log').scrollTop=$('activity-log').scrollHeight;
 $('review-desk').dataset.pane=name;
 if(name==='delivery')renderComparison();
}
function renderTask(t){
 const continuing=taskDetail?.id===t.id&&!!$('conversation-form');
 const expanded=new Set(continuing?[...document.querySelectorAll('#content details[open]')].map(el=>el.dataset.section):[]);
 if(!continuing){
  closeComparison();selectedPane='';
  $('content').replaceChildren($('task-workspace').content.cloneNode(true));
  $('chat-text').value=draftFor(t).text;
 }
 taskDetail=t;listed=[];
 const r=latestDelivery(t),history=taskHistory(t),doc=safeLink(t.document_url);
 const reportLink='<a class="button" href="/editor#task='+encodeURIComponent(t.id)+'">'+(t.has_report_source&&t.can_submit?'编辑报告正文':'打开报告草稿')+'</a>';
 const meta=[t.course,t.deadline?'截止 '+date(t.deadline):''].filter(Boolean).map(esc).join(' · ');
 $('task-meta').innerHTML='<span>'+meta+'</span>'+badge(phaseNames[t.status]||t.status,taskTone(t));
 $('task-links').innerHTML=t.has_report_source&&t.can_submit?reportLink:'';
 const tools=(doc?'<a class="button" href="'+doc+'" target="_blank" rel="noopener noreferrer">飞书审阅文档 ↗</a>':'')+
  (t.course_id?'<a class="button" href="'+esc(href('course',t.course_id))+'">查看课程</a>':'')+
  (t.can_submit&&t.has_report_source?'':reportLink)+taskControls(t);
 $('task-secondary').innerHTML=(history?disclosure('history','历史版本',history):'')+
  disclosure('tools','作业管理','<div class="actions">'+tools+'</div>');
 $('delivery-notes').innerHTML=revisionNotes(r);
 $('pipeline-status').innerHTML=workflowPanel(t);
 const reviewFiles=currentDelivery(t)?.files||[];
 $('pane-review').innerHTML='<div class="section-heading"><h2>第 '+esc(t.revision||1)+' 版独立复审</h2></div>'+
  reviewPanel(t,t.revision||1,reviewFiles,true);
 for(const el of document.querySelectorAll('#content details[data-section]'))el.open=expanded.has(el.dataset.section);
 setPane(selectedPane||(t.status==='failed'||active(t)?'workflow':'delivery'));
 renderActivity(t.timeline||[]);
 renderConversation(t);
}
const activityStates={active:'进行中',done:'已完成',waiting:'等待中',failed:'遇到问题'};
const roles={writer:'主写',reviewer:'独立复审',system:'工作台'};
function activityRow(row){
 const byline=[row.at?date(row.at):'',row.round?'第 '+row.round+' 轮':'',
  [roles[row.role],row.harness?harness(row.harness):''].filter(Boolean).join(' · ')].filter(Boolean);
 const label=row.state==='done'?'':activityStates[row.state]||'';
 return '<li class="event event-'+esc(row.state)+'"><div class="event-meta">'+esc(byline.join(' · '))+'</div><div class="event-title"><strong>'+
  esc(row.title)+'</strong><span>'+esc(label)+'</span></div>'+
  (row.detail?'<p>'+esc(row.detail)+'</p>':'')+
  (row.files?.length?'<small>'+row.files.map(esc).join(' · ')+'</small>':'')+'</li>';
}
function renderActivity(rows){
 const log=$('activity-log'),revision=taskDetail.workflow?.target_revision||taskDetail.revision||1;
 const past=new Map(),current=[];
 for(const row of rows){
  if(!row.revision||row.revision===revision){current.push(row);continue;}
  if(!past.has(row.revision))past.set(row.revision,[]);
  past.get(row.revision).push(row);
 }
 const groups=[...past].map(([rev,items])=>'<li class="past-activity"><details data-log-version="'+esc(rev)+'"><summary>第 '+
  esc(rev)+' 版记录 · '+items.length+' 项</summary><ol>'+items.map(activityRow).join('')+'</ol></details></li>');
 const html=groups.join('')+current.map(activityRow).join('');
 if(log.eventHTML!==html){
  const follow=log.scrollHeight-log.clientHeight-log.scrollTop<32;
  const opened=[...log.querySelectorAll('details[open]')].map(d=>d.dataset.logVersion);
  log.innerHTML=html;log.eventHTML=html;
  for(const d of log.querySelectorAll('details'))d.open=opened.includes(d.dataset.logVersion);
  if(follow)log.scrollTop=log.scrollHeight;
 }
 $('activity-hint').textContent=current.length?'':'后台尚未记录本版的执行步骤。';
}
function draftFor(t){
 if(!chatDrafts.has(t.id)){
  let saved;
  try{saved=JSON.parse(sessionStorage.getItem('task-chat-'+t.id)||'null');}catch{}
  const draft={text:'',kind:'ask',files:[],revision:t.revision||1,fileRevision:null,id:''};
  if(saved&&typeof saved.text==='string'&&Array.isArray(saved.files))Object.assign(draft,saved);
  chatDrafts.set(t.id,{...draft,sending:false,error:''});
 }
 return chatDrafts.get(t.id);
}
function rememberChat(t,d){
 const {text,files,id,revision,fileRevision,kind}=d;
 try{
  const value=text||files.length?JSON.stringify({text,files,id,revision,fileRevision,kind}):'';
  if(d.stored===value)return;
  if(value)sessionStorage.setItem('task-chat-'+t.id,value);
  else sessionStorage.removeItem('task-chat-'+t.id);
  d.stored=value;
 }catch{}
}
function pinnedDelivery(t){
 const revision=t.conversation?.file_revision;
 return revision===undefined?latestDelivery(t):(t.revisions||[]).find(r=>r.revision===revision);
}
function latestComment(t,index){
 const review=(t.reviews||[]).filter(r=>r.revision===(t.revision||1)).at(-1);
 const c=review?.comments?.[index];if(!c)return;
 if(typeof c==='string')return {text:c};
 const artifact=c.artifact||'',position=[artifact,c.location||c.file].filter(Boolean).join(' · ');
 const text=[c.issue||c.comment||c.description,c.suggestion||c.requested_change].filter(Boolean).join(' ');
 return {artifact,text:[position,text].filter(Boolean).join(' — ')};
}
function citeComment(index){
 const t=taskDetail,d=draftFor(t),c=latestComment(t,index);
 if(!c||d.sending)return;
 if(d.revision!==t.revision&&(d.text||d.files.length)){d.error='请先核对草稿所属版本，再引用本轮意见。';renderComposer(t);return;}
 d.kind=t.conversation?.can_send?'change':'ask';
 d.revision=t.revision;d.id='';d.error='';d.text=[d.text.trimEnd(),c.text].filter(Boolean).join('\n');
 const r=pinnedDelivery(t);
 if(r?.revision===t.revision&&r.files?.some(f=>f.name===c.artifact)&&!d.files.includes(c.artifact)){d.files.push(c.artifact);d.fileRevision=r.revision;}
 $('chat-text').value=d.text;renderComposer(t);$('chat-text').focus();
}
function restoreFailedMessage(id){
 const t=taskDetail,d=draftFor(t),m=t.conversation.messages.find(item=>item.id===id&&item.role==='user'&&item.kind==='ask'&&item.state==='error');
 if(!m||d.sending)return;
 if(d.text||d.files.length){d.error='请先发送或清空当前草稿，再载入这条消息。';renderComposer(t);return;}
 Object.assign(d,{id:'',kind:m.kind,text:m.text,revision:m.revision,files:[...(m.files||[])],fileRevision:m.file_revision,error:''});
 $('chat-text').value=d.text;renderComposer(t);$('chat-text').focus();
}
function renderConversation(t){
 const log=$('conversation-log'),messages=t.conversation?.messages||[];
 const d=draftFor(t);
 if(d.id&&messages.some(m=>m.role==='user'&&m.id===d.id)&&!d.sending)clearSent(t,d);
 const states={queued:'等待主写',running:'正在处理',answered:'已回复',error:'处理失败',waiting:'等待作业恢复',stopped:'作业已停止'};
 const html=messages.length?messages.map(m=>{
  const mine=m.role==='user';
  const version=mine&&m.kind==='change'?(m.target_revision||m.revision):m.revision;
  const meta=[mine?'你':('主写'+(m.writer?' · '+harness(m.writer):'')),m.created_at?date(m.created_at):''].filter(Boolean);
  const label=[mine?(m.kind==='ask'?'提问':'修改要求'):'',version?'第 '+version+' 版':'',
   mine?(states[m.state]||''):''].filter(Boolean);
  return '<article class="chat-message '+(mine?'from-owner':'from-writer')+'"><header>'+meta.map(esc).join(' · ')+'</header><p>'+
   esc(m.text)+'</p>'+(m.files?.length?'<small>关联第 '+esc(m.file_revision||m.revision)+' 版 · '+m.files.map(esc).join('、')+'</small>':'')+
   '<footer>'+label.map(esc).join(' · ')+'</footer>'+
   (m.error?'<div class="message-error">'+esc(m.error)+'</div>':'')+
   (mine&&m.kind==='ask'&&m.state==='error'?'<button class="text-button" data-retry-chat="'+esc(m.id)+'">编辑并重试</button>':'')+'</article>';
 }).join(''):'<p class="chat-empty">还没有对话。可以询问解题思路，或告诉主写要改哪份文件。</p>';
 if(log.lastHTML!==html){
  const bottom=log.scrollHeight-log.scrollTop-log.clientHeight<30;
  log.innerHTML=html;log.lastHTML=html;
  if(bottom)log.scrollTop=log.scrollHeight;
 }
 renderComposer(t);
}
function renderComposer(t){
 const d=draftFor(t),c=t.conversation||{},r=pinnedDelivery(t);
 if(!d.text&&!d.files.length&&!d.sending){
  d.revision=t.revision||1;
  if(!c.can_send&&c.can_ask)d.kind='ask';
 }
 const stale=d.revision!==t.revision||(d.files.length&&d.fileRevision!==r?.revision);
 const allowed=d.kind==='ask'?c.can_ask:c.can_send;
 for(const b of document.querySelectorAll('[data-kind]')){
  b.disabled=d.sending||!(b.dataset.kind==='ask'?c.can_ask:c.can_send);
  b.setAttribute('aria-pressed',String(b.dataset.kind===d.kind));
 }
 const input=$('chat-text');
 input.readOnly=d.sending||(!c.can_ask&&!c.can_send&&!d.text);
 input.placeholder=d.kind==='ask'?'询问思路、当前进展或文件内容…':'描述希望主写如何调整作业…';
 const files=r?.files||[];
 const choices=files.map(f=>'<label><input type="checkbox" value="'+esc(f.name)+'"> '+esc(f.name)+'</label>').join('');
 const field=$('chat-files');
 if(field.choices!==choices){field.innerHTML=choices;field.choices=choices;}
 field.disabled=!!(stale||d.sending||!allowed);
 for(const checkbox of field.querySelectorAll('input'))checkbox.checked=d.files.includes(checkbox.value);
 $('chat-context').hidden=!files.length;
 $('context-count').textContent=d.files.length?'已关联 '+d.files.length+' 个文件':'关联文件'+(r?' · 第 '+r.revision+' 版':'');
 $('chat-scope').textContent=stale?'草稿来自第 '+d.revision+' 版'+(d.files.length?'，已关联 '+d.files.join('、'):'')+'。请核对当前版本。':'';
 $('chat-scope').hidden=!stale;
 $('chat-rebase').hidden=!stale||d.sending;
 $('chat-rebase').textContent=d.files.length?'保留文字，重新选择本版文件':'按当前版本继续';
 $('chat-action-note').textContent=!allowed?'当前状态无法发送这种消息。':d.kind==='ask'?'主写将回答你的问题。':active(t)?'在下一次主写时处理。':'交给主写更新产物并独立复审。';
 $('chat-send').disabled=!!(!allowed||stale||d.sending||!d.text.trim());
 $('chat-send').textContent=d.sending?'发送中…':'发送';
 $('chat-error').hidden=!d.error;$('chat-error').textContent=d.error;
 rememberChat(t,d);
}
function clearSent(t,d){
 d.text='';d.files=[];d.fileRevision=null;d.id='';d.error='';d.revision=t.revision||1;
 rememberChat(t,d);
 if(taskDetail?.id===t.id&&$('chat-text'))$('chat-text').value='';
}
async function sendConversation(){
 const t=taskDetail,d=draftFor(t);
 if($('chat-send').disabled)return;
 if(!d.id)d.id=Array.from(crypto.getRandomValues(new Uint8Array(16)),v=>v.toString(16).padStart(2,'0')).join('');
 const request={id:d.id,revision:d.revision,kind:d.kind,instruction:d.text.trim(),files:[...d.files]};
 if(d.files.length)request.file_revision=d.fileRevision;
 d.sending=true;d.error='';renderComposer(t);
 try{
  const updated=await api('/task/'+encodeURIComponent(t.id)+'/conversation',request);
  clearSent(updated,d);
  if(currentView==='task'&&currentID===t.id){++renderID;renderTask(updated);$('conversation-log').scrollTop=$('conversation-log').scrollHeight;}
 }catch(e){d.error=e.message;}
 finally{
  d.sending=false;
  if(currentView==='task'&&currentID===t.id&&taskDetail){renderConversation(taskDetail);pollActivity();}
 }
}
async function pollActivity(){
 if(document.hidden||currentView!=='task'||!taskDetail||activityBusy)return;
 const t=taskDetail,route=renderID;
 const current=()=>taskDetail===t&&currentView==='task'&&route===renderID;
 activityBusy=true;
 try{
  const path='/task/'+encodeURIComponent(t.id),fresh=await api(path+'/activity');
  if(!current())return;
  if(fresh.revision!==t.revision||fresh.status!==t.status||fresh.updated_at!==t.updated_at){
   const updated=await api(path);
   if(current())renderTask(updated);
  }else{
   t.timeline=fresh.timeline||[];t.conversation=fresh.conversation;
   renderActivity(t.timeline);renderConversation(t);
  }
 }catch(e){if(current())$('activity-hint').textContent=e.message;}
 finally{activityBusy=false;}
}

function renderSettings(h){settingsDirty=false;$('content').innerHTML='<div class="settings"><form id="settings-form" class="panel"><h2>作业处理方式</h2><div class="form-row">'+[['writer','主写'],['reviewer','复审']].map(([key,label])=>'<label>'+label+'<select name="'+key+'">'+['claude','codex'].map(c=>'<option value="'+c+'" '+(h[key]===c?'selected':'')+'>'+harness(c)+'</option>').join('')+'</select></label>').join('')+'</div><p class="hint">两者分别选择，可以相同。使用各自 CLI 的默认模型配置，每一阶段独立启动新会话。</p><label>课程同步间隔（小时）<input name="poll_hours" type="number" min="0.016667" max="8760" step="any" value="'+esc(h.poll_hours||12)+'" required></label><p class="hint">默认每 12 小时同步课程，网络学堂每 10 分钟保活。更改应用于后续任务，已开始的任务沿用启动时的分工。</p><button class="primary" type="submit">保存设置</button><span id="settings-result" class="muted" role="status"></span></form></div><div class="section-heading"><h2>运行状态</h2></div><section class="panel" id="health"></section><p class="muted" id="scan-result"></p>';}
async function renderRoute(){
 if(!ready)return;routeFailed=false;const id=++renderID,params=new URLSearchParams(location.hash.slice(1));
 if(params.get('task')&&!params.get('view')){location.replace('/editor'+location.hash);return;}
 currentView=params.get('view')||'overview';currentID=params.get('id')||'';directoryPath=params.get('path')||'';
 if(currentView!=='files')directoryBusy(false);
 const titles={overview:'作业审阅',courses:'我的课程',course:'课程',files:'文件',artifacts:'作业审阅',task:'作业',settings:'设置与状态'};
 if(!titles[currentView])currentView='overview';
 const section=currentView==='course'?'courses':['task','artifacts'].includes(currentView)?'overview':currentView;
 queueFilter=currentView==='artifacts'?'delivered':params.get('filter')==='all'?'all':'current';
 for(const a of document.querySelectorAll('[data-nav]')){
  a.classList.toggle('active',a.dataset.nav===section);
  if(a.dataset.nav===section)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');
 }
 closeComparison();taskDetail=null;$('toast').hidden=true;

 $('back-link').hidden=currentView!=='task';$('back-link').innerHTML='<a href="'+esc(href('overview'))+'">‹ 作业审阅</a>';
 $('scan').hidden=!['overview','artifacts','courses','settings'].includes(currentView);
 $('title').textContent=titles[currentView];error('');
 try{
  if(['overview','artifacts'].includes(currentView))renderOverview();
  else if(currentView==='courses'){
   $('content').innerHTML='<div class="filters"><input type="search" id="course-search" placeholder="搜索课程" aria-label="搜索课程"><select id="semester-filter" aria-label="筛选学期"><option value="">全部学期</option>'+[...new Set(overview.courses.map(c=>c.semester))].map(s=>'<option>'+esc(s)+'</option>').join('')+'</select></div><div class="grid" id="course-results"></div>';renderCourseResults();
  }else if(currentView==='settings')renderSettings(overview.health);
  else if(currentView==='files'){
   if(!$('directory-results')){
    directoryData=null;directoryShownKey=null;$('content').innerHTML=directoryShell();
   }
   await loadDirectory(id,true);
  }else if(currentView==='task'){
   $('content').innerHTML=empty('正在载入作业…');const t=await api('/task/'+encodeURIComponent(currentID));if(id!==renderID)return;$('title').textContent=t.title;renderTask(t);
  }else{
   const c=overview.courses.find(c=>c.id===currentID);if(!c)throw new Error('找不到这门课程，请返回课程列表。');
   $('content').innerHTML=empty('正在载入文件…');const loaded=await api('/files?course='+encodeURIComponent(c.id));if(id!==renderID)return;
   files=loaded;$('title').textContent=c.name;renderCourse(c);
  }
  if(id===renderID)updateHealth(overview.health);
 }catch(e){if(id===renderID){routeFailed=true;error(e.message);if(currentView!=='files')$('content').innerHTML=empty('暂时无法载入','正在自动重试，也可以点击同步课程。');}}
}
function renderCourseResults(){const q=($('course-search')?.value||'').toLocaleLowerCase(),semester=$('semester-filter')?.value||'';const rows=overview.courses.filter(c=>(!q||c.name.toLocaleLowerCase().includes(q))&&(!semester||c.semester===semester));$('course-results').innerHTML=rows.map(courseCard).join('')||empty('没有匹配的课程','可以先同步课程，或更换搜索条件。');}
function disconnected(message){
 connectionEpoch++;connectionError=message;$('connection').textContent='连接中断，正在重试';$('connection-dot').className='dot';error(message);
 if(!ready)$('content').innerHTML=empty('等待连接本机后台','后台恢复后会自动重新连接。');
}
async function updateVisibleState(changed,force){
 const view=currentView,id=currentID;
 if(view==='settings'){
  if(!settingsDirty){const form=$('settings-form');if(form)for(const key of ['writer','reviewer','poll_hours'])form.elements[key].value=overview.health[key];}
  return;
 }
 if(!changed&&!force)return;
 if(['overview','artifacts'].includes(view)){renderOverview();return;}
 if(view==='courses'){renderCourseResults();return;}
 if(view==='files'){await loadDirectory(++renderID);return;}
 if(view==='course'&&courseTab==='tasks'){renderCourseTasks();return;}
 const route=++renderID;
 if(view==='task'){
  const task=await api('/task/'+encodeURIComponent(id));if(route!==renderID||view!==currentView||id!==currentID)return;
  if(JSON.stringify(task)!==JSON.stringify(taskDetail))renderTask(task);
  return;
 }
 if(view==='course'){
  const loaded=await api('/files'+(view==='course'?'?course='+encodeURIComponent(id):''));
  if(route!==renderID||view!==currentView||id!==currentID)return;
  files=loaded;renderFiles();
 }
}
function applyOverview(fresh,force=false){
 pendingState={fresh,force:force||!!pendingState?.force};
 if(applyingState)return applyingState;
 applyingState=(async()=>{
  while(pendingState){
   const {fresh,force}=pendingState;pendingState=null;const epoch=connectionEpoch;
   const first=!ready,changed=JSON.stringify(fresh.tasks)!==JSON.stringify(overview.tasks)||JSON.stringify(fresh.courses)!==JSON.stringify(overview.courses);
   const before=overview.health.scan||{},scan=fresh.health.scan||{},scanned=scan.state==='completed'&&(before.state!=='completed'||before.completed_at!==scan.completed_at);
   overview=fresh;ready=true;updateHealth(fresh.health);
   if(first||routeFailed)await renderRoute();else await updateVisibleState(changed,force||scanned);
   if(epoch===connectionEpoch){updateHealth(overview.health);if(connectionError&&$('error').textContent===connectionError)error('');connectionError='';}
   else if(connectionError)disconnected(connectionError);
   if(scan.state==='error'&&before.state!=='error')error('课程同步未完成：'+(scan.error||'请查看运行状态'));
  }
 })().finally(()=>{applyingState=null;});
 return applyingState;
}
async function desktopState(state,force=false){
 if(!state||state.sequence<stateSequence)return;
 stateSequence=state.sequence;
 if(state.error){pendingState=null;disconnected(state.error);return;}
 await applyOverview(state.overview,force);
}
async function refresh(){
 if(window.avatarDesktop?.refresh)return desktopState(await window.avatarDesktop.refresh(),true);
 return applyOverview(await api(),true);
}
const previews=new WeakMap();
function closeFileSurface(body){
 if(!body)return;
 const session=previews.get(body);
 if(!session)return;
 previews.delete(body);
 session.observer?.disconnect();
 session.render?.cancel();
 session.loading?.destroy().catch(()=>{});
 body.onscroll=null;
}
function showReviewText(body,session,text){
 const pre=document.createElement('pre');
 const marks=[];
 let parent=0;
 for(const [i,line] of text.split(/\r?\n/).entries()){
  if(i)pre.append('\n');
  const mark=questionMarker(line,parent);
  if(!mark){pre.append(line);continue;}
  parent=mark.parent;
  const node=document.createElement('span');
  node.textContent=line;pre.append(node);marks.push({...mark,node});
 }
 body.append(pre);
 session.marks=marks;
 let highlighted;
 session.mark=mark=>{
  highlighted?.classList.remove('text-location');
  highlighted=mark?.node;highlighted?.classList.add('text-location');
 };
 session.jump=mark=>{
  if(mark.node)body.scrollTop+=mark.node.getBoundingClientRect().top-body.getBoundingClientRect().top-10;
  else body.scrollTop=(mark.y||0)*body.scrollHeight;
 };
}
async function showFileSurface(body,file,options={}){
 closeFileSurface(body);
 const position=options.position||{page:1,zoom:'fit',top:0,left:0};
 const session={...options,file,position};
 const current=()=>body.isConnected&&previews.get(body)===session;
 previews.set(body,session);body.textContent='正在载入预览…';body.scrollTop=0;
 try{
  const data=file.text!==undefined?{kind:'text',text:file.text}:await api('/preview?'+new URLSearchParams(fileQuery(file)));
  if(!current())return;
  body.replaceChildren();
  if(data.kind==='pdf'){await previewPDF(body,session,current);return;}
  if(data.kind==='html'){
   const frame=document.createElement('iframe');frame.title=file.name;frame.setAttribute('sandbox','');frame.referrerPolicy='no-referrer';frame.src=fileURL(file,true);body.append(frame);
  }else if(data.kind==='image'){
   const img=document.createElement('img');img.alt=file.name;img.src=fileURL(file,true);body.append(img);
   await img.decode();if(!current())return;
  }else if(data.kind==='text'){
   if(session.side)showReviewText(body,session,data.text);
   else{const pre=document.createElement('pre');pre.textContent=data.text;body.append(pre);}
  }else if(data.kind==='zip'){
   for(const entry of data.entries||[]){
    const row=document.createElement('div');row.className='zip-entry';
    const name=document.createElement(entry.file?'button':'span');name.textContent=entry.name;
    if(entry.file){name.className='text-button';name.onclick=()=>session.openMember?session.openMember(entry.file):previewFile(entry.file,file);}
    const bytes=document.createElement('small');bytes.textContent=size(entry.size);
    row.append(name,bytes);body.append(row);
   }
   if(!body.children.length)body.innerHTML=empty('压缩包内没有文件');
   if(data.truncated)body.insertAdjacentHTML('beforeend','<p class="quiet">显示前 500 项，下载后可查看完整内容。</p>');
  }else{
   body.innerHTML=empty('请下载此文件后打开');
  }
  if(session.side){
   session.canBookmark=data.kind==='text'||data.kind==='image';
   session.jump??=mark=>{body.scrollTop=(mark.y||0)*body.scrollHeight;};
   session.observer=new ResizeObserver(()=>{
    if(body.clientHeight)body.scrollTop=(position.top||0)*body.scrollHeight;
   });
   session.observer.observe(body);
   if(body.clientHeight)body.scrollTop=(position.top||0)*body.scrollHeight;
   body.onscroll=()=>{if(body.clientHeight){position.top=body.scrollTop/body.scrollHeight;session.changed();}};
  }
 }catch(e){if(current())body.textContent='预览未能载入。'+e.message;}
 finally{
  if(current()&&session.side&&!session.pdf){session.marks??=[];updateQuestionNav();}
 }
}
function previewFile(f,parent=null){
 $('preview-title').textContent=f.member||f.title||f.name;
 const back=$('preview-parent');back.hidden=!parent;back.onclick=parent?()=>previewFile(parent):null;
 $('preview-meta').textContent=(f.course?f.course+' · ':'')+size(f.size);
 $('preview-download').href=fileURL(f);
 if(!$('preview').open)$('preview').showModal();
 showFileSurface($('preview-body'),f);
}
function editFile(f,action){fileAction={file:f,action};$('file-dialog-title').textContent=action==='rename'?'重命名补充资料':'移除补充资料';$('file-dialog-description').textContent=action==='rename'?f.name:'将「'+f.name+'」移到本机回收目录，后续作业不再引用它。';$('rename-label').hidden=action!=='rename';$('rename-input').required=action==='rename';$('rename-input').value=f.name;$('file-confirm').textContent=action==='rename'?'保存文件名':'移到回收目录';$('file-error').hidden=true;$('file-dialog').showModal();}
document.addEventListener('click',e=>{
 const el=e.target.closest('button,a');if(!el)return;
 const d=el.dataset;
 if(d.close){$(d.close).close();return;}
 if(d.preview!==undefined){previewFile(listed[Number(d.preview)]);return;}
 if(d.rename!==undefined||d.remove!==undefined){
  editFile(listed[Number(d.rename??d.remove)],d.rename!==undefined?'rename':'remove');return;
 }
 if(d.retryChat){restoreFailedMessage(d.retryChat);return;}
 if(d.quoteComment!==undefined){citeComment(Number(d.quoteComment));return;}
 if(d.pane){setPane(d.pane);return;}
 if(d.layout&&reading){reading.state.layout=d.layout;reading.changed();renderComparison();return;}
 if(d.question){jumpToQuestion(d.question);return;}
 if(el.id==='mark-position'){openReadingMark();return;}
 if(el.id==='remove-mark'){finishReadingMark(true);return;}
 if(el.id==='cite-position'){citeReadingPosition();return;}
 if(el.id==='return-reading'){
  const target=$('question-nav').querySelector('[aria-pressed=true]')||$('tab-delivery');
  target.focus({preventScroll:true});$('pane-delivery').scrollIntoView({block:'start'});return;
 }
 if(d.kind){
  Object.assign(draftFor(taskDetail),{kind:d.kind,id:'',error:''});
  renderComposer(taskDetail);$('chat-text').focus();return;
 }
 if(el.id==='chat-rebase'){
  Object.assign(draftFor(taskDetail),{revision:taskDetail.revision,fileRevision:null,files:[],id:'',error:''});
  renderComposer(taskDetail);return;
 }
 if(d.taskAction){editTask(d.taskId,d.taskAction);return;}
 if(d.upload){uploadCourse=d.upload;$('upload-input').click();return;}
 if(d.tab){courseTab=d.tab;renderCourse(overview.courses.find(c=>c.id===currentID));}
});
document.addEventListener('change',e=>{
 if(e.target.closest('#chat-files')){
  const files=[...$('chat-files').querySelectorAll('input:checked')].map(el=>el.value);
  Object.assign(draftFor(taskDetail),{files,fileRevision:pinnedDelivery(taskDetail)?.revision,id:'',error:''});
  renderComposer(taskDetail);return;
 }
 if(e.target.id==='artifact-choice'||e.target.id==='prompt-choice'){
  selectReadingFile(e.target.id==='prompt-choice'?'prompt':'answer',e.target.value);renderComparison();return;
 }
 if(e.target.id==='task-filter'){taskFilter=e.target.value;renderCourseTasks();}
});
document.addEventListener('input',e=>{
 if(e.target.id==='chat-text'){
  Object.assign(draftFor(taskDetail),{text:e.target.value,id:'',error:''});renderComposer(taskDetail);return;
 }
 if(e.target.id==='directory-search')renderDirectory();
 if(['file-search','course-filter','kind-filter'].includes(e.target.id))renderFiles();
 if(['course-search','semester-filter'].includes(e.target.id))renderCourseResults();
});
document.addEventListener('submit',async e=>{if(e.target.id==='conversation-form'){e.preventDefault();await sendConversation();return;}if(e.target.id!=='settings-form')return;e.preventDefault();const form=e.target,button=form.querySelector('button[type=submit]');button.disabled=true;try{const data=new FormData(form),h=await api('/settings',{writer:data.get('writer'),reviewer:data.get('reviewer'),poll_hours:Number(data.get('poll_hours'))});settingsDirty=[...data].some(([key,value])=>String(form.elements[key].value)!==String(value));updateHealth(h);$('settings-result').textContent='  已保存';toast('设置已保存');}catch(err){error(err.message);}finally{button.disabled=false;}});
$('file-form').addEventListener('submit',async e=>{e.preventDefault();if(!fileAction)return;const {file,action}=fileAction;$('file-confirm').disabled=true;try{const result=await api('/file-action',{course:file.course_id,path:file.path,action,name:$('rename-input').value});$('file-dialog').close();toast(result.message);await renderRoute();}catch(err){$('file-error').textContent=err.message;$('file-error').hidden=false;}finally{$('file-confirm').disabled=false;}});
$('task-form').addEventListener('submit',async e=>{
 e.preventDefault();if(!taskAction)return;const {id,action}=taskAction;$('task-confirm').disabled=true;
 try{await api('/task/'+encodeURIComponent(id)+'/'+action,action==='ignore'?{reason:$('ignore-reason').value.trim()||'本人确认无需提交'}:{});$('task-dialog').close();toast(action==='ignore'?'已忽略作业':'已恢复作业');await refresh();}
 catch(err){$('task-error').textContent=err.message;$('task-error').hidden=false;}
 finally{$('task-confirm').disabled=false;}
});
$('upload-input').addEventListener('change',async e=>{const chosen=[...e.target.files];e.target.value='';if(!chosen.length)return;let count=0;try{for(const f of chosen){if(f.size>63*1024*1024)throw new Error(f.name+' 超过 63 MB，请通过本机课程目录添加。');const data=new FormData();data.append('file',f);toast('正在上传 '+f.name);await api('/upload?course='+encodeURIComponent(uploadCourse),data);count++;}toast('已加入 '+count+' 份补充资料');await renderRoute();}catch(err){error((count?'已加入 '+count+' 份资料；':'')+err.message);await renderRoute();error(err.message);}});
$('scan').addEventListener('click',async()=>{
 if(syncRequest)return;syncRequest=true;error('');updateHealth(overview.health);
 try{const scan=await api('/scan',{});updateHealth({...overview.health,scan});await refresh();toast('正在同步课程，完成后自动更新');}
 catch(e){error(e.message);}
 finally{syncRequest=false;updateHealth(overview.health);if(connectionError)disconnected(connectionError);}
});
document.addEventListener('keydown',e=>{
 if(e.target.hasAttribute('data-pane')&&['ArrowLeft','ArrowRight','Home','End'].includes(e.key)){
  const tabs=[...document.querySelectorAll('[data-pane]')],i=tabs.indexOf(e.target);
  const next=e.key==='Home'?0:e.key==='End'?tabs.length-1:(i+(e.key==='ArrowLeft'?-1:1)+tabs.length)%tabs.length;
  e.preventDefault();setPane(tabs[next].dataset.pane);tabs[next].focus();
 }
 if(e.target.id==='chat-text'&&!e.isComposing&&(e.ctrlKey||e.metaKey)&&e.key==='Enter'){
  e.preventDefault();$('conversation-form').requestSubmit();
 }
});
window.addEventListener('pagehide',()=>reading?.save());
window.addEventListener('hashchange',()=>{courseTab='tasks';taskFilter='visible';renderRoute();});
async function start(){
 const params=new URLSearchParams(location.hash.slice(1));
 if(params.get('task')&&!params.get('view')){location.replace('/editor'+location.hash);return;}
 try{
  const token=params.get('token');
  if(token){const response=await fetch('/session',{method:'POST',headers:{'X-Editor-Token':token}});if(!response.ok)throw new Error('连接已失效，请重新打开 AvatarTHU。');document.querySelectorAll('a[href^="#view="]').forEach(a=>{const p=new URLSearchParams(a.hash.slice(1));p.set('token',token);a.hash=p.toString();});}
  if(window.avatarDesktop?.onState)window.avatarDesktop.onState(state=>desktopState(state).catch(e=>disconnected(e.message)));
  await refresh();
 }catch(e){disconnected(e.message);$('content').innerHTML=empty('等待连接本机后台','后台恢复后会自动重新连接。');}
 setInterval(pollActivity,2500);
 const resume=()=>{if(!document.hidden){refresh().catch(e=>disconnected(e.message));pollActivity();}};
 document.addEventListener('visibilitychange',resume);
 window.addEventListener('focus',resume);
 if(!window.avatarDesktop?.onState)setInterval(resume,5000);
}
for(const event of ['input','change'])document.addEventListener(event,e=>{if(e.target.closest('#settings-form'))settingsDirty=true;});
$('preview').addEventListener('close',()=>closeFileSurface($('preview-body')));
$('mark-form').addEventListener('submit',e=>{e.preventDefault();finishReadingMark();});

start();
