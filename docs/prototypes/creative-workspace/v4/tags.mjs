export const TAG_COLORS=[['sage','鼠尾草','#55775f'],['blue','雾蓝','#527caa'],['violet','紫藤','#8670a7'],['rose','蔷薇','#af6579'],['amber','琥珀','#a77d35'],['coral','珊瑚','#b5684e'],['teal','青绿','#40847f'],['gray','石墨','#717a78']];
export const tagColor=key=>TAG_COLORS.find(([id])=>id===key)?.[2]||TAG_COLORS[0][2];
export const tagKey=value=>value.trim().normalize('NFC').toLocaleLowerCase();
const allAssets=state=>[...state.assets,...(state.trash||[]).map(item=>item.asset)];
/** ID 是关联权威；tags 名称镜像仅供旧卡片/搜索兼容，保存时统一刷新。 */
export function normalizeTags(state){
  const next=structuredClone(state);next.tagCatalog??=[];next.tagGroups??=[];
  for(const asset of allAssets(next)){
    if(!Array.isArray(asset.tagIds)){
      asset.tagIds=[];
      for(const raw of asset.tags||[]){
        const name=raw.trim();if(!name)continue;
        let tag=next.tagCatalog.find(t=>tagKey(t.name)===tagKey(name));
        if(!tag){tag={id:crypto.randomUUID(),name,color:'sage',groupId:null};next.tagCatalog.push(tag);}
        asset.tagIds.push(tag.id);
      }
    }
    asset.tagIds=[...new Set(asset.tagIds)].filter(id=>next.tagCatalog.some(tag=>tag.id===id));
    asset.tags=asset.tagIds.map(id=>next.tagCatalog.find(tag=>tag.id===id).name);
  }
  return next;
}
export function validateTag(state,{id,name,color='sage',groupId=null}){
  const clean=name.trim();if(!clean||clean.length>40)throw new Error('标签名称请填写 1–40 个字符。');
  if(state.tagCatalog.some(tag=>tag.id!==id&&tagKey(tag.name)===tagKey(clean)))throw new Error('已存在同名标签，请直接选择已有标签。');
  if(!TAG_COLORS.some(([key])=>key===color))throw new Error('请选择有效的标签颜色。');
  if(groupId&&!state.tagGroups.some(group=>group.id===groupId))throw new Error('这个分组已不存在，请重新选择。');
  return {id:id||crypto.randomUUID(),name:clean,color,groupId:groupId||null};
}
export function saveTag(state,input){
  const next=normalizeTags(state),tag=validateTag(next,input),index=next.tagCatalog.findIndex(t=>t.id===tag.id);
  if(index<0)next.tagCatalog.push(tag);else next.tagCatalog[index]=tag;
  return normalizeTags(next);
}
export function deleteTag(state,id){
  const next=normalizeTags(state);next.tagCatalog=next.tagCatalog.filter(tag=>tag.id!==id);
  return normalizeTags(next);
}
export function saveTagGroup(state,{id,name}){
  const next=normalizeTags(state),clean=name.trim();if(!clean||clean.length>40)throw new Error('分组名称请填写 1–40 个字符。');
  if(['全部标签','未分组'].includes(clean))throw new Error('这个名称留给系统视图，请换一个分组名称。');
  if(next.tagGroups.some(group=>group.id!==id&&tagKey(group.name)===tagKey(clean)))throw new Error('已存在同名分组。');
  const index=next.tagGroups.findIndex(group=>group.id===id),group={id:id||crypto.randomUUID(),name:clean};
  if(index<0)next.tagGroups.push(group);else next.tagGroups[index]=group;return next;
}
export function deleteTagGroup(state,id){
  const next=normalizeTags(state);next.tagGroups=next.tagGroups.filter(group=>group.id!==id);next.tagCatalog=next.tagCatalog.map(tag=>tag.groupId===id?{...tag,groupId:null}:tag);return next;
}
export function matchTagFilter(asset,ids,mode='all'){
  if(!ids.length)return true;const attached=new Set(asset.tagIds||[]);return mode==='any'?ids.some(id=>attached.has(id)):ids.every(id=>attached.has(id));
}
