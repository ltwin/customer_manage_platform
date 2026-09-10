export const PROJECT_TABS=[['overview','项目概览'],['references','参考素材'],['plan','拍摄策划'],['canvas','创作画布'],['live','现场模式']];
export const PLAN_SECTIONS=[['style','风格方向'],['shots','分镜表'],['location','场地与布景'],['lighting','布光方案'],['props','道具准备'],['post','后期思路']];

/** 已有内容优先；仅两个具名演示项目有默认样例，新项目保持空白。 */
export function projectDocument(project,assets){
  if(project.document)return project.document;
  const sea=project.id==='p-sea',mono=project.id==='p-mono',example=sea||mono;
  const reference=project.items.map(id=>assets.find(a=>a.id===id)).filter(a=>a?.kind==='image');
  return {
    brief:sea?'想拍一组轻盈、松弛的海边肖像。把海风、白裙与日落前的蓝色留在画面里，动作自然一些，构图留白多一些。':mono?'用一束光和建筑线条，拍一组安静、克制的黑白肖像。让人物与空间互相呼应，保留自然的姿态。':'',
    place:sea?'临海步道 · 需提前确认可拍区域':mono?'有侧窗的室内空间':'',
    time:sea?'日落前约 40 分钟':mono?'下午，侧窗有斜射光时':'',
    equipment:sea?'一机一镜、反光板；自然光为主':mono?'一盏灯、柔光附件、黑色背景':'',
    style:sea?'色彩：低饱和蓝、柔和的白，保留肤色温度。\n构图：让天空和海面留出呼吸，人物不必总在中央。\n情绪：轻盈、自然，像一次没有刻意安排的散步。':mono?'色彩：黑白，保留皮肤与墙面的层次。\n光线：侧向硬光与阴影交界。\n姿态：克制、自然，让建筑线条延续身体方向。':'',
    location:sea?'优先寻找无遮挡、能看到水平线的步道。\n备选：靠近海边的浅色墙面，风大时转为近景。\n待核实：通行范围、潮位与拍摄时段的光线。':mono?'优先选择有规则线条的墙面或立柱。\n场景尽量简洁，避开背景中突出的标识。\n先确认现场拍摄许可与可用空间。':'',
    lighting:sea?'先观察自然侧逆光，让海风与头发形成轮廓。\n脸部过暗时，用反光板轻补，不把阴影完全填平。\n阴天备选：靠近浅色墙面，利用柔和反射光。':mono?'主光从人物侧前方进入，先观察鼻影与下颌线。\n用黑色反光面压暗阴影一侧。\n先用现有一盏灯完成方向，再微调人物位置。':'',
    props:sea?'白色或浅色服装\n反光板与固定夹\n备用电池、擦镜布\n保暖外套和饮水':mono?'黑色与白色服装各一套\n背景布与固定夹\n灯具电池与备用电池':'',
    post:sea?'让蓝色偏柔和，避免把肤色一起调冷。\n保留高光的空气感，不抹平海面纹理。\n拍摄时多留几张干净背景，供后续画面调整。':mono?'先确认黑白灰层次，再局部调整皮肤与建筑。\n保持皮肤纹理，不把明暗交界修得过于平滑。\n保留一点颗粒作为质感尝试。':'',
    referenceNotes:{},propChecks:{},execution:[],
    shots:example?reference.slice(0,2).map((a,i)=>snapshotShot(a,`${project.id}-shot-${i+1}`,sea?(i?'花与海浪的呼应':'海岸与留白'):(i?'拱形里的留白':'墙面的光影'),sea?'参考空间与色彩，现场加入人物，动作以缓慢行走或停留为主。':'借用建筑的线条与光影，观察人物姿态和留白的关系。')):[],
  };
}
export function snapshotShot(asset,id,title=asset.title,description=asset.description||asset.text||'写下这次想拍的画面、动作和光线。'){
  return {id,title,description,sourceId:asset.id,sourceTitle:asset.title,image:asset.kind==='image'?asset.src:null,width:asset.width,height:asset.height,result:null};
}

export function createProjectWorkspace(ctx){
  const $=id=>document.getElementById(id),h=ctx.escapeHTML,icon=ctx.icon;
  let activeProject=null,activeRoute=null,activeAssets=[],editing=null,editorBaseline='',liveIndex=0,liveProject=null,memosOpen=false,imageDraft=null,pendingRemoval=null;
  const documentFor=()=>projectDocument(activeProject,activeAssets);
  const go=patch=>ctx.navigate({...patch,q:'',mediaType:'all',orientation:'all'});
  const unavailable=(label,reason)=>`<span class="future-control"><button class="button" disabled>${label}<span class="unavailable-label">暂未开发</span></button><small>${reason}</small></span>`;
  const paragraph=text=>`<p class="project-prose">${h(text||'这里还留着空白。按这次拍摄的需要，补上一点想法。').replace(/\n/g,'<br>')}</p>`;
  const imageFor=shot=>shot.image?`<img src="${h(shot.image)}" alt="${h(shot.sourceTitle||shot.title)}" loading="lazy">`:`<span class="shot-no-image">${icon('image')}<span>手写分镜</span></span>`;
  const resultLabel=result=>result==='done'?'已拍':result==='skipped'?'已跳过':'待拍';
  const action=(label,pane,extra='')=>`<button class="text-button" data-project-tab="${pane}" ${extra}>${label}${icon('arrow-right')}</button>`;

  function render(route,project,assets){
    activeProject=project;activeRoute=route;activeAssets=assets;
    if(liveProject!==project.id){liveIndex=0;liveProject=project.id;memosOpen=false;}
    const pane=PROJECT_TABS.some(([id])=>id===route.pane)?route.pane:'overview';
    $('project-tabs').innerHTML=PROJECT_TABS.map(([id,label])=>`<button data-project-tab="${id}" aria-pressed="${id===pane}" class="${id===pane?'active':''}">${label}${id==='canvas'?'<span class="tab-preview">布局预览</span>':''}</button>`).join('');
    $('project-content').hidden=pane==='references';
    if(pane==='references'){$('project-content').innerHTML='';return;}
    const doc=documentFor();
    if(pane==='overview')renderOverview(doc);
    if(pane==='plan')renderPlan(doc);
    if(pane==='canvas')renderCanvas(doc);
    if(pane==='live')renderLive(doc);
  }
  function renderOverview(doc){
    const cover=activeProject.items.map(id=>activeAssets.find(a=>a.id===id)).find(a=>a?.kind==='image');
    const short=doc.shots.slice(0,2);
    $('project-content').innerHTML=`
      <div class="project-overview-grid">
        <section class="project-intent glass"><div class="project-section-heading"><span class="eyebrow">THE CREATIVE INTENT</span><button class="text-button" data-project-edit="brief">编辑想法</button></div><h2>这一次，想表达什么？</h2>${paragraph(doc.brief)}<div class="project-intent-footer"><span>${activeProject.items.length} 份参考素材 · ${doc.shots.length} 个拍摄画面</span>${action('看看参考','references')}</div></section>
        <figure class="project-cover">${cover?`<img src="${h(cover.src)}" alt="${h(cover.title)}"><figcaption><span>本次创作的参考片段</span><span>${h(cover.tags.slice(0,2).join(' · '))}</span></figcaption>`:`<div class="project-cover-empty">${icon('spark')}<span>先留下一点想法，再慢慢寻找参考。</span></div>`}</figure>
        <section class="project-conditions glass"><div class="project-section-heading"><h3>环境与条件</h3><button class="text-button" data-project-edit="conditions">编辑条件</button></div><dl><div><dt>场地</dt><dd>${h(doc.place||'还没决定')}</dd></div><div><dt>时间 / 光线</dt><dd>${h(doc.time||'按现场情况安排')}</dd></div><div><dt>可用器材</dt><dd>${h(doc.equipment||'按需要补充')}</dd></div></dl><div class="future-inline"><span>客户 / 订单关联</span><span class="unavailable-label">原型未接入</span></div></section>
        <section class="project-outline glass"><div class="project-section-heading"><h3>把想法变成方案</h3>${action('打开策划','plan')}</div><div class="project-section-links">${PLAN_SECTIONS.map(([id,label])=>`<button data-project-section="${id}"><span>${label}</span><span>${id==='shots'?`${doc.shots.length} 个画面`:doc[id]?'已有手动内容':'可以慢慢补充'} ${icon('arrow-right')}</span></button>`).join('')}</div></section>
      </div>
      <section class="project-shots-preview"><div class="project-section-heading"><h2>先想好几个画面</h2>${action('查看分镜','plan','data-next-section="shots"')}</div><div class="shot-preview-grid">${short.length?short.map((shot,i)=>`<button class="shot-preview-card glass" data-edit-shot="${shot.id}"><div>${imageFor(shot)}</div><section><span class="eyebrow">SHOT ${String(i+1).padStart(2,'0')}</span><h3>${h(shot.title)}</h3><p>${h(shot.description)}</p></section></button>`).join(''):`<div class="project-empty"><p>从参考素材里挑一张，或直接写下想拍的画面。</p><button class="button" data-new-shot>手写第一个分镜</button></div>`}</div></section>
      <aside class="project-agent glass"><div>${icon('spark')}<div><h3>创作助手</h3><p>未来可结合参考素材与现场条件，帮你检查遗漏、提出方案。当前未接入模型，不会自动生成内容。</p></div></div>${unavailable('请助手推敲方案','模型与 Agent 尚未接入')}</aside>`;
  }
  function renderPlan(doc){
    const section=PLAN_SECTIONS.some(([id])=>id===activeRoute.section)?activeRoute.section:'style';
    $('project-content').innerHTML=`<div class="project-plan-heading"><div><h2>让参考，变成自己的方案。</h2><p>内容按需填写，不必完成一整套表格。</p></div>${unavailable('导出 PDF','导出文件与云端版本尚未接入')}</div><nav class="plan-section-nav" aria-label="策划章节">${PLAN_SECTIONS.map(([id,label])=>`<button data-project-section="${id}" class="${id===section?'active':''}" aria-pressed="${id===section}">${label}</button>`).join('')}</nav><div id="plan-section-body"></div>`;
    const label=PLAN_SECTIONS.find(([id])=>id===section)[1];
    if(section==='shots'){
      $('plan-section-body').innerHTML=`<div class="project-section-heading"><h3>分镜表 <span class="section-count">${doc.shots.length}</span></h3><div class="project-row-actions">${action('从参考素材加入','references')}<button class="button" data-new-shot>${icon('plus')}手写分镜</button></div></div><div class="shot-list">${doc.shots.map((shot,i)=>`<article class="shot-row glass"><div class="shot-index">${String(i+1).padStart(2,'0')}</div><div class="shot-thumb">${imageFor(shot)}</div><div class="shot-row-content"><h3>${h(shot.title)}</h3><p>${h(shot.description)}</p><span class="shot-result ${shot.result||''}">${resultLabel(shot.result)}</span></div><div class="shot-actions"><button class="text-button" data-edit-shot="${shot.id}" aria-label="编辑分镜：${h(shot.title)}">编辑</button><button class="text-button danger-text" data-remove-shot="${shot.id}" aria-label="移除分镜：${h(shot.title)}">移除</button></div></article>`).join('')||'<div class="project-empty">还没有分镜。选一份参考，或者直接写一个画面。</div>'}</div>`;
    }else{
      const props=section==='props';
      $('plan-section-body').innerHTML=`<article class="plan-article glass"><div class="project-section-heading"><span class="eyebrow">${props?'A NOTE BEFORE SHOOTING':'YOUR CREATIVE NOTES'}</span><button class="button" data-project-edit="${section}">${doc[section]?'编辑内容':'添加想法'}</button></div><h2>${label}</h2>${props?renderProps(doc):paragraph(doc[section])}<div class="plan-content-note">手动内容 · 仅保存在当前浏览器</div></article>${section==='lighting'?`<aside class="plan-future-note"><p>示意图可以帮助核对光源位置。当前先保留文字方案。</p>${unavailable('绘制布光图','专业布光图编辑暂未实现')}</aside>`:''}`;
    }
  }
  function renderProps(doc){
    const props=doc.props.split('\n').map(s=>s.trim()).filter(Boolean);
    return props.length?`<div class="project-props">${props.map((text,i)=>`<label><input type="checkbox" data-prop-index="${i}" ${doc.propChecks[text]?'checked':''}><span>${h(text)}</span></label>`).join('')}</div>`:paragraph('');
  }
  function renderCanvas(doc){
    const refs=activeProject.items.map(id=>activeAssets.find(a=>a.id===id)).filter(a=>a?.kind==='image').slice(0,2);
    $('project-content').innerHTML=`<div class="project-plan-heading"><div><h2>让画面彼此对话。</h2><p>内容来自本项目。这里展示未来画布的布局关系。</p></div><span class="prototype-label">布局预览</span></div><div class="canvas-explanation">可以浏览参考与方案节点；拖拽、缩放、连线和自动排布暂未开发，当前布局不会保存为正式画布。</div><div class="canvas-scroll"><div class="project-canvas"><svg class="canvas-lines" viewBox="0 0 1020 650" aria-hidden="true"><path d="M270 190C365 190 330 120 420 120M270 420C360 420 345 320 420 320M665 130C735 130 715 240 770 240M670 420C740 420 710 330 770 330"/></svg><div class="canvas-frame-label">${h(activeProject.name)} · 创作方向</div>${refs.map((asset,i)=>`<article class="canvas-node canvas-reference node-${i}"><img src="${h(asset.src)}" alt="${h(asset.title)}"><span>参考 · ${h(asset.title)}</span></article>`).join('')}<article class="canvas-node canvas-intent"><span class="eyebrow">创作意图</span><p>${h(doc.brief||'写下本次的创作意图，让参考有一个共同方向。')}</p><button class="text-button" data-project-edit="brief">编辑想法</button></article><article class="canvas-node canvas-style"><span class="eyebrow">风格与光线</span><p>${h((doc.style||'先确定想保留的色彩、光线与情绪。').split('\n')[0])}</p><button class="text-button" data-project-section="style">查看方案</button></article><article class="canvas-node canvas-output"><span class="eyebrow">要拍的画面</span><h3>${doc.shots.length} 个分镜</h3><p>${h(doc.shots.map(s=>s.title).join(' / ')||'从参考里选择，或手写新的画面。')}</p><button class="text-button" data-project-section="shots">打开分镜表</button></article></div></div><div class="canvas-unavailable">${unavailable('自由排布','拖拽和位置保存暂未开发')}${unavailable('添加连线','节点关系编辑暂未开发')}</div>`;
  }
  function renderLive(doc){
    liveIndex=Math.min(Math.max(liveIndex,0),Math.max(0,doc.shots.length-1));const shot=doc.shots[liveIndex];
    if(!shot){$('project-content').innerHTML=`<div class="project-empty glass"><h2>先留下一个要拍的画面</h2><p>有了分镜，就能在这里对着参考图拍摄。</p><button class="button primary" data-new-shot>手写第一个分镜</button>${action('从参考素材挑选','references')}</div>`;return;}
    $('project-content').innerHTML=`<div class="live-heading"><div><span class="eyebrow">ON LOCATION · 本地演示</span><h2>把注意力，留给眼前。</h2></div><div class="live-page"><button class="icon-button" data-live-step="-1" aria-label="上一个画面" ${liveIndex===0?'disabled':''}>${icon('arrow-left')}</button><span>${liveIndex+1} / ${doc.shots.length}</span><button class="icon-button" data-live-step="1" aria-label="下一个画面" ${liveIndex===doc.shots.length-1?'disabled':''}>${icon('arrow-right')}</button></div></div><div class="live-workspace"><div class="live-reference">${imageFor(shot)}</div><section class="live-instruction glass"><span class="shot-result ${shot.result||''}">${resultLabel(shot.result)}</span><h2>${h(shot.title)}</h2>${paragraph(shot.description)}<div class="live-source">${shot.sourceTitle?`参考快照：${h(shot.sourceTitle)}`:'手写画面 · 无参考图'}</div><div class="live-buttons"><button class="button primary" data-live-result="done" ${shot.result==='done'?'disabled':''}>${icon('check-square')}拍到了</button><button class="button" data-live-result="skipped" ${shot.result==='skipped'?'disabled':''}>先跳过</button>${shot.result?'<button class="text-button" data-live-result="clear">撤销记录</button>':''}</div><p class="local-save-note">操作只记录到本地原型，不会写入真实拍摄记录。</p></section></div><details class="live-memos glass" ${memosOpen?'open':''}><summary>拍摄备忘与道具</summary>${renderProps(doc)}</details>`;
  }
  async function saveDocument(update,projectId=activeProject.id){
    const next=structuredClone(ctx.getState());
    const target=next.projects.find(p=>p.id===projectId);
    if(!target)throw new Error('项目已不存在，请返回项目列表。');
    const doc=structuredClone(projectDocument(target,next.assets));update(doc,next,target);target.document=doc;target.updatedAt=Date.now();
    await ctx.commit(next);ctx.render();
  }
  function field(name,label,value,multiline=true){
    return `<label for="project-field-${name}">${label}</label>${multiline?`<textarea id="project-field-${name}" name="${name}" rows="5" maxlength="6000">${h(value)}</textarea>`:`<input id="project-field-${name}" name="${name}" value="${h(value)}" maxlength="200">`}`;
  }
  const formValues=()=>Object.fromEntries(new FormData($('project-editor-form')));
  const editorValue=()=>JSON.stringify({fields:formValues(),image:imageDraft});
  function openEditor(kind,shotId){
    const doc=documentFor();editing={kind,shotId,projectId:activeProject.id};imageDraft=null;
    let fields='';
    if(kind==='conditions'){
      $('project-editor-title').textContent='环境与拍摄条件';fields=field('place','场地',doc.place,false)+field('time','时间 / 光线',doc.time,false)+field('equipment','可用器材',doc.equipment);
    }else if(kind==='shot'){
      const shot=doc.shots.find(s=>s.id===shotId);$('project-editor-title').textContent=shot?'编辑这个画面':'写下一个新的画面';imageDraft=shot?.image?{id:shot.sourceId,title:shot.sourceTitle,src:shot.image,width:shot.width,height:shot.height}:null;fields=field('title','画面名称',shot?.title||'',false)+field('description','画面、动作与光线',shot?.description||'')+'<section id="shot-image-editor" aria-label="分镜参考图"></section>'; 
    }else{
      $('project-editor-title').textContent=kind==='brief'?'这一次的创作意图':PLAN_SECTIONS.find(([id])=>id===kind)[1];fields=field(kind,kind==='props'?'一行一条，按需要准备':'写下这次想采用的表达',doc[kind]);
    }
    $('project-editor-fields').innerHTML=fields;$('project-editor-error').textContent='';if(kind==='shot')renderImageEditor();editorBaseline=editorValue();ctx.openModal('project-editor');
    $('project-editor-fields').querySelector('input,textarea')?.focus();
  }
  function renderImageEditor(){
    $('shot-image-editor').innerHTML=`<h3>参考图片 <span>可选</span></h3><div class="shot-image-preview">${imageDraft?`<img src="${h(imageDraft.src)}" alt="${h(imageDraft.title||'分镜参考图')}"><span>${h(imageDraft.title||'参考图片')}</span>`:'<p>为这个画面插入一张参考图</p>'}</div><div class="shot-image-actions"><button type="button" class="button" data-image-library>从素材库选择</button><button type="button" class="button" data-image-upload>${imageDraft?'上传替换图片':'上传图片'}</button>${imageDraft?'<button type="button" class="text-button danger-text" data-image-clear>移除图片</button>':''}</div><input id="shot-image-file" type="file" accept="image/jpeg,image/png,image/webp" hidden><p class="shot-image-note">JPG、PNG、WebP · 单张不超过 5 MB。保存后，所选图片也会加入本项目参考；上传图片会存入素材库。</p><div id="shot-image-library" hidden><label for="shot-image-search">查找图片</label><input id="shot-image-search" type="search" placeholder="按名称或标签查找"><div id="shot-image-options" class="shot-image-options"></div></div>`;
  }
  function renderImageOptions(query=''){
    const images=ctx.getState().assets.filter(a=>a.kind==='image'&&`${a.title} ${(a.tags||[]).join(' ')}`.toLowerCase().includes(query.trim().toLowerCase())).sort((a,b)=>Number(activeProject.items.includes(b.id))-Number(activeProject.items.includes(a.id)));
    $('shot-image-options').innerHTML=images.map(a=>`<button type="button" data-image-asset="${h(a.id)}" aria-pressed="${imageDraft?.id===a.id}" aria-label="选择图片：${h(a.title)}"><img src="${h(a.src)}" alt="" loading="lazy"><span>${h(a.title)}</span><small>${activeProject.items.includes(a.id)?'本项目参考':'素材库'}</small></button>`).join('')||'<p>没有匹配的图片，可以换个关键词或上传。</p>';
  }
  $('project-editor-fields').addEventListener('click',event=>{
    const button=event.target.closest('button');if(!button||$('project-editor-save').disabled)return;
    if(button.hasAttribute('data-image-library')){$('shot-image-library').hidden=false;renderImageOptions();$('shot-image-search').focus();}
    if(button.hasAttribute('data-image-upload'))$('shot-image-file').click();
    if(button.hasAttribute('data-image-clear')){imageDraft=null;renderImageEditor();$('shot-image-editor').querySelector('button').focus();}
    if(button.dataset.imageAsset){const asset=ctx.getState().assets.find(a=>a.id===button.dataset.imageAsset);if(!asset)return;imageDraft={id:asset.id,title:asset.title,src:asset.src,width:asset.width,height:asset.height};renderImageEditor();$('shot-image-editor').querySelector('button').focus();}
  });
  $('project-editor-fields').addEventListener('input',event=>{if(event.target.id==='shot-image-search')renderImageOptions(event.target.value);});
  $('project-editor-fields').addEventListener('change',async event=>{
    if(event.target.id!=='shot-image-file')return;
    const file=event.target.files[0];event.target.value='';if(!file)return;
    const error=$('project-editor-error');error.textContent='';
    if(!['image/jpeg','image/png','image/webp'].includes(file.type)||file.size>5*1024*1024){error.textContent='请选择不超过 5 MB 的 JPG、PNG 或 WebP 图片。';return;}
    const current=editing;$('project-editor-save').disabled=true;$('shot-image-editor').inert=true;
    try{const image=await ctx.readImage(file);if(editing===current){imageDraft={id:crypto.randomUUID(),title:file.name.replace(/\.[^.]+$/,''),...image,uploaded:true};renderImageEditor();}}
    catch(failure){error.textContent=failure.message;}
    finally{$('project-editor-save').disabled=false;$('shot-image-editor').inert=false;}
  });
  $('remove-shot-confirm').addEventListener('click',async()=>{
    const current=pendingRemoval,button=$('remove-shot-confirm');if(!current||button.disabled)return;button.disabled=true;
    try{await saveDocument(doc=>{const shot=doc.shots.find(s=>s.id===current.shotId);if(!shot)throw new Error('这个分镜已不存在，请刷新后查看。');doc.removedShots??=[];doc.removedShots.push({...shot,removedAt:new Date().toISOString()});doc.shots=doc.shots.filter(s=>s.id!==current.shotId);},current.projectId);pendingRemoval=null;ctx.closeModal('remove-shot-dialog');ctx.notify('已移除分镜，原素材与拍摄记录保留');document.querySelector('[data-new-shot]')?.focus({preventScroll:true});}
    catch(error){$('remove-shot-error').textContent=error.message;}
    finally{button.disabled=false;}
  });
  $('project-editor-form').addEventListener('submit',async event=>{
    event.preventDefault();if(!editing||$('project-editor-save').disabled)return;const values=formValues(),button=$('project-editor-save');
    if(editing.projectId!==activeProject?.id){$('project-editor-error').textContent='当前项目已切换，请关闭窗口后重新编辑。';return;}
    if(editing.kind==='shot'&&!values.title.trim()){$('project-editor-error').textContent='给这个画面一个名称，方便现场找到。';const field=$('project-field-title');field.setAttribute('aria-invalid','true');field.setAttribute('aria-describedby','project-editor-error');field.focus();return;}
    button.disabled=true;$('project-editor-fields').inert=true;
    try{
      const current={...editing},picture=imageDraft?{...imageDraft}:null;await saveDocument((doc,next,target)=>{
        if(current.kind==='shot'){
          let shot=doc.shots.find(s=>s.id===current.shotId);
          if(current.shotId&&!shot)throw new Error('这个分镜已不存在，请关闭后刷新。');
          if(!shot){shot={id:crypto.randomUUID(),result:null};doc.shots.push(shot);}
          Object.assign(shot,{title:values.title.trim(),description:values.description,image:picture?.src||null,sourceId:picture?.id||null,sourceTitle:picture?.title||null,width:picture?.width||null,height:picture?.height||null});
          if(picture){
            if(picture.uploaded)next.assets.unshift({id:picture.id,kind:'image',title:picture.title,src:picture.src,width:picture.width,height:picture.height,category:'素材',tags:['新收集'],note:'',created:Date.now()});
            if(next.assets.some(a=>a.id===picture.id)&&!target.items.includes(picture.id))target.items.push(picture.id);
          }
        }else if(current.kind==='conditions')Object.assign(doc,values);
        else{doc[current.kind]=values[current.kind];if(current.kind==='props'){const lines=new Set(values.props.split('\n').map(s=>s.trim()));doc.propChecks=Object.fromEntries(Object.entries(doc.propChecks).filter(([key])=>lines.has(key)));}}
      });
      editorBaseline=JSON.stringify(values);editing=null;ctx.closeModal('project-editor');ctx.notify('已保存到本地项目');
      document.querySelector(current.kind==='shot'?'[data-new-shot]':`[data-project-edit="${current.kind}"]`)?.focus({preventScroll:true});
    }catch(error){$('project-editor-error').textContent=error.message;}finally{button.disabled=false;$('project-editor-fields').inert=false;}
  });
  $('project-workbench').addEventListener('click',async event=>{
    const button=event.target.closest('button');if(!button||button.disabled)return;
    if(button.dataset.projectTab){go({pane:button.dataset.projectTab,section:button.dataset.nextSection||''});return;}
    if(button.dataset.projectSection){go({pane:'plan',section:button.dataset.projectSection});return;}
    if(button.dataset.projectEdit){openEditor(button.dataset.projectEdit);return;}
    if(button.hasAttribute('data-new-shot')){openEditor('shot');return;}
    if(button.dataset.removeShot){
      const shot=documentFor().shots.find(s=>s.id===button.dataset.removeShot);if(!shot)return;
      pendingRemoval={projectId:activeProject.id,shotId:shot.id};$('remove-shot-description').textContent=`移除「${shot.title}」后，它将不再出现在分镜表和现场模式中。原素材、项目参考与已有拍摄记录会保留。`;$('remove-shot-error').textContent='';ctx.openModal('remove-shot-dialog');return;
    }
    if(button.dataset.editShot){openEditor('shot',button.dataset.editShot);return;}
    if(button.dataset.liveStep){liveIndex+=Number(button.dataset.liveStep);renderLive(documentFor());return;}
    if(button.dataset.liveResult){
      button.disabled=true;
      try{const shotId=documentFor().shots[liveIndex].id;await saveDocument(doc=>{const shot=doc.shots.find(s=>s.id===shotId);const result=button.dataset.liveResult;doc.execution.push({id:crypto.randomUUID(),shotId,previous:shot.result,result:result==='clear'?null:result,at:new Date().toISOString()});shot.result=result==='clear'?null:result;});ctx.notify('现场记录已保存在本地原型');}
      catch(error){ctx.notify(error.message);button.disabled=false;}
    }
  });
  $('project-workbench').addEventListener('change',async event=>{
    const input=event.target;if(!input.matches('[data-prop-index]'))return;
    const text=documentFor().props.split('\n').map(s=>s.trim()).filter(Boolean)[Number(input.dataset.propIndex)];const checked=input.checked;input.disabled=true;
    try{await saveDocument(doc=>{doc.propChecks[text]=checked;});$('project-workbench').querySelector(`[data-prop-index="${input.dataset.propIndex}"]`)?.focus({preventScroll:true});}catch(error){input.checked=!checked;input.disabled=false;ctx.notify(error.message);}
  });
  $('project-workbench').addEventListener('toggle',event=>{if(event.target.matches('.live-memos'))memosOpen=event.target.open;},true);
  async function addShots(ids){
    if(!activeProject)return;
    const assets=ctx.getState().assets.filter(a=>ids.includes(a.id)&&activeProject.items.includes(a.id));
    if(!assets.length)return;
    await saveDocument(doc=>{assets.forEach(asset=>{doc.shots.push(snapshotShot(asset,crypto.randomUUID()));});});
    ctx.notify('已将所选参考加入分镜，内容以快照保留');go({pane:'plan',section:'shots'});
  }
  return {render,addShots,isDirty:()=>Boolean(editing)&&editorBaseline!==editorValue(),referenceNote:(project,assetId)=>projectDocument(project,ctx.getState().assets).referenceNotes[assetId]||'',async saveReferenceNote(projectId,assetId,note){
    const next=structuredClone(ctx.getState()),project=next.projects.find(p=>p.id===projectId);if(!project)throw new Error('项目不存在');
    project.document=structuredClone(projectDocument(project,next.assets));project.document.referenceNotes[assetId]=note;project.updatedAt=Date.now();await ctx.commit(next);
  }};
}
