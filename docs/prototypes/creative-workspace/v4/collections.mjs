export const FAVORITES = 'system-favorites';
export const UNFILED = 'system-unfiled';

export function collectionViews(state) {
  const classified = new Set(state.collections.flatMap(group => group.items));
  return [
    { id:FAVORITES, name:'我的最爱', system:true, items:state.favorites, description:'系统集合 · 不可删除' },
    { id:UNFILED, name:'未归类', system:true, items:state.assets.filter(a=>!classified.has(a.id)).map(a=>a.id), description:'自动汇集 · 尚未加入普通集合' },
    ...state.collections,
  ];
}

export function ordinaryCollection(state, id) {
  if ([FAVORITES,UNFILED].includes(id)) throw new Error('系统集合与自动视图不能重命名或删除。');
  const group=state.collections.find(g=>g.id===id);
  if(!group) throw new Error('这个集合已不存在，请返回集合页。');
  return group;
}

export function deleteCollection(state, id) {
  ordinaryCollection(state,id);
  const next=structuredClone(state);
  next.collections=next.collections.filter(g=>g.id!==id);
  return next;
}

export function renameCollection(state, id, name) {
  ordinaryCollection(state,id);
  if(!name.trim())throw new Error('请给集合一个名字。');
  const next=structuredClone(state);
  next.collections.find(g=>g.id===id).name=name.trim();
  return next;
}

export function removeMembers(state, id, ids) {
  if(id===UNFILED)throw new Error('未归类是自动视图，请将素材加入普通集合来归类。');
  const next=structuredClone(state),remove=new Set(ids);
  if(id===FAVORITES)next.favorites=next.favorites.filter(asset=>!remove.has(asset));
  else ordinaryCollection(next,id).items=ordinaryCollection(next,id).items.filter(asset=>!remove.has(asset));
  return next;
}
