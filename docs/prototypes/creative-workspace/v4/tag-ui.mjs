import {TAG_COLORS,tagColor,tagKey,saveTag,validateTag,deleteTag,saveTagGroup,deleteTagGroup} from './tags.mjs';
export function createTagUI(ctx){
  const $=id=>document.getElementById(id),h=ctx.escapeHTML;
  let groupFilter='all',picker=null,editor=null,groupEditing=null,groupBaseline='',editorBaseline='',pendingDelete=null,busy=false;
  const catalog=(drafts=[])=>[...ctx.getState().tagCatalog,...drafts.filter(t=>!ctx.getState().tagCatalog.some(tag=>tag.id===t.id))];
  const chip=(tag,extra='')=>`<span class="tag-dot" style="--tag-color:${tagColor(tag.color)}"></span><span>${h(tag.name)}</span>${extra}`;
  const tagFor=(id,drafts=[])=>catalog(drafts).find(tag=>tag.id===id);
  const names=ids=>ids.map(id=>tagFor(id)?.name||'标签已删除');
  const formValue=()=>JSON.stringify(Object.fromEntries(new FormData($('tag-editor-form'))));
  function isDirty(id){
    if(id==='tag-editor')return Boolean(editor)&&formValue()!==editorBaseline;
    if(id==='tag-group-editor')return Boolean(groupEditing)&&$('tag-group-name').value!==groupBaseline;
    if(id==='tag-picker')return false;
    return false;
  }
  function renderManager(){
    const state=ctx.getState(),query=$('tag-manager-search').value.trim().toLocaleLowerCase();
    if(!['all','unfiled',...state.tagGroups.map(g=>g.id)].includes(groupFilter))groupFilter='all';
    const groups=[{id:'all',name:'全部标签'},{id:'unfiled',name:'未分组'},...state.tagGroups];
    $('tag-group-list').innerHTML=groups.map(group=>`<button data-tag-group="${h(group.id)}" aria-pressed="${groupFilter===group.id}"><span>${h(group.name)}</span><small>${state.tagCatalog.filter(tag=>group.id==='all'||(group.id==='unfiled'?!tag.groupId:tag.groupId===group.id)).length}</small></button>`).join('');
    $('tag-group-heading').textContent=groups.find(g=>g.id===groupFilter).name;$('tag-group-actions').hidden=['all','unfiled'].includes(groupFilter);
    const tags=state.tagCatalog.filter(tag=>(groupFilter==='all'||(groupFilter==='unfiled'?!tag.groupId:tag.groupId===groupFilter))&&`${tag.name} ${state.tagGroups.find(g=>g.id===tag.groupId)?.name||''}`.toLocaleLowerCase().includes(query)).sort((a,b)=>a.name.localeCompare(b.name,'zh-CN'));
    $('tag-manager-summary').textContent=`${tags.length} 个标签 · 使用数量不包含回收站`;
    $('managed-tag-list').innerHTML=tags.map(tag=>`<article class="managed-tag-row"><button class="tag-chip" data-browse-tag="${tag.id}" title="查看使用此标签的素材">${chip(tag)}</button><span class="managed-tag-group">${h(state.tagGroups.find(g=>g.id===tag.groupId)?.name||'未分组')}</span><span class="managed-tag-usage">${state.assets.filter(a=>a.tagIds.includes(tag.id)).length} 份素材</span><div><button class="icon-button" data-edit-tag="${tag.id}" aria-label="编辑标签：${h(tag.name)}" title="编辑名称、颜色和分组">${ctx.icon('edit')}</button><button class="icon-button" data-delete-tag="${tag.id}" aria-label="删除标签：${h(tag.name)}" title="删除标签">${ctx.icon('trash')}</button></div></article>`).join('')||'<div class="tag-empty">这里还没有标签。可以新建一个，或换个关键词。</div>';
  }
  function renderFilters(route){
    const ids=route.tags?route.tags.split(',').filter(Boolean):[];
    $('active-tag-filters').innerHTML=ids.map(id=>{const tag=tagFor(id)||{name:'标签已删除',color:'gray'};return `<button class="tag-chip" data-remove-tag-filter="${h(id)}" aria-label="移除筛选：${h(tag.name)}">${chip(tag,ctx.icon('x'))}</button>`;}).join('');
    $('tag-match-label').hidden=!ids.length;$('tag-filter-clear').hidden=!ids.length;$('tag-match-mode').value=route.tagMode||'all';
  }
  function fieldHTML(key,ids=[],drafts=[]){
    return `<span class="tag-field-label">标签 <small>可选</small></span><div class="tag-field-chips">${ids.map(id=>{const tag=tagFor(id,drafts);return tag?`<span class="tag-chip">${chip(tag)}</span>`:'';}).join('')}</div><button type="button" class="tag-select-button" data-pick-tags="${h(key)}">${ctx.icon('tag')}<span>${ids.length?'编辑已选标签':'选择或新建标签'}</span>${ctx.icon('plus')}</button>`;
  }
  function closeDropdown(restoreFocus=false){
    if(!picker)return;if(busy)return;
    const anchor=picker.anchor;anchor?.setAttribute('aria-expanded','false');$('tag-picker').hidden=true;document.body.append($('tag-picker'));picker=null;
    if(restoreFocus&&anchor?.isConnected)anchor.focus({preventScroll:true});
  }
  function openPicker({selected=[],drafts=[],onApply,allowCreate=true,title='选择标签',liveSave=false}){
    const anchor=document.activeElement;
    if(picker?.anchor===anchor){closeDropdown(true);return;}
    closeDropdown();
    picker={selected:[...new Set(selected.filter(id=>catalog(drafts).some(tag=>tag.id===id)))],inherited:[...drafts],created:[],closed:new Set(),onApply,allowCreate,anchor};
    anchor.setAttribute('aria-expanded','true');anchor.setAttribute('aria-controls','tag-picker');
    const host=anchor.closest('.tag-field')||(!allowCreate?$('asset-filter-tools'):$('detail-edit-tags').parentElement);
    if(liveSave)anchor.after($('tag-picker'));else host.append($('tag-picker'));$('tag-picker').classList.toggle('tag-filter-dropdown',!allowCreate);
    $('tag-picker-title').textContent=title;$('tag-picker-help').textContent=allowCreate?(liveSave?'选择后立即保存到此素材。':'选择后立即加入素材草稿，保存素材时生效。'):'选中或取消标签，素材结果立即更新。';
    $('tag-picker-search').value='';$('tag-picker-error').textContent='';$('tag-inline-new').hidden=true;$('tag-picker-create').hidden=!allowCreate;
    $('tag-live-count').textContent=allowCreate?`已选 ${picker.selected.length} 个标签`:`实时匹配 ${ctx.getResultCount()} 份素材`;
    renderPicker();$('tag-picker').hidden=false;$('tag-picker-search').focus({preventScroll:true});
  }
  async function publishSelection(previous){
    if(!picker||busy)return;const current=picker,owner=current.anchor.closest('dialog');busy=true;if(owner)owner.inert=true;$('tag-picker').setAttribute('aria-busy','true');$('tag-picker-groups').inert=true;$('tag-picker-selected').inert=true;$('tag-inline-new').inert=true;$('tag-picker-create').disabled=true;
    try{await current.onApply([...current.selected],[...current.inherited,...current.created].filter(tag=>current.selected.includes(tag.id)));$('tag-picker-error').textContent='';}
    catch(error){current.selected=previous;$('tag-picker-error').textContent=error.message;}
    finally{busy=false;if(owner)owner.inert=false;$('tag-picker').removeAttribute('aria-busy');$('tag-picker-groups').inert=false;$('tag-picker-selected').inert=false;$('tag-inline-new').inert=false;renderPicker();if(current.allowCreate)$('tag-live-count').textContent=`已选 ${current.selected.length} 个标签`;}
  }
  function renderPicker(){
    if(!picker)return;
    const state=ctx.getState(),all=catalog([...picker.inherited,...picker.created]),query=$('tag-picker-search').value.trim().toLocaleLowerCase();
    $('tag-picker-selected').innerHTML=picker.selected.map(id=>{const tag=all.find(t=>t.id===id);return tag?`<button type="button" class="tag-chip" data-unpick-tag="${id}" aria-label="移除标签：${h(tag.name)}">${chip(tag,ctx.icon('x'))}</button>`:'';}).join('')||'<span class="tag-help">尚未选择标签</span>';
    const matches=all.filter(tag=>`${tag.name} ${state.tagGroups.find(g=>g.id===tag.groupId)?.name||'未分组'}`.toLocaleLowerCase().includes(query));
    const groups=[...state.tagGroups,{id:null,name:'未分组'}];
    $('tag-picker-groups').innerHTML=groups.map(group=>{const tags=matches.filter(tag=>tag.groupId===group.id);return tags.length?`<details class="tag-picker-group" data-picker-group="${group.id||'unfiled'}" ${picker.closed.has(group.id||'unfiled')?'':'open'}><summary>${h(group.name)} <small>${tags.length}</small></summary><div>${tags.map(tag=>`<label><input type="checkbox" data-pick-tag="${tag.id}" ${picker.selected.includes(tag.id)?'checked':''}><span class="tag-chip">${chip(tag)}</span></label>`).join('')}</div></details>`:'';}).join('')||'<p class="tag-empty">没有找到匹配的标签。</p>';
    const exists=query&&all.some(tag=>tagKey(tag.name)===tagKey(query));$('tag-picker-create').disabled=Boolean(exists);$('tag-picker-create').textContent=exists?'已有同名标签，可在上方选择':query?`新建「${$('tag-picker-search').value.trim()}」`:'新建标签';
  }
  function openEditor(id=null){
    const tag=id?tagFor(id):null;editor={id};$('tag-editor-title').textContent=tag?'编辑标签':'新建标签';$('tag-name').value=tag?.name||'';
    $('tag-parent-group').innerHTML='<option value="">未分组</option>'+ctx.getState().tagGroups.map(g=>`<option value="${g.id}">${h(g.name)}</option>`).join('');$('tag-parent-group').value=tag?.groupId||(!['all','unfiled'].includes(groupFilter)?groupFilter:'');
    $('tag-color-options').innerHTML=TAG_COLORS.map(([key,label])=>`<label title="${label}"><input type="radio" name="color" value="${key}" ${(tag?.color||'sage')===key?'checked':''}><span class="tag-color-swatch" style="--tag-color:${tagColor(key)}"></span><span>${label}</span></label>`).join('');
    $('tag-editor-note').textContent='名称和颜色同步更新所有使用处，素材本身保持不变。';$('tag-editor-error').textContent='';editorBaseline=formValue();ctx.openModal('tag-editor');$('tag-name').focus();
  }
  function openGroupEditor(id=null){
    const group=ctx.getState().tagGroups.find(g=>g.id===id);groupEditing={id};$('tag-group-editor-title').textContent=group?'重命名标签分组':'新建标签分组';$('tag-group-name').value=group?.name||'';groupBaseline=$('tag-group-name').value;$('tag-group-error').textContent='';ctx.openModal('tag-group-editor');$('tag-group-name').focus();
  }
  $('tag-manage-open').addEventListener('click',()=>{groupFilter='all';$('tag-manager-search').value='';renderManager();ctx.openModal('tag-manager');});
  $('tag-group-list').addEventListener('click',event=>{const button=event.target.closest('[data-tag-group]');if(button){groupFilter=button.dataset.tagGroup;renderManager();}});
  $('tag-manager-search').addEventListener('input',renderManager);
  $('tag-new').addEventListener('click',()=>openEditor());$('tag-group-new').addEventListener('click',()=>openGroupEditor());$('tag-group-edit').addEventListener('click',()=>openGroupEditor(groupFilter));
  $('managed-tag-list').addEventListener('click',event=>{
    const button=event.target.closest('button');if(!button)return;
    if(button.dataset.editTag)openEditor(button.dataset.editTag);
    if(button.dataset.deleteTag)requestDelete('tag',button.dataset.deleteTag);
    if(button.dataset.browseTag){ctx.closeModal('tag-manager');ctx.navigate({view:'library',group:'',q:'',mediaType:'all',orientation:'all',tags:button.dataset.browseTag});}
  });
  $('tag-editor-form').addEventListener('submit',async event=>{
    event.preventDefault();if(!editor||busy)return;busy=true;$('tag-editor-save').disabled=true;$('tag-editor-form').setAttribute('aria-busy','true');$('tag-editor-form').inert=true;
    try{
      const current={...editor},input={id:current.id,name:$('tag-name').value,color:document.querySelector('#tag-color-options input:checked').value,groupId:$('tag-parent-group').value||null};
      await ctx.commit(saveTag(ctx.getState(),input));ctx.render();renderManager();ctx.notify('标签已保存');
      editor=null;ctx.closeModal('tag-editor');
    }catch(error){$('tag-editor-error').textContent=error.message;}
    finally{busy=false;$('tag-editor-save').disabled=false;$('tag-editor-form').removeAttribute('aria-busy');$('tag-editor-form').inert=false;}
  });
  $('tag-group-form').addEventListener('submit',async event=>{
    event.preventDefault();if(!groupEditing||busy)return;busy=true;$('tag-group-save').disabled=true;$('tag-group-form').inert=true;
    try{await ctx.commit(saveTagGroup(ctx.getState(),{id:groupEditing.id,name:$('tag-group-name').value}));groupEditing=null;ctx.closeModal('tag-group-editor');renderManager();ctx.render();ctx.notify('分组已保存');}
    catch(error){$('tag-group-error').textContent=error.message;}finally{busy=false;$('tag-group-save').disabled=false;$('tag-group-form').inert=false;}
  });
  function requestDelete(kind,id){
    pendingDelete={kind,id};const state=ctx.getState(),item=kind==='tag'?state.tagCatalog.find(t=>t.id===id):state.tagGroups.find(g=>g.id===id);if(!item)return;
    $('tag-delete-title').textContent=kind==='tag'?'删除这个标签？':'删除这个分组？';
    const uses=[...state.assets,...(state.trash||[]).map(t=>t.asset)].filter(a=>a.tagIds.includes(id)).length;
    $('tag-delete-description').textContent=kind==='tag'?`删除「${item.name}」将从 ${uses} 份素材（含回收站）上移除这个标签。素材不会被删除，此操作无法撤销。`:`删除「${item.name}」后，其中标签会回到“未分组”。标签、颜色和素材关联都保留。`;$('tag-delete-error').textContent='';ctx.openModal('tag-delete-dialog');
  }
  $('tag-group-delete').addEventListener('click',()=>requestDelete('group',groupFilter));
  $('tag-delete-confirm').addEventListener('click',async()=>{
    if(!pendingDelete||busy)return;busy=true;$('tag-delete-confirm').disabled=true;
    try{const {kind,id}=pendingDelete;await ctx.commit(kind==='tag'?deleteTag(ctx.getState(),id):deleteTagGroup(ctx.getState(),id));pendingDelete=null;ctx.closeModal('tag-delete-dialog');renderManager();ctx.render();ctx.notify(kind==='tag'?'标签已删除，素材仍保留':'分组已删除，标签已回到未分组');}
    catch(error){$('tag-delete-error').textContent=error.message;}finally{busy=false;$('tag-delete-confirm').disabled=false;}
  });
  $('tag-picker-search').addEventListener('input',()=>{if(!picker||busy)return;picker.closed.clear();renderPicker();});
  $('tag-picker-groups').addEventListener('toggle',event=>{if(!picker||!event.target.isConnected||!event.target.matches('details'))return;const key=event.target.dataset.pickerGroup;if(event.target.open)picker.closed.delete(key);else picker.closed.add(key);},true);
  $('tag-picker-create').addEventListener('click',()=>{
    if(!picker||busy)return;$('tag-inline-name').value=$('tag-picker-search').value.trim();
    $('tag-inline-group').innerHTML='<option value="">未分组</option>'+ctx.getState().tagGroups.map(g=>`<option value="${g.id}">${h(g.name)}</option>`).join('');
    $('tag-inline-color').innerHTML=TAG_COLORS.map(([key,label])=>`<option value="${key}">${label}</option>`).join('');$('tag-inline-new').hidden=false;$('tag-inline-name').focus();
  });
  $('tag-inline-cancel').addEventListener('click',()=>{$('tag-inline-new').hidden=true;$('tag-picker-search').focus({preventScroll:true});});
  $('tag-inline-save').addEventListener('click',async()=>{
    if(!picker||busy)return;
    try{const tag=validateTag({...ctx.getState(),tagCatalog:catalog([...picker.inherited,...picker.created])},{name:$('tag-inline-name').value,color:$('tag-inline-color').value,groupId:$('tag-inline-group').value||null});const previous=[...picker.selected];picker.created.push(tag);picker.selected.push(tag.id);await publishSelection(previous);if(!$('tag-picker-error').textContent){$('tag-inline-new').hidden=true;$('tag-picker-search').value='';renderPicker();$('tag-picker-search').focus({preventScroll:true});}}
    catch(error){$('tag-picker-error').textContent=error.message;}
  });
  $('tag-picker-groups').addEventListener('change',async event=>{const input=event.target;if(!picker||busy||!input.dataset.pickTag)return;const previous=[...picker.selected];picker.selected=input.checked?[...picker.selected,input.dataset.pickTag]:picker.selected.filter(id=>id!==input.dataset.pickTag);await publishSelection(previous);$('tag-picker-groups').querySelector(`[data-pick-tag="${input.dataset.pickTag}"]`)?.focus({preventScroll:true});});
  $('tag-picker-selected').addEventListener('click',async event=>{const button=event.target.closest('[data-unpick-tag]');if(button&&picker&&!busy){const previous=[...picker.selected];picker.selected=picker.selected.filter(id=>id!==button.dataset.unpickTag);await publishSelection(previous);$('tag-picker-search').focus({preventScroll:true});}});
  $('tag-picker-apply').addEventListener('click',()=>closeDropdown(true));$('tag-picker-close').addEventListener('click',()=>closeDropdown(true));
  document.addEventListener('pointerdown',event=>{if(picker&&!$('tag-picker').contains(event.target)&&!picker.anchor.contains(event.target))closeDropdown();});
  document.addEventListener('keydown',event=>{
    if(!picker)return;
    if(event.key==='Escape'){event.preventDefault();event.stopPropagation();closeDropdown(true);}
    if(event.key==='Enter'&&!event.isComposing&&['tag-picker-search','tag-inline-name'].includes(event.target.id)){event.preventDefault();if(event.target.id==='tag-inline-name')$('tag-inline-save').click();}
  },true);
  window.addEventListener('hashchange',()=>closeDropdown());
  $('tag-filter-open').addEventListener('click',()=>openPicker({selected:ctx.getRoute().tags.split(',').filter(Boolean),allowCreate:false,title:'按标签筛选素材',onApply:ids=>ctx.navigate({tags:ids.join(',')},true)}));
  $('active-tag-filters').addEventListener('click',event=>{const button=event.target.closest('[data-remove-tag-filter]');if(button)ctx.navigate({tags:ctx.getRoute().tags.split(',').filter(id=>id!==button.dataset.removeTagFilter).join(',')});});
  $('tag-filter-clear').addEventListener('click',()=>ctx.navigate({tags:''}));$('tag-match-mode').addEventListener('change',()=>ctx.navigate({tagMode:$('tag-match-mode').value}));
  return {renderFilters,fieldHTML,openPicker,closeDropdown,updateResults(count){if(picker&&!picker.allowCreate)$('tag-live-count').textContent=`实时匹配 ${count} 份素材`;},updateField(host,key,ids,drafts){const template=document.createElement('div');template.innerHTML=fieldHTML(key,ids,drafts);host.querySelector('.tag-field-chips').innerHTML=template.querySelector('.tag-field-chips').innerHTML;host.querySelector('.tag-select-button>span').textContent=ids.length?'编辑已选标签':'选择或新建标签';},isDirty,isBusy:()=>busy,chip,tagFor,names,
    mergeDrafts(next,drafts){for(const tag of drafts)if(!next.tagCatalog.some(t=>t.id===tag.id))next.tagCatalog.push(validateTag(next,tag));},
    editAssetTags(id){const asset=ctx.getState().assets.find(a=>a.id===id);if(!asset)return;openPicker({selected:asset.tagIds,title:'编辑素材标签',liveSave:true,onApply:async(ids,drafts)=>{const next=structuredClone(ctx.getState());for(const tag of drafts)if(!next.tagCatalog.some(t=>t.id===tag.id))next.tagCatalog.push(validateTag(next,tag));const target=next.assets.find(a=>a.id===id);if(!target)throw new Error('素材已不存在，请刷新。');target.tagIds=ids;await ctx.commit(next);ctx.render();ctx.refreshDetail(id);ctx.notify('素材标签已保存');}});}
  };
}
