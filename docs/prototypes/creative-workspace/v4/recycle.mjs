import {projectDocument} from './projects.mjs';
export const RETENTION_DAYS=[7,30,90,null];
export const retentionDays=state=>state.trashRetentionDays===undefined?30:state.trashRetentionDays;
export const expiredItems=(state,now=Date.now(),days=retentionDays(state))=>days===null?[]:(state.trash||[]).filter(item=>item.deletedAt+days*86400000<=now);

export function trashAssets(state,ids,now=Date.now()){
  const chosen=new Set(ids),assets=state.assets.filter(a=>chosen.has(a.id));
  if(!assets.length)throw new Error('所选素材已不存在，请刷新后查看。');
  const next=structuredClone(state);next.trash??=[];
  for(const project of next.projects){
    if(project.items.some(id=>chosen.has(id)))project.document=structuredClone(projectDocument(project,next.assets));
  }
  for(const asset of assets)next.trash.push({asset:structuredClone(asset),deletedAt:now,favorite:state.favorites.includes(asset.id),collectionIds:state.collections.filter(g=>g.items.includes(asset.id)).map(g=>g.id),projectIds:state.projects.filter(g=>g.items.includes(asset.id)).map(g=>g.id)});
  next.assets=next.assets.filter(a=>!chosen.has(a.id));next.favorites=next.favorites.filter(id=>!chosen.has(id));
  for(const group of [...next.collections,...next.projects])group.items=group.items.filter(id=>!chosen.has(id));
  return next;
}
export function restoreAssets(state,ids){
  const chosen=new Set(ids),next=structuredClone(state),items=(next.trash||[]).filter(item=>chosen.has(item.asset.id));
  if(!items.length)throw new Error('所选素材已不在回收站。');
  for(const item of items){
    const id=item.asset.id;if(!next.assets.some(a=>a.id===id))next.assets.push(item.asset);
    if(item.favorite&&!next.favorites.includes(id))next.favorites.push(id);
    for(const [groups,oldIds] of [[next.collections,item.collectionIds],[next.projects,item.projectIds]])for(const group of groups)if(oldIds.includes(group.id)&&!group.items.includes(id))group.items.push(id);
  }
  next.trash=next.trash.filter(item=>!chosen.has(item.asset.id));return next;
}
export function purgeAssets(state,ids){
  const chosen=new Set(ids),next=structuredClone(state);
  const removed=(next.trash||[]).filter(item=>chosen.has(item.asset.id)).map(item=>item.asset.id);
  next.trash=(next.trash||[]).filter(item=>!chosen.has(item.asset.id));
  // 采纳的分镜与执行记录是项目内容；只清除已删除素材的参考笔记。
  for(const project of next.projects)for(const id of removed)if(project.document?.referenceNotes)delete project.document.referenceNotes[id];
  return next;
}
export function setRetention(state,days,now=Date.now()){
  if(!RETENTION_DAYS.includes(days))throw new Error('请选择有效的保留时长。');
  const next=purgeAssets(state,expiredItems(state,now,days).map(item=>item.asset.id));next.trashRetentionDays=days;return next;
}

export function createRecycleBin(ctx){
  const $=id=>document.getElementById(id),h=ctx.escapeHTML;
  let pending=null,working=false;
  const selected=()=>[...$('trash-list').querySelectorAll('input:checked')].map(input=>input.value);
  const date=value=>new Date(value).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'});
  function render(state){
    const items=[...(state.trash||[])].sort((a,b)=>b.deletedAt-a.deletedAt),days=retentionDays(state);
    $('trash-retention').value=days===null?'never':String(days);
    $('trash-count').textContent=`${items.length} 份素材`;$('trash-empty').hidden=items.length>0;
    $('trash-list').innerHTML=items.map(item=>`<article class="trash-row glass"><label class="trash-select"><input type="checkbox" value="${h(item.asset.id)}" aria-label="选择回收站素材：${h(item.asset.title)}"></label><div class="trash-preview">${item.asset.kind==='image'?`<img src="${h(item.asset.src)}" alt="${h(item.asset.title)}" loading="lazy">`:ctx.icon(item.asset.kind==='link'?'link':'layers')}</div><div class="trash-info"><h3>${h(item.asset.title)}</h3><p>${{image:'图片',text:'文字',link:'链接'}[item.asset.kind]} · ${date(item.deletedAt)} 移入</p><small>${days===null?'保留至手动彻底删除':`剩余 ${Math.max(0,Math.ceil((item.deletedAt+days*86400000-Date.now())/86400000))} 天 · 到期后清理`}</small></div><div class="trash-row-actions"><button class="button" data-restore="${h(item.asset.id)}">恢复</button><button class="text-button danger-text" data-purge="${h(item.asset.id)}">彻底删除</button></div></article>`).join('');
    $('trash-clear').disabled=!items.length;$('trash-select-all').checked=false;$('trash-select-all').disabled=!items.length;syncSelected();
  }
  function syncSelected(){
    const ids=selected(),all=$('trash-list').querySelectorAll('input').length;
    $('trash-selection-count').textContent=`已选 ${ids.length} 项`;$('trash-restore-selected').disabled=!ids.length;$('trash-purge-selected').disabled=!ids.length;
    $('trash-select-all').checked=all>0&&ids.length===all;$('trash-select-all').indeterminate=ids.length>0&&ids.length<all;
  }
  function confirm(action,ids=[],days){
    if(working)return;
    const state=ctx.getState(),items=action==='trash'?state.assets.filter(a=>ids.includes(a.id)):(state.trash||[]).filter(item=>ids.includes(item.asset.id)).map(item=>item.asset);
    const usedProjects=state.projects.filter(project=>project.items.some(id=>ids.includes(id))).length,usedCollections=state.collections.filter(group=>group.items.some(id=>ids.includes(id))).length;
    pending={action,ids:[...ids],days};
    $('asset-delete-title').textContent=action==='trash'?'移入回收站？':action==='retention'?'保存保留时长？':'彻底删除这些素材？';
    const names=items.slice(0,3).map(a=>`「${a.title}」`).join('、')+(items.length>3?'等':'');
    $('asset-delete-description').textContent=action==='trash'?`${names}，共 ${items.length} 份素材，将从素材库、${usedCollections} 个集合和 ${usedProjects} 个项目参考中移出，可在回收站恢复。已采纳的分镜快照继续保留。`:action==='retention'?`保留时长改为${days===null?'不自动清理':`${days} 天`}。按移入回收站的时间计算，${ids.length?`${ids.length} 份已到期素材将立即彻底删除，无法恢复。`:'目前没有需要清理的素材。'}`:`${names}，共 ${items.length} 份素材，将从回收站彻底删除，无法恢复。项目已采纳的分镜快照仍保留。`;
    $('asset-delete-error').textContent='';$('asset-delete-confirm').textContent=action==='trash'?'移入回收站':action==='retention'?'保存设置':'彻底删除';ctx.openModal('asset-delete-dialog');
  }
  async function restore(ids){
    if(working)return;working=true;let restored=false;$('trash-panel').inert=true;
    try{await ctx.commit(restoreAssets(ctx.getState(),ids));ctx.render();ctx.notify('已恢复素材及仍存在的集合、最爱和项目引用');restored=true;}
    catch(error){ctx.notify(error.message);}finally{working=false;$('trash-panel').inert=false;if(restored)$('trash-count').focus({preventScroll:true});}
  }
  $('asset-delete-confirm').addEventListener('click',async()=>{
    if(!pending||working)return;working=true;$('asset-delete-confirm').disabled=true;
    try{
      const state=ctx.getState(),{action,ids,days}=pending;
      const next=action==='trash'?trashAssets(state,ids):action==='retention'?setRetention(state,days):purgeAssets(state,ids);
      await ctx.commit(next);ctx.closeModal('asset-delete-dialog');if(action==='trash'&&$('detail').open)ctx.closeModal('detail');pending=null;ctx.render();ctx.notify(action==='trash'?'素材已移入回收站':action==='retention'?'保留时长已保存':'素材已彻底删除');(action==='trash'?($('trash-open').hidden?$('main'):$('trash-open')):$('trash-count')).focus({preventScroll:true});
    }catch(error){$('asset-delete-error').textContent=error.message;}
    finally{working=false;$('asset-delete-confirm').disabled=false;}
  });
  $('trash-open').addEventListener('click',()=>ctx.navigate({view:'trash',group:'',q:''}));
  $('trash-list').addEventListener('change',syncSelected);
  $('trash-select-all').addEventListener('change',event=>{$('trash-list').querySelectorAll('input').forEach(input=>input.checked=event.target.checked);syncSelected();});
  $('trash-list').addEventListener('click',event=>{const button=event.target.closest('button');if(!button)return;if(button.dataset.restore)void restore([button.dataset.restore]);if(button.dataset.purge)confirm('purge',[button.dataset.purge]);});
  $('trash-restore-selected').addEventListener('click',()=>void restore(selected()));
  $('trash-purge-selected').addEventListener('click',()=>confirm('purge',selected()));
  $('trash-clear').addEventListener('click',()=>confirm('purge',(ctx.getState().trash||[]).map(item=>item.asset.id)));
  $('trash-save-retention').addEventListener('click',()=>{const raw=$('trash-retention').value,days=raw==='never'?null:Number(raw);confirm('retention',expiredItems(ctx.getState(),Date.now(),days).map(item=>item.asset.id),days);});
  async function sweep(){
    if(working||document.hidden||document.querySelector('dialog[open]')||!ctx.getState())return;
    const state=ctx.getState();
    if(!$('trash-panel').hidden&&$('trash-retention').value!==(retentionDays(state)===null?'never':String(retentionDays(state))))return;
    const expired=expiredItems(state);if(!expired.length)return;working=true;
    try{await ctx.commit(purgeAssets(state,expired.map(item=>item.asset.id)));ctx.render();ctx.notify(`已清理 ${expired.length} 份到期的回收站素材`);}catch(error){ctx.notify(error.message);}finally{working=false;}
  }
  document.addEventListener('visibilitychange',()=>{if(!document.hidden)void sweep();});
  window.setInterval(()=>void sweep(),60000);
  return {render,sweep,isBusy:()=>working,move:ids=>confirm('trash',ids)};
}
