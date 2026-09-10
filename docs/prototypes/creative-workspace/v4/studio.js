import {createGlassSelects} from './glass-select.mjs';
import {canvasSurface,createCanvasEmbed} from './canvas-embed.mjs';
import {normalizeTags,matchTagFilter,tagColor} from './tags.mjs';
import {createTagUI} from './tag-ui.mjs';
import { createCardMenu } from './card-menu.mjs';
import { createRecycleBin } from './recycle.mjs';
import { createCollector } from './collector.mjs';
import { pack } from './masonry.mjs?canvas-shelf=1';
import { createProjectWorkspace, projectDocument } from './projects.mjs';
import { FAVORITES, UNFILED, collectionViews, ordinaryCollection, deleteCollection, renameCollection, removeMembers } from './collections.mjs';

const $ = id => document.getElementById(id);
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[char]));
const paths = {
  'chevron-down':'<path d="m7 10 5 5 5-5"/>',
  'text-lines':'<path d="M5 5h14M5 10h14M5 15h9M5 20h6"/>',
  video:'<rect x="3" y="5" width="18" height="14" rx="3"/><path d="m10 9 5 3-5 3Z"/>',
  clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  check:'<path d="m5 12 4 4L19 6"/>',
  tag:'<path d="M20 13l-7 7-10-10V3h7l10 10Z"/><circle cx="7.5" cy="7.5" r="1"/>',
  edit:'<path d="m14 5 5 5M4 20l4-1L20 7l-5-5L3 14l1 6Z"/>',
  more:'<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
  trash:'<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5m4-5v5"/>',
  grid:'<rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="10" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="17" width="7" height="4" rx="1.5"/>',
  layers:'<rect x="6" y="3" width="15" height="15" rx="3"/><path d="M3 7v11a3 3 0 0 0 3 3h11"/>',
  folder:'<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/><path d="M3 11h18"/>',
  heart:'<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z"/>',
  search:'<circle cx="10.7" cy="10.7" r="6.7"/><path d="m16 16 4.5 4.5"/>',
  plus:'<path d="M12 5v14M5 12h14"/>', x:'<path d="m6 6 12 12M18 6 6 18"/>',
  sliders:'<path d="M4 7h7m4 0h5M4 17h3m4 0h9"/><circle cx="13" cy="7" r="2"/><circle cx="9" cy="17" r="2"/>',
  'check-square':'<rect x="3" y="3" width="18" height="18" rx="5"/><path d="m8 12 3 3 5-6"/>',
  'arrow-left':'<path d="M20 12H4m6-6-6 6 6 6"/>',
  'arrow-right':'<path d="M4 12h16m-6-6 6 6-6 6"/>',
  'arrow-up-right':'<path d="M6 18 18 6M6 6h12v12"/>',
  image:'<rect x="3" y="3" width="18" height="18" rx="4"/><circle cx="8" cy="8" r="1.5"/><path d="m3 16 5-4 4 4 4-6 5 6"/>',
  link:'<path d="m10 13 4-4m-6 7-2 2a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m4 0 2-2a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0" transform="translate(1 0)"/>',
  spark:'<path d="m12 2 2.5 7.5L22 12l-7.5 2.5L12 22l-2.5-7.5L2 12l7.5-2.5Z"/>',
};
const icon = name => `<svg viewBox="0 0 24 24" aria-hidden="true">${paths[name] || paths.image}</svg>`;
document.querySelectorAll('[data-icon]').forEach(el => { el.innerHTML = icon(el.dataset.icon); });

let state, db, visible = [], selection = new Set(), selecting = false, focusedAsset = null;
let imageObserver, layoutFrame = 0, toastTimer, discardAction = null;
let pickerKind = 'projects', pickerIds = [], composing = false, slow = false, createKind = 'collections', renameId = null, renameFromOverview = false, deleteId = null;
const openers = new Map();
const mediaTypes = {all:'全部类型',image:'图片',text:'文字',link:'链接'};
let route = readRoute();

function readRoute() {
  const p = new URLSearchParams(location.hash.slice(1));
  const legacyFavorite=p.get('view')==='saved';
  const view = legacyFavorite?'collections':['library','collections','projects','trash'].includes(p.get('view')) ? p.get('view') : 'library';
  const legacyType={文字:'text',链接:'link'}[p.get('category')];
  const mediaType=Object.hasOwn(mediaTypes,p.get('mediaType'))?p.get('mediaType'):(legacyType||'all');
  return { view, mediaType, tags:p.get('tags')||'',tagMode:p.get('tagMode')==='any'?'any':'all', q:p.get('q') || '', group:legacyFavorite?FAVORITES:p.get('group') || '', sort:p.get('sort') || 'recent', orientation:p.get('orientation') || 'all', size:p.get('size') || 'comfortable', pane:p.get('pane') || 'overview', section:p.get('section') || 'style' };
}
function navigate(patch, replace = false) {
  const next = { ...route, ...patch };
  delete next.category;
  const p = new URLSearchParams();
  for (const [k,v] of Object.entries(next)) if (v) p.set(k,v);
  if (replace) { history.replaceState(null,'',`#${p}`); route=readRoute(); render(); }
  else location.hash=p.toString();
}
window.addEventListener('hashchange', () => {
  route=readRoute(); selection.clear(); selecting=false; render();
  window.scrollTo({ top:0, behavior:'instant' });
});

function openDB() {
  return new Promise((resolve,reject) => {
    const req=indexedDB.open(canvasSurface?'creative-canvas-assets-v5':'creative-glass-prototype-v4',1);
    req.onupgradeneeded=()=>req.result.createObjectStore('workspace');
    req.onsuccess=()=>resolve(req.result);
    req.onerror=()=>reject(new Error('浏览器存储不可用，请允许此页面使用本地存储后重试。'));
  });
}
function loadState() {
  return new Promise((resolve,reject) => {
    const tx=db.transaction('workspace','readonly');
    const request=tx.objectStore('workspace').get('state');
    request.onsuccess=()=>resolve(request.result);
    request.onerror=()=>reject(new Error('读取本地灵感失败，请重试。'));
  });
}
async function commit(next) {
  next=normalizeTags(next);
  await new Promise((resolve,reject) => {
    const tx=db.transaction('workspace','readwrite');
    const store=tx.objectStore('workspace');
    const req=store.get('state');
    let message='本地保存失败，可能是存储空间不足。输入仍保留，请释放空间后重试。';
    req.onsuccess=()=>{
      if (req.result && req.result.revision !== state.revision) {
        message='另一个页面已更新灵感库。请保留当前输入，刷新后重试。'; tx.abort(); return;
      }
      next.revision=(state?.revision || 0)+1; store.put(next,'state');
    };
    tx.oncomplete=resolve;
    tx.onerror=()=>reject(new Error(message));
    tx.onabort=()=>reject(new Error(message));
  });
  state=next;
  canvasEmbed.sync();
}
function notify(message) {
  clearTimeout(toastTimer); $('toast').textContent=message; $('toast').classList.add('show');
  toastTimer=setTimeout(()=>$('toast').classList.remove('show'),3500);
}
function showWarning(error) { $('storage-warning').textContent=error.message; $('storage-warning').hidden=false; }
function freshState(photos) {
  const notes=[
    {id:'note-1',kind:'text',title:'关于光的一点念头',text:'让光停在脸的一侧，\n让故事留在另一侧。',category:'文字',tags:['布光','情绪'],width:4,height:4,note:'试试一盏灯，一块黑色背景。'},
    {id:'note-2',kind:'text',title:'拍摄前的小纸条',text:'不必让画面很满。\n风、光，和一个\n恰好的眼神。',category:'文字',tags:['留白','人像'],width:4,height:5,note:''},
    {id:'note-3',kind:'text',title:'下一次，去海边',text:'日落前四十分钟。\n白裙，低机位，\n等海风吹过来。',category:'文字',tags:['海边','拍摄想法'],width:4,height:3.7,note:''},
    {id:'link-1',kind:'link',title:'寻找光与建筑的关系',text:'去看建筑，\n也去看落在\n建筑里的光。',category:'链接',tags:['建筑','空间'],width:4,height:4,source:'https://unsplash.com/s/photos/tadao-ando',note:''},
  ];
  const all=[...photos.slice(0,5),notes[0],...photos.slice(5,9),notes[1],photos[9],notes[3],photos[10],notes[2],photos[11]];
  return {revision:0,assets:all.map((a,i)=>({...a,created:Date.now()-i*3600000})),favorites:['asset-1','asset-3','asset-9'],collections:[{id:'c-light',name:'光的形状',items:['asset-1','asset-3','asset-5','asset-6','asset-8','note-1']},{id:'c-sea',name:'把海风收藏',items:['asset-4','asset-7','asset-10','asset-11','note-3']},{id:'c-space',name:'空间里的秩序',items:['asset-2','asset-9','asset-12','link-1']}],projects:[{id:'p-sea',name:'九月，向海而行',items:['asset-4','asset-10','asset-12','note-3']},{id:'p-mono',name:'黑白肖像习作',items:['asset-1','asset-5','asset-6','note-1']}]};
}

async function boot() {
  $('page-error').hidden=true;
  try {
    db ||= await openDB();
    const saved=await loadState();
    if (saved){state=saved;const migrated=normalizeTags(saved);if(JSON.stringify(migrated)!==JSON.stringify(saved))await commit(migrated);}
    else {
      const response=await fetch('photos.json');
      if (!response.ok) throw new Error('示例素材未能加载，请检查本地服务后重试。');
      state=freshState(await response.json()); await commit(structuredClone(state));
    }
    await recycle.sweep();render();canvasEmbed.sync();
  } catch(error) { $('page-error-text').textContent=error.message; $('page-error').hidden=false; $('result-count').textContent='加载未完成'; }
}
$('retry-page').addEventListener('click',boot);

function referenceNote(asset){
  const project=route.view==='projects'?state.projects.find(p=>p.id===route.group):null;
  return project?projectWorkspace.referenceNote(project,asset.id):(asset.note||'');
}
function matches(asset) {
  const q=route.q.trim().toLocaleLowerCase();
  const haystack=[asset.title,asset.description,asset.source,asset.text,asset.note,referenceNote(asset),...asset.tags].join(' ').toLocaleLowerCase();
  const ratio=asset.width/asset.height;
  return matchTagFilter(asset,route.tags.split(',').filter(Boolean),route.tagMode) && (!q || q.split(/\s+/).every(word=>haystack.includes(word))) &&
    (route.mediaType==='all' || route.mediaType===asset.kind) &&
    (route.orientation==='all' || asset.kind==='image' && (route.orientation==='portrait' ? ratio<.95 : route.orientation==='landscape' ? ratio>1.05 : ratio>=.95&&ratio<=1.05));
}
function render() {
  if (!state) return;
  cardMenu.close();tagUI.renderFilters(route);
  const trashView=route.view==='trash';
  $('trash-panel').hidden=!trashView;$('trash-open').hidden=route.view!=='library';$('trash-badge').textContent=String(state.trash?.length||0);
  imageObserver?.disconnect();
  $('search').value=route.q; $('clear-search').hidden=!route.q;
  $('sort').value=route.sort; $('orientation').value=route.orientation; $('size').value=route.size;
  $('library-count').textContent=`${state.assets.length} 份灵感，${state.collections.length} 个自建集合`;
  document.querySelectorAll('[data-nav]').forEach(el=>{
    const active=el.dataset.nav===(trashView?'library':route.view);
    el.classList.toggle('active',active); if(active) el.setAttribute('aria-current','page'); else el.removeAttribute('aria-current');
  });
  $('media-type').value=route.mediaType;glassSelects.sync();const tagCount=route.tags.split(',').filter(Boolean).length;$('tag-filter-number').hidden=!tagCount;$('tag-filter-number').textContent=String(tagCount);$('tag-filter-open').classList.toggle('has-tags',tagCount>0);
  const groupView=['collections','projects'].includes(route.view);
  const group=groupView ? (route.view==='collections'?collectionViews(state):state.projects).find(g=>g.id===route.group) : null;
  const overview=groupView&&!route.group;
  const projectView=route.view==='projects'&&Boolean(group);
  $('project-workbench').hidden=!projectView;
  document.body.classList.toggle('project-mode',projectView);
  if(projectView)$('library-count').textContent=`${group.items.length} 份参考，${projectDocument(group,state.assets).shots.length} 个分镜`;
  $('search-form').hidden=trashView||projectView&&route.pane!=='references';
  $('bulk-shot').hidden=!projectView||route.pane!=='references';
  $('bulk-project').hidden=projectView;
  document.querySelector('.results-line').hidden=projectView&&route.pane!=='references';
  $('back').hidden=!route.group&&!trashView; $('create-group').hidden=!overview;
  $('collection-actions').hidden=!(route.view==='collections'&&group&&!group.system);
  $('bulk-remove').hidden=!(route.view==='collections'&&group&&group.id!==UNFILED);
  $('bulk-remove').textContent=route.group===FAVORITES?'移出我的最爱':'移出集合';
  $('create-group').innerHTML=icon('plus')+(route.view==='projects'?'新建项目':'新建集合');
  $('eyebrow').textContent=group ? (route.view==='projects'?'A WORK IN PROGRESS':'A PERSONAL COLLECTION') : ({library:'YOUR PRIVATE COLLECTION',collections:'COLLECTED WITH INTENTION',projects:'FROM INSPIRATION TO CREATION',trash:'A SECOND CHANCE'}[route.view]);
  $('page-title').textContent=group?.name || ({library:'灵感，慢慢成形。',collections:'把喜欢，放在一起。',projects:'让想法，成为画面。',trash:'回收站'}[route.view]);
  $('page-description').textContent=group ? (route.view==='projects'?'为这一次拍摄，留下真正想表达的。':'围绕一个念头，慢慢积攒它的样子。') : ({library:'把喜欢的光线、色彩与瞬间，留给下一次创作。',collections:'用自己的方式，整理那些相互呼应的灵感。',projects:'一次拍摄，一段关于光与人的故事。',trash:'暂时放下，也留一次找回的机会。'}[route.view]);
  if(group?.id===FAVORITES)$('page-description').textContent='那些想再看一眼的画面。系统集合，始终为你保留。';
  if(group?.id===UNFILED)$('page-description').textContent='还没有加入普通集合的素材。标记最爱或用于项目，都不改变这里的归类。';
  if(groupView&&route.group&&!group){$('page-title').textContent='这个集合或项目已不存在';$('page-description').textContent='素材仍保留在灵感库，可以返回列表继续浏览。';}
  document.title=`${group?.name || {library:'灵感库',collections:'灵感集合',projects:'创作项目',trash:'回收站'}[route.view]} · 创意空间`;
  document.querySelector('.toolbar').hidden=trashView||overview||(projectView&&route.pane!=='references');
  if(trashView||overview||(projectView&&route.pane!=='references')) $('filter-panel').hidden=true;
  $('tag-filter-bar').hidden=!route.tags||trashView||overview||(projectView&&route.pane!=='references');$('tag-manage-open').hidden=trashView||overview||(projectView&&route.pane!=='references');
  $('gallery').hidden=trashView||overview||(projectView&&route.pane!=='references'); $('group-grid').hidden=!overview; $('empty').hidden=true;
  if(trashView){$('filter-panel').hidden=true;document.querySelector('.results-line').hidden=true;$('gallery').innerHTML='';visible=[];selection.clear();syncSelection();recycle.render(state);if(canvasSurface)glassSelects.sync();return;}
  if(projectView){projectWorkspace.render(route,group,state.assets);if(route.pane!=='references'){visible=[];$('gallery').innerHTML='';selection.clear();syncSelection();return;}}
  if(overview) renderGroups();
  else {
    let assets=state.assets;
    if(route.group) assets=group ? assets.filter(a=>group.items.includes(a.id)) : [];
    visible=assets.filter(matches).sort((a,b)=>route.sort==='name'?a.title.localeCompare(b.title,'zh-CN'):route.sort==='oldest'?a.created-b.created:b.created-a.created);
    selection=new Set([...selection].filter(id=>visible.some(asset=>asset.id===id)));
    tagUI.updateResults(visible.length);
    $('result-count').textContent=`${route.q ? '找到 ' : '共 '}${visible.length} 份灵感${route.group?' · 参考素材':''}`;
    $('gallery').innerHTML=visible.map(renderCard).join('');
    layout(); observeImages();
    if(!visible.length) showEmpty(Boolean(route.q || route.tags || route.mediaType!=='all' || route.orientation!=='all'));
  }
  syncSelection();
}
function showEmpty(filtered) {
  $('empty').hidden=false;
  $('empty-title').textContent=filtered?'还没有找到这份灵感':route.group===FAVORITES?'把心动的画面留在这里':'这里，留给新的灵感';
  $('empty-copy').textContent=filtered?'换个关键词，或放宽一点筛选条件。':route.group?'从素材库选一些喜欢的画面，加入这里。':'去素材库逛逛，或收集一份新的灵感。';
  if(!filtered&&route.group===UNFILED){$('empty-title').textContent='每份灵感，都找到了归处';$('empty-copy').textContent='新收集、还未加入普通集合的素材会自动出现在这里。';}
  $('empty-action').textContent=filtered?'清除筛选':'浏览素材库';
  $('empty-action').dataset.filtered=String(filtered);
}
function renderCard(asset) {
  const title=escapeHTML(asset.title), selected=selection.has(asset.id), favorite=state.favorites.includes(asset.id);
  const picture=asset.kind==='image' ? `<img data-src="${escapeHTML(asset.src)}" width="${asset.width}" height="${asset.height}" alt="${title}" decoding="async" loading="lazy">` : `<span class="note-symbol">${asset.kind==='link'?'↗':'“'}</span><span class="note-content">${escapeHTML(asset.text).replace(/\n/g,'<br>')}</span><span class="note-label">${asset.kind==='link'?'LINK / 外部灵感':'A NOTE TO SELF / 灵感手记'}</span>`;
  return `<article class="asset-card ${selected?'selected':''}" data-id="${asset.id}" ${canvasSurface?'draggable="true"':''}><div class="asset-media ${asset.kind==='image'?'loading':`text-card ${asset.kind==='link'?'link-card':''}`}" style="--aspect:${asset.width}/${asset.height}" ${asset.kind==='image'?'aria-busy="true"':''}><button class="asset-open" data-open="${asset.id}" aria-label="查看：${title}">${picture}</button>${canvasSurface?`<button class="canvas-add" type="button" data-canvas-add="${asset.id}" aria-label="放入画布：${title}" title="放入画布">${icon('plus')}</button>`:''}<div class="card-actions"><label class="card-check"><input type="checkbox" data-select="${asset.id}" aria-label="选择：${title}" ${selected?'checked':''}></label><button class="icon-button ${favorite?'heart-on':''}" data-favorite="${asset.id}" aria-label="${favorite?'移出':'加入'}我的最爱：${title}" aria-pressed="${favorite}">${icon('heart')}</button></div></div><div class="asset-meta"><div><h3 title="${title}">${title}</h3><p>${(asset.tagIds||[]).slice(0,2).map(id=>{const tag=tagUI.tagFor(id);return tag?`<span class="card-tag"><i style="--tag-color:${tagColor(tag.color)}"></i>${escapeHTML(tag.name)}</span>`:'';}).join('')}</p></div><button class="icon-button asset-menu-trigger" type="button" data-asset-menu="${asset.id}" aria-label="更多操作：${title}" title="更多操作" aria-haspopup="menu" aria-expanded="false" aria-controls="asset-menu">${icon('more')}</button></div></article>`;
}
function renderGroups() {
  const groups=(route.view==='collections'?collectionViews(state):state.projects).filter(g=>g.name.toLocaleLowerCase().includes(route.q.toLocaleLowerCase()));
  $('result-count').textContent=route.view==='projects'?`${groups.length} 个项目`:`${groups.filter(g=>!g.system).length} 个自建集合 · ${groups.filter(g=>g.system).length} 个固定入口`;
  $('group-grid').classList.toggle('project-list-grid',route.view==='projects');
  $('group-grid').innerHTML=groups.map(group=>{
    const members=group.items.map(id=>state.assets.find(a=>a.id===id)).filter(Boolean);
    const images=members.filter(a=>a.kind==='image').slice(0,3);
    const textPreview=members.find(a=>a.kind==='text');
    if(route.view==='projects'){
      const doc=projectDocument(group,state.assets);
      return `<button class="project-list-card glass" data-group="${group.id}"><div class="project-list-cover">${images[0]?`<img src="${escapeHTML(images[0].src)}" alt="" loading="lazy">`:`<span>${icon('folder')}</span>`}<span class="project-list-label">创作项目</span></div><section><h2>${escapeHTML(group.name)}</h2><p>${escapeHTML(doc.brief||'还没有写下创作意图。先留一个念头，再慢慢找到画面。')}</p><div class="project-card-facts"><span>${group.items.length} 份参考</span><span>${doc.shots.length} 个分镜</span><span>${group.updatedAt?'已在本地编辑':'本地原型'}</span></div><div class="project-card-enter">进入创作工作台 ${icon('arrow-right')}</div></section></button>`;
    }
    return `<article class="group-card ${group.system?'system-group':''}" data-collection-card="${group.id}"><button class="group-open" data-group="${group.id}"><div class="group-cover">${group.system?`<span class="system-badge">${icon(group.id===FAVORITES?'heart':'layers')}</span>`:''}${images.length?images.map(a=>`<img src="${escapeHTML(a.src)}" alt="" loading="lazy" width="${a.width}" height="${a.height}">`).join(''):textPreview?`<span class="group-note-preview">${escapeHTML(textPreview.text)}</span>`:`<span class="group-empty">${icon(group.id===FAVORITES?'heart':'layers')}</span>`}</div><h2>${escapeHTML(group.name)}</h2><p>${group.items.length} 份灵感<span aria-hidden="true"> · </span>${escapeHTML(group.description || (route.view==='projects'?'创作中的片段':'私人集合'))}</p></button>${group.system?'':`<button type="button" class="icon-button collection-menu-trigger" data-collection-menu="${group.id}" aria-label="集合操作：${escapeHTML(group.name)}" title="集合操作" aria-haspopup="menu" aria-expanded="false" aria-controls="asset-menu">${icon('more')}</button>`}</article>`;
  }).join('');
  if(!groups.length) showEmpty(Boolean(route.q));
}
function layout() {
  if($('gallery').hidden || !$('gallery').clientWidth) return;
  const result=pack(visible,$('gallery').clientWidth,{compact:route.size==='compact',mobile:matchMedia('(max-width:760px)').matches,minimumMobileWidth:canvasSurface?100:140});
  $('gallery').style.height=`${result.height}px`;
  $('gallery').dataset.columns=result.columns;
  [...$('gallery').children].forEach((el,i)=>{
    const p=result.positions[i]; if(!p) return;
    el.style.width=`${p.width}px`; el.style.transform=`translate(${p.x}px,${p.y}px)`;
  });
}
let previousWidth=0;
new ResizeObserver(entries=>{
  const width=entries[0].contentRect.width;
  if(width===previousWidth) return;
  previousWidth=width; cancelAnimationFrame(layoutFrame); layoutFrame=requestAnimationFrame(layout);
}).observe($('gallery'));
window.addEventListener('resize',()=>{cancelAnimationFrame(layoutFrame);layoutFrame=requestAnimationFrame(layout);});
function observeImages() {
  const images=$('gallery').querySelectorAll('img[data-src]');
  if(!('IntersectionObserver' in window)) { images.forEach(loadImage); return; }
  imageObserver=new IntersectionObserver(entries=>{
    entries.forEach(entry=>{if(entry.isIntersecting){imageObserver.unobserve(entry.target);loadImage(entry.target);}});
  },{rootMargin:'180px 0px',threshold:0.01});
  images.forEach(img=>imageObserver.observe(img));
}
function loadImage(img) {
  if(!img.isConnected) return;
  const media=img.closest('.asset-media');
  media.querySelector('.image-error')?.remove(); media.classList.add('loading'); media.classList.remove('loaded'); media.setAttribute('aria-busy','true');
  let watchdog;
  const failure=()=>{
    clearTimeout(watchdog); media.classList.remove('loading'); media.setAttribute('aria-busy','false');
    if(media.querySelector('.image-error')) return;
    const error=document.createElement('div'); error.className='image-error';
    error.innerHTML=icon('image')+'<span>图片暂未加载</span><button type="button">重新加载</button>';
    error.querySelector('button').addEventListener('click',()=>{img.removeAttribute('src');loadImage(img);});
    media.append(error);
  };
  img.onload=()=>{clearTimeout(watchdog);media.querySelector('.image-error')?.remove();media.classList.remove('loading');media.classList.add('loaded');media.setAttribute('aria-busy','false');};
  img.onerror=failure;
  const begin=()=>{if(!img.isConnected)return;img.src=img.dataset.src;watchdog=setTimeout(failure,12000);};
  if(slow) setTimeout(begin,1200); else begin();
}

function syncSelection() {
  $('gallery').classList.toggle('selecting',selecting || selection.size>0);
  $('selection-toggle').setAttribute('aria-pressed',String(selecting));
  $('selection-bar').hidden=selection.size===0; $('selection-count').textContent=`已选 ${selection.size} 项`;
  $('gallery').querySelectorAll('[data-select]').forEach(input=>{
    input.checked=selection.has(input.dataset.select); input.closest('.asset-card').classList.toggle('selected',input.checked);
  });
}
function toggleSelect(id) { if(selection.has(id))selection.delete(id);else selection.add(id);syncSelection(); }
async function toggleFavorite(id,button) {
  button.disabled=true;
  try {
    const next=structuredClone(state), has=next.favorites.includes(id);
    next.favorites=has?next.favorites.filter(x=>x!==id):[...next.favorites,id]; await commit(next);
    const on=!has;
    document.querySelectorAll(`[data-favorite="${id}"]`).forEach(el=>{el.classList.toggle('heart-on',on);el.setAttribute('aria-pressed',String(on));el.setAttribute('aria-label',`${on?'移出':'加入'}我的最爱：${state.assets.find(a=>a.id===id).title}`);});
    if(focusedAsset===id){$('detail-favorite').classList.toggle('heart-on',on);$('detail-favorite').setAttribute('aria-pressed',String(on));$('detail-favorite').setAttribute('aria-label',on?'移出我的最爱':'加入我的最爱');}
    notify(on?'已放入我的最爱':'已移出我的最爱');
    if(route.view==='collections'&&route.group===FAVORITES) { selection.delete(id); render(); }
  }catch(error){showWarning(error);notify(error.message);}finally{button.disabled=false;}
}
$('gallery').addEventListener('click',e=>{
  const canvasAdd=e.target.closest('[data-canvas-add]');if(canvasAdd){canvasEmbed.add([canvasAdd.dataset.canvasAdd]);return;}
  const favorite=e.target.closest('[data-favorite]'); if(favorite){void toggleFavorite(favorite.dataset.favorite,favorite);return;}
  const open=e.target.closest('[data-open]'); if(open){if(selecting)toggleSelect(open.dataset.open);else showDetail(open.dataset.open);}
});
$('gallery').addEventListener('change',e=>{if(e.target.matches('[data-select]'))toggleSelect(e.target.dataset.select);});
$('media-type').addEventListener('change',()=>navigate({mediaType:$('media-type').value,orientation:$('media-type').value==='image'?route.orientation:'all'},true));
$('selection-toggle').addEventListener('click',()=>{selecting=!selecting;if(!selecting)selection.clear();syncSelection();});
$('clear-selection').addEventListener('click',()=>{selection.clear();selecting=false;syncSelection();});
$('sort').addEventListener('change',e=>navigate({sort:e.target.value}));
$('orientation').addEventListener('change',e=>navigate({orientation:e.target.value}));
$('size').addEventListener('change',e=>navigate({size:e.target.value}));
$('filter-toggle').addEventListener('click',()=>{const open=$('filter-panel').hidden;$('filter-panel').hidden=!open;$('filter-toggle').setAttribute('aria-expanded',String(open));});
function resetFilters(){navigate({tags:'',q:'',mediaType:'all',orientation:'all'});}
$('reset-filters').addEventListener('click',resetFilters);
$('empty-action').addEventListener('click',()=>{$('empty-action').dataset.filtered==='true'?resetFilters():navigate({view:'library',group:'',q:'',mediaType:'all',orientation:'all'});});
$('clear-search').addEventListener('click',()=>{navigate({q:''},true);$('search').focus();});
$('search').addEventListener('compositionstart',()=>{composing=true;});
$('search').addEventListener('compositionend',()=>{composing=false;navigate({q:$('search').value},true);});
$('search').addEventListener('input',()=>{if(!composing)navigate({q:$('search').value},true);});
$('search-form').addEventListener('submit',e=>{e.preventDefault();if(!composing)navigate({q:$('search').value},true);});
$('back').addEventListener('click',()=>navigate({view:route.view==='trash'?'library':route.view,group:'',q:'',mediaType:'all',orientation:'all'}));
$('group-grid').addEventListener('click',e=>{const group=e.target.closest('[data-group]');if(group)navigate({group:group.dataset.group,q:'',mediaType:'all'});});

function openModal(id) { const dialog=$(id);openers.set(id,document.activeElement);if(!dialog.open)dialog.showModal(); }
function closeModal(id) {
  $(id).close(); const opener=openers.get(id);
  if(opener?.isConnected) opener.focus({preventScroll:true});
  else if(id==='detail') document.querySelector(`[data-open="${focusedAsset}"]`)?.focus({preventScroll:true});
  if(id==='import-dialog')collector.reset();
  if(id==='about'&&state)render();
}
function dirty(id) {
  if(id==='detail')return $('detail-note').value!==referenceNote(state.assets.find(a=>a.id===focusedAsset));
  if(id.startsWith('tag-'))return tagUI.isDirty(id);
  if(id==='project-editor')return projectWorkspace.isDirty();
  if(id==='import-dialog')return collector.isDirty();
  if(id==='create-dialog')return $('group-name').value !== (renameId ? state.collections.find(g=>g.id===renameId)?.name || '' : '');
  return false;
}
function guard(id,fn,message='关闭后，本次未保存的输入会被丢弃。') {if(dirty(id)){$('discard-description').textContent=message;discardAction=fn;openModal('discard-dialog');}else fn();}
function requestClose(id){if(id.startsWith('tag-')&&tagUI.isBusy())return;if(id==='asset-delete-dialog'&&recycle.isBusy())return;if(id==='remove-shot-dialog'&&$('remove-shot-confirm').disabled)return;if(id==='project-editor'&&$('project-editor-save').disabled){notify('正在保存，请稍候。');return;}if(id==='delete-dialog'&&$('confirm-delete-collection').disabled)return;if(id==='import-dialog'&&$('import-submit').disabled){notify('正在保存，请稍候。');return;}guard(id,()=>closeModal(id));}
document.querySelectorAll('[data-close]').forEach(button=>button.addEventListener('click',()=>requestClose(button.dataset.close)));
document.querySelectorAll('dialog').forEach(dialog=>{
  dialog.addEventListener('cancel',e=>{e.preventDefault();if(dialog.id==='discard-dialog')closeModal('discard-dialog');else requestClose(dialog.id);});
  dialog.addEventListener('keydown',e=>{
    if(e.key!=='Tab')return;
    const controls=[...dialog.querySelectorAll('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled),a[href],[tabindex="0"]')].filter(el=>el.getClientRects().length>0);
    const first=controls[0],last=controls.at(-1);if(!first){e.preventDefault();dialog.focus();return;}
    if(e.shiftKey&&(document.activeElement===first||document.activeElement===dialog)){e.preventDefault();last.focus();}
    else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus();}
  });
});
$('keep-editing').addEventListener('click',()=>{discardAction=null;closeModal('discard-dialog');});
$('discard').addEventListener('click',()=>{const fn=discardAction;discardAction=null;closeModal('discard-dialog');fn?.();});
window.addEventListener('beforeunload',e=>{if(['detail','import-dialog','create-dialog','project-editor','tag-editor','tag-group-editor'].some(id=>$(id).open&&dirty(id))){e.preventDefault();e.returnValue='';}});

function showDetail(id) {
  const asset=state.assets.find(a=>a.id===id);if(!asset)return;
  focusedAsset=id;
  $('detail-title').textContent=asset.title;
  $('detail-type').textContent=asset.kind==='image'?'影像参考 / VISUAL REFERENCE':asset.kind==='text'?'灵感手记 / A NOTE':'外部灵感 / LINK';
  $('detail-remove').hidden=!(route.view==='collections'&&route.group&&!([FAVORITES,UNFILED].includes(route.group))&&state.collections.some(g=>g.id===route.group&&g.items.includes(id)));
  $('detail-dimensions').textContent=asset.kind==='image'?`${asset.width} × ${asset.height} · 原始比例 · ${mediaTypes[asset.kind]||'素材'}`:`${mediaTypes[asset.kind]||'素材'} · 仅自己可见`;
  $('detail-image').innerHTML=asset.kind==='image'?`<img src="${escapeHTML(asset.src)}" alt="${escapeHTML(asset.title)}" width="${asset.width}" height="${asset.height}">`:`<p class="large-note">${escapeHTML(asset.text).replace(/\n/g,'<br>')}</p>`;
  $('detail-description').hidden=asset.kind!=='image'||!asset.description;$('detail-description').textContent=asset.description||'';
  const detailImg=$('detail-image').querySelector('img');
  if(detailImg) detailImg.addEventListener('error',()=>{const p=document.createElement('p');p.textContent='图片暂未加载，请关闭后重试。';detailImg.replaceWith(p);});
  $('detail-tags').innerHTML=(asset.tagIds||[]).map(id=>{const tag=tagUI.tagFor(id);return tag?`<button class="tag-chip" data-tag="${id}">${tagUI.chip(tag)}</button>`:'';}).join('');
  document.querySelector('label[for="detail-note"]').textContent=route.view==='projects'?'本次拍摄参考什么':'为什么留下这份灵感';
  $('detail-note').value=referenceNote(asset);$('note-status').textContent=referenceNote(asset)?'已保存在此浏览器':'';
  const projects=state.projects.filter(p=>p.items.includes(id));
  $('detail-usage').textContent=projects.length?`已用于：${projects.map(p=>p.name).join('、')}`:'这份灵感，还没有加入拍摄项目。';
  $('detail-source').hidden=!asset.source;
  if(asset.source)$('detail-source').href=asset.source;else $('detail-source').removeAttribute('href');
  const on=state.favorites.includes(id);$('detail-favorite').classList.toggle('heart-on',on);$('detail-favorite').setAttribute('aria-pressed',String(on));$('detail-favorite').setAttribute('aria-label',on?'移出我的最爱':'加入我的最爱');
  const index=visible.findIndex(a=>a.id===id);$('detail-prev').disabled=index<=0;$('detail-next').disabled=index<0||index>=visible.length-1;
  if(!$('detail').open)openModal('detail');
}
$('detail-prev').addEventListener('click',()=>detailStep(-1));$('detail-next').addEventListener('click',()=>detailStep(1));
function detailStep(delta){const index=visible.findIndex(a=>a.id===focusedAsset),asset=visible[index+delta];if(asset)guard('detail',()=>showDetail(asset.id));}
$('detail-favorite').addEventListener('click',()=>void toggleFavorite(focusedAsset,$('detail-favorite')));
$('detail-tags').addEventListener('click',e=>{const button=e.target.closest('[data-tag]');if(button)guard('detail',()=>{closeModal('detail');navigate({q:'',tags:button.dataset.tag,mediaType:'all',orientation:'all'});});});
$('detail-note').addEventListener('input',()=>{$('note-status').textContent=dirty('detail')?'尚未保存':'已保存';});
$('save-note').addEventListener('click',async()=>{
  const button=$('save-note');button.disabled=true;
  try{if(route.view==='projects'&&route.group){await projectWorkspace.saveReferenceNote(route.group,focusedAsset,$('detail-note').value);}else{const next=structuredClone(state);next.assets.find(a=>a.id===focusedAsset).note=$('detail-note').value;await commit(next);}$('note-status').textContent='已保存在此浏览器';notify('这段想法，已留下');}
  catch(error){$('note-status').textContent=error.message;}finally{button.disabled=false;}
});

function showPicker(kind,ids) {
  pickerKind=kind;pickerIds=[...ids];const word=kind==='projects'?'项目':'集合';
  $('picker-title').textContent=`加入${word}`;$('picker-description').textContent=`选中的 ${pickerIds.length} 份灵感可以加入多个${word}，原素材会保留。`;
  $('picker-options').innerHTML=(kind==='collections'?collectionViews(state).filter(g=>g.id!==UNFILED):state.projects).map(group=>{
    const complete=pickerIds.every(id=>group.items.includes(id));
    return `<label class="picker-option"><input type="checkbox" name="group" value="${group.id}" ${complete?'checked disabled':''}><span>${escapeHTML(group.name)}<small>${complete?'所选素材已全部加入':`${group.items.length} 份灵感`}</small></span></label>`;
  }).join('') || `<p>还没有${word}，先到${word}页创建一个。</p>`;
  $('picker-error').textContent='';$('picker-submit').textContent=`确认加入${word}`;openModal('picker');
}
$('detail-add').addEventListener('click',()=>canvasSurface?canvasEmbed.add([focusedAsset]):showPicker('projects',[focusedAsset]));
$('detail-collection').addEventListener('click',()=>showPicker('collections',[focusedAsset]));
$('bulk-project').addEventListener('click',()=>canvasSurface?canvasEmbed.add(selection):showPicker('projects',selection));
$('bulk-collection').addEventListener('click',()=>showPicker('collections',selection));
$('picker-form').addEventListener('submit',async e=>{
  e.preventDefault();const checked=[...$('picker-options').querySelectorAll('input:checked:not(:disabled)')].map(el=>el.value);
  if(!checked.length){$('picker-error').textContent='请选择一个尚未加入的目标。';$('picker-options').querySelector('input:not(:disabled)')?.focus();return;}
  const button=$('picker-submit');button.disabled=true;
  try{
    const next=structuredClone(state);next[pickerKind].forEach(group=>{if(checked.includes(group.id))group.items=[...new Set([...group.items,...pickerIds])];});
    if(pickerKind==='collections'&&checked.includes(FAVORITES))next.favorites=[...new Set([...next.favorites,...pickerIds])];
    await commit(next);closeModal('picker');render();if($('detail').open){const note=$('detail-note').value;showDetail(focusedAsset);$('detail-note').value=note;$('note-status').textContent=dirty('detail')?'尚未保存':'已保存';}notify(`已加入 ${checked.length} 个${pickerKind==='projects'?'项目':'集合'}`);
  }catch(error){$('picker-error').textContent=error.message;}finally{button.disabled=false;}
});

$('create-group').addEventListener('click',()=>{renameId=null;$('create-submit').textContent='创建';createKind=route.view;$('create-title').textContent=createKind==='projects'?'开始一次新的创作':'新的灵感集合';$('create-form').reset();$('create-error').textContent='';openModal('create-dialog');$('group-name').focus();});
$('create-form').addEventListener('submit',async e=>{
  e.preventDefault();const name=$('group-name').value.trim();if(!name){$('create-error').textContent='给它一个名字，方便下次找到。';$('group-name').setAttribute('aria-invalid','true');$('group-name').setAttribute('aria-describedby','create-error');$('group-name').focus();return;}
  const button=e.submitter;button.disabled=true;
  try{const id=renameId||crypto.randomUUID();const next=renameId?renameCollection(state,renameId,name):structuredClone(state);if(!renameId)next[createKind].push({id,name,items:[]});await commit(next);const renamed=Boolean(renameId);$('group-name').value='';closeModal('create-dialog');if(renamed&&renameFromOverview){render();(document.querySelector(`[data-collection-menu="${id}"]`)||$('create-group')).focus({preventScroll:true});}else{navigate({view:createKind,group:id,q:''});render();}notify(renamed?'集合名称已更新':'新的创作空间，已准备好');}catch(error){$('create-error').textContent=error.message;}finally{button.disabled=false;}
});
$('group-name').addEventListener('input',()=>{$('group-name').removeAttribute('aria-invalid');$('create-error').textContent='';});

function openCollectionRename(id){
  try{
    const group=ordinaryCollection(state,id);renameFromOverview=route.view==='collections'&&!route.group;renameId=group.id;createKind='collections';
    $('create-title').textContent='为集合换个名字';$('create-submit').textContent='保存名称';
    $('group-name').value=group.name;$('group-name').removeAttribute('aria-invalid');$('create-error').textContent='';
    openModal('create-dialog');$('group-name').focus();$('group-name').select();
  }catch(error){notify(error.message);}
}
$('rename-collection').addEventListener('click',()=>openCollectionRename(route.group));
function openCollectionDelete(id){
  try{
    const group=ordinaryCollection(state,id);deleteId=group.id;
    $('delete-title').textContent=`删除「${group.name}」？`;
    $('delete-description').textContent=`集合里的 ${group.items.length} 份素材仍会保留在灵感库。我的最爱、其他集合和项目不受影响；没有其他普通集合归属的素材会回到「未归类」。`;
    $('delete-error').textContent='';openModal('delete-dialog');
  }catch(error){notify(error.message);}
}
$('delete-collection').addEventListener('click',()=>openCollectionDelete(route.group));
$('confirm-delete-collection').addEventListener('click',async()=>{
  const button=$('confirm-delete-collection');if(button.disabled)return;button.disabled=true;
  try{
    const next=deleteCollection(state,deleteId);await commit(next);closeModal('delete-dialog');
    selection.clear();navigate({view:'collections',group:'',q:'',mediaType:'all',orientation:'all'},true);$('create-group').focus({preventScroll:true});
    notify('集合已删除，素材仍保留在灵感库');
  }catch(error){$('delete-error').textContent=error.message;}finally{button.disabled=false;}
});
async function removeFromCurrentCollection(ids,closeDetail=false){
  const groupId=route.group;
  if(route.view!=='collections'||!groupId)return;
  $('bulk-remove').disabled=true;$('detail-remove').disabled=true;
  try{
    await commit(removeMembers(state,groupId,ids));
    if(closeDetail)closeModal('detail');selection.clear();render();
    notify(groupId===FAVORITES?'已移出我的最爱，素材仍保留':'已移出集合，素材仍保留在灵感库');
  }catch(error){showWarning(error);notify(error.message);}finally{$('bulk-remove').disabled=false;$('detail-remove').disabled=false;}
}
$('bulk-remove').addEventListener('click',()=>void removeFromCurrentCollection([...selection]));
$('detail-remove').addEventListener('click',()=>guard('detail',()=>void removeFromCurrentCollection([focusedAsset],true)));

function readImage(file) {
  return new Promise((resolve,reject)=>{
    const reader=new FileReader();reader.onerror=()=>reject(new Error(`${file.name} 读取失败，请重新选择。`));
    reader.onload=()=>{const img=new Image();img.onload=()=>resolve({src:reader.result,width:img.naturalWidth,height:img.naturalHeight});img.onerror=()=>reject(new Error(`${file.name} 无法解码，请检查图片文件。`));img.src=reader.result;};reader.readAsDataURL(file);
  });
}
let dragDepth=0;
document.addEventListener('dragenter',e=>{if([...e.dataTransfer.types].includes('Files')){e.preventDefault();dragDepth++;$('drop-overlay').hidden=false;}});
document.addEventListener('dragover',e=>{if([...e.dataTransfer.types].includes('Files'))e.preventDefault();});
document.addEventListener('dragleave',()=>{dragDepth=Math.max(0,dragDepth-1);if(!dragDepth)$('drop-overlay').hidden=true;});
document.addEventListener('drop',e=>{
  if(![...e.dataTransfer.types].includes('Files'))return;e.preventDefault();dragDepth=0;$('drop-overlay').hidden=true;
  if(document.querySelector('dialog[open]:not(#import-dialog)')){notify('请先关闭当前窗口，再收集图片');return;}
  collector.receiveFiles([...e.dataTransfer.files]);
});
$('about-open').addEventListener('click',()=>openModal('about'));
document.querySelector('[data-about]').addEventListener('click',()=>openModal('about'));
$('slow-mode').addEventListener('change',e=>{slow=e.target.checked;notify(slow?'已开启慢速演示，关闭说明后即可查看':'已恢复正常加载');});
document.addEventListener('keydown',e=>{
  const typing=e.target.matches('input,textarea,select,[contenteditable]');
  if(e.isComposing||composing)return;
  if(e.key==='/'&&!typing&&!document.querySelector('dialog[open]')){e.preventDefault();$('search').focus();}
  if($('detail').open&&!typing&&!$('picker').open&&!$('discard-dialog').open){if(e.key==='ArrowLeft')detailStep(-1);if(e.key==='ArrowRight')detailStep(1);}
});
window.addEventListener('offline',()=>notify('当前离线，本地收藏仍可使用；未加载的图片可以稍后重试。'));
const glassSelects=createGlassSelects({ids:['media-type','sort',...(canvasSurface?['tag-match-mode','orientation','size','trash-retention']:[])],icon,beforeOpen:()=>{tagUI.closeDropdown();cardMenu.close();}});
const tagUI=createTagUI({getState:()=>state,getRoute:()=>route,getResultCount:()=>visible.length,commit,notify,navigate,openModal,closeModal,render,escapeHTML,icon,refreshDetail:showDetail});
$('detail-edit-tags').addEventListener('click',()=>guard('detail',()=>tagUI.editAssetTags(focusedAsset)));
const cardMenu=createCardMenu({getState:()=>state,icon,projectLabel:canvasSurface?'放入画布':'加入项目',run:(action,id,trigger)=>{
  if(action==='collection-open')navigate({view:'collections',group:id,q:''});
  else if(action==='collection-rename')openCollectionRename(id);
  else if(action==='collection-delete')openCollectionDelete(id);
  else if(action==='detail')showDetail(id);
  else if(action==='favorite')void toggleFavorite(id,trigger);
  else if(action==='projects'&&canvasSurface)canvasEmbed.add([id]);
  else if(action==='collections'||action==='projects')showPicker(action,[id]);
  else if(action==='trash')recycle.move([id]);
}});
const recycle=createRecycleBin({getState:()=>state,commit,notify,navigate,openModal,closeModal,render,escapeHTML,icon});
$('detail-trash').addEventListener('click',()=>guard('detail',()=>recycle.move([focusedAsset])));
$('bulk-trash').addEventListener('click',()=>recycle.move([...selection]));
const collector=createCollector({getState:()=>state,getProjectId:()=>route.view==='projects'&&route.group?route.group:null,commit,notify,navigate,openModal,closeModal,guard,render,escapeHTML,icon,readImage,tagUI});
const projectWorkspace=createProjectWorkspace({getState:()=>state,commit,notify,navigate,openModal,closeModal,render,escapeHTML,icon,readImage});
$('bulk-shot').addEventListener('click',async()=>{const button=$('bulk-shot');button.disabled=true;try{await projectWorkspace.addShots([...selection]);selection.clear();syncSelection();}catch(error){notify(error.message);}finally{button.disabled=false;}});
const canvasEmbed=createCanvasEmbed({getState:()=>state,commit,notify,navigate,render,icon,closeOverlays:()=>{tagUI.closeDropdown();cardMenu.close();glassSelects.close();}});
void boot();
