/** 一次只创建一种素材；图片的说明属于图片，文字正文不按行拆分。 */
export function createCollector(ctx){
  const $=id=>document.getElementById(id),h=ctx.escapeHTML;
  const labels={image:'图片',text:'文字',link:'链接'};
  let kind='image',files=[],projectId=null;
  let tagDrafts=[],textTagIds=[],linkTagIds=[];
  function renderTagFields(){for(const type of ['text','link'])$(`import-${type}-tags`).innerHTML=ctx.tagUI.fieldHTML(type,type==='text'?textTagIds:linkTagIds,tagDrafts);}
  const value=id=>$(id).value.trim();
  const busy=()=>$('import-submit').disabled;
  function isDirty(){return files.length>0||textTagIds.length>0||linkTagIds.length>0||[...$('import-fields').querySelectorAll('input:not([type=file]),textarea')].some(input=>!input.closest('#tag-picker')&&input.value.trim());}
  function reset(){
    ctx.tagUI.closeDropdown();
    files.forEach(item=>URL.revokeObjectURL(item.preview));files=[];
    $('import-form').reset();$('file-list').innerHTML='';clearErrors();
    kind='image';projectId=null;tagDrafts=[];textTagIds=[];linkTagIds=[];renderTagFields();syncType();
  }
  function clearErrors(){
    $('import-error').textContent='';
    $('import-fields').querySelectorAll('[aria-invalid]').forEach(el=>{el.removeAttribute('aria-invalid');el.removeAttribute('aria-describedby');});
  }
  function syncType(){
    for(const key of Object.keys(labels)){
      $(`import-${key}-panel`).hidden=kind!==key;
      document.querySelector(`[data-import-kind="${key}"]`).setAttribute('aria-pressed',String(kind===key));
    }
    $('import-submit').textContent=kind==='image'&&files.length?`保存 ${files.length} 份图片素材`:`保存${labels[kind]}素材`;
  }
  function open(){
    if($('import-dialog').open)return;
    renderTagFields();projectId=ctx.getProjectId();$('import-title').textContent=projectId?'为这次创作收集参考':'收集新的灵感';ctx.openModal('import-dialog');
  }
  function switchType(next,after){
    if(busy())return;
    if(next===kind){after?.();return;}
    ctx.guard('import-dialog',()=>{
      const targetProject=projectId;reset();projectId=targetProject;kind=next;syncType();
      document.querySelector(`[data-import-kind="${kind}"]`).focus();after?.();
    },`切换为${labels[next]}后，当前${labels[kind]}素材的未保存内容会被丢弃。选择「继续编辑」可保留内容。`);
  }
  $('import-open').addEventListener('click',open);
  $('import-fields').addEventListener('click',event=>{
    const button=event.target.closest('button');if(!button||busy())return;
    if(button.dataset.pickTags){
      const key=button.dataset.pickTags,item=files.find(file=>file.id===key),ids=item?item.tagIds:key==='text'?textTagIds:linkTagIds;
      ctx.tagUI.openPicker({selected:ids,drafts:tagDrafts,onApply:(selected,drafts)=>{
        for(const tag of drafts)if(!tagDrafts.some(t=>t.id===tag.id))tagDrafts.push(tag);
        if(item){item.tagIds=selected;const host=$('file-list').querySelector(`[data-file-id="${item.id}"] .tag-field`);ctx.tagUI.updateField(host,item.id,item.tagIds,tagDrafts);}
        else{if(key==='text')textTagIds=selected;else linkTagIds=selected;ctx.tagUI.updateField($(`import-${key}-tags`),key,selected,tagDrafts);}
      }});return;
    }
    if(button.dataset.importKind)switchType(button.dataset.importKind);
    if(button.dataset.removeFile){
      const i=files.findIndex(item=>item.id===button.dataset.removeFile);if(i<0)return;
      URL.revokeObjectURL(files[i].preview);files.splice(i,1);renderFiles();syncType();$('files').focus();
    }
  });
  $('import-fields').addEventListener('input',event=>{if(event.target.hasAttribute('aria-invalid'))clearErrors();});
  $('files').addEventListener('change',event=>{addFiles([...event.target.files]);event.target.value='';});
  function addFiles(incoming){
    if(busy())return;const errors=[];
    for(const file of incoming){
      if(!['image/jpeg','image/png','image/webp'].includes(file.type)){errors.push(`${file.name}：请选择 JPG、PNG 或 WebP`);continue;}
      if(file.size>5*1024*1024){errors.push(`${file.name}：超过 5 MB`);continue;}
      if(files.some(item=>item.file.name===file.name&&item.file.size===file.size&&item.file.lastModified===file.lastModified))continue;
      if(files.length>=50){errors.push('每次最多导入 50 张图片');break;}
      files.push({id:crypto.randomUUID(),file,preview:URL.createObjectURL(file),title:file.name.replace(/\.[^.]+$/,''),description:'',tagIds:[],source:''});
    }
    renderFiles();syncType();$('import-error').textContent=errors.join('；');
  }
  function renderFiles(){
    $('file-list').innerHTML=files.map((item,i)=>`<article class="import-image-item" data-file-id="${item.id}"><div class="import-image-heading"><span>图片 ${String(i+1).padStart(2,'0')} · ${h(item.file.name)}</span><button type="button" class="text-button danger-text" data-remove-file="${item.id}" aria-label="移除待保存图片：${h(item.file.name)}">移除</button></div><img class="import-image-preview" src="${h(item.preview)}" alt="${h(item.title)}"><div class="import-image-data">${imageField(item,'title','标题','必填',false,200)}${imageField(item,'description','图片描述','可选 · 只描述这张图片',true,6000)}<div class="tag-field">${ctx.tagUI.fieldHTML(item.id,item.tagIds,tagDrafts)}</div>${imageField(item,'source','来源网址','可选',false,2048)}</div></article>`).join('');
    $('file-list').querySelectorAll('img').forEach(img=>img.addEventListener('error',()=>{const error=document.createElement('p');error.className='import-hint';error.textContent='图片无法预览，请移除后重新选择。';img.replaceWith(error);}));
  }
  function imageField(item,key,label,hint,multiline,max){
    const id=`import-${item.id}-${key}`;
    return `<label for="${id}">${label} <span>${hint}</span></label>${multiline?`<textarea id="${id}" data-file-field="${key}" rows="3" maxlength="${max}">${h(item[key])}</textarea>`:`<input id="${id}" data-file-field="${key}" type="${key==='source'?'url':'text'}" maxlength="${max}" value="${h(item[key])}">`}`;
  }
  $('file-list').addEventListener('input',event=>{
    const input=event.target,key=input.dataset.fileField;if(!key)return;
    const item=files.find(file=>file.id===input.closest('[data-file-id]').dataset.fileId);if(item)item[key]=input.value;
  });
  function invalid(id,message){
    const input=$(id);input.setAttribute('aria-invalid','true');input.setAttribute('aria-describedby','import-error');input.focus();throw new Error(message);
  }
  function url(raw,id,required=false){
    if(!raw.trim()&&!required)return '';
    try{const parsed=new URL(raw.trim());if(!['http:','https:'].includes(parsed.protocol)||!parsed.hostname)throw new Error();return parsed.href;}
    catch{invalid(id,'请填写完整的 http:// 或 https:// 网址。');}
  }
  $('import-form').addEventListener('submit',async event=>{
    event.preventDefault();if(busy())return;clearErrors();
    const button=$('import-submit');
    try{
      // 先校验所有输入，再解码；素材与项目引用只有一次提交。
      let drafts=[];
      if(kind==='image'){
        if(!files.length)invalid('files','请先选择至少一张图片。');
        drafts=files.map(item=>{
          if(!item.title.trim())invalid(`import-${item.id}-title`,'请为每张图片填写标题。');
          return {...item,title:item.title.trim(),description:item.description.trim(),source:url(item.source,`import-${item.id}-source`),tagIds:[...item.tagIds]};
        });
      }else if(kind==='text'){
        if(!value('import-text-title'))invalid('import-text-title','请为这份文字素材填写标题。');
        if(!value('import-text'))invalid('import-text','请填写正文。整篇内容会保存为一份文字素材。');
        drafts=[{kind:'text',title:value('import-text-title'),text:value('import-text'),category:'文字',tagIds:[...textTagIds],width:4,height:4}];
      }else{
        const source=url(value('import-link-url'),'import-link-url',true),description=value('import-link-description');
        drafts=[{kind:'link',title:value('import-link-title')||new URL(source).hostname,source,description,text:description||source,category:'链接',tagIds:[...linkTagIds],width:4,height:4}];
      }
      button.disabled=true;$('import-fields').inert=true;$('import-form').setAttribute('aria-busy','true');
      const assets=[];
      for(const [i,draft] of drafts.entries()){
        let content=draft;
        if(kind==='image'){
          button.textContent=`正在读取 ${i+1} / ${drafts.length}`;
          content={kind:'image',title:draft.title,description:draft.description,tagIds:draft.tagIds,source:draft.source,category:'素材',...await ctx.readImage(draft.file)};
        }
        assets.push({id:crypto.randomUUID(),note:'',created:Date.now()+i,...content});
      }
      const next=structuredClone(ctx.getState()),targetId=projectId;ctx.tagUI.mergeDrafts(next,tagDrafts.filter(tag=>assets.some(asset=>asset.tagIds.includes(tag.id))));next.assets.unshift(...assets);
      if(targetId){const project=next.projects.find(p=>p.id===targetId);if(!project)throw new Error('项目已不存在，请关闭后重新收集。');project.items=[...new Set([...project.items,...assets.map(a=>a.id)])];}
      await ctx.commit(next);const label=labels[kind];ctx.closeModal('import-dialog');ctx.navigate({view:targetId?'projects':'library',group:targetId||'',pane:targetId?'references':'overview',q:'',mediaType:'all',orientation:'all',tags:'',sort:'recent'});ctx.render();ctx.notify(`已保存 ${assets.length} 份${label}素材`);
    }catch(error){$('import-error').textContent=error.message;}
    finally{button.disabled=false;$('import-fields').inert=false;$('import-form').removeAttribute('aria-busy');syncType();}
  });
  return {reset,isDirty,receiveFiles(incoming){
    if(busy()){ctx.notify('正在保存，请稍候再添加图片。');return;}
    open();switchType('image',()=>addFiles(incoming));
  }};
}
