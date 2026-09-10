export const canvasSurface = document.documentElement.dataset.surface === 'canvas';
export function createCanvasEmbed(ctx) {
  if (!canvasSurface) return { sync() {}, add() {} };
  const send = data => parent.postMessage({ channel: 'creative-library-v5', ...data }, location.origin);
  const serial = asset => ({ ...asset, src: asset.src ? new URL(asset.src, location.href).href : '', text: asset.text || asset.description || asset.source || '' });
  function sync() { if (ctx.getState()) send({ type: 'catalog', assets: ctx.getState().assets.map(serial) }); }
  function add(ids) { const assets = [...ids].map(id => ctx.getState().assets.find(a => a.id === id)).filter(Boolean).map(serial); if (assets.length) { send({ type: 'add', ids: assets.map(a => a.id), assets }); ctx.notify(`正在放入画布 · ${assets.length} 份素材`); } }
  const nav = document.createElement('nav'); nav.className = 'shelf-nav'; nav.setAttribute('aria-label', '个人资产视图'); nav.innerHTML = '<a href="#view=library" data-shelf-view="library">全部素材</a><a href="#view=collections" data-shelf-view="collections">集合</a>';
  document.querySelector('.topbar').after(nav);
  const syncRoute = () => { const view = new URLSearchParams(location.hash.slice(1)).get('view') || 'library'; nav.querySelectorAll('a').forEach(a => { if (a.dataset.shelfView === view) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current'); }); };
  syncRoute(); window.addEventListener('hashchange', syncRoute);
  document.getElementById('search').placeholder = '搜索素材、标签或笔记';
  document.getElementById('detail-add').innerHTML = ctx.icon('plus') + '放入画布';
  document.getElementById('bulk-project').textContent = '放入画布';
  document.getElementById('import-open').setAttribute('aria-label', '收集灵感');
  document.getElementById('gallery').addEventListener('dragstart', event => {
    const card = event.target.closest('[data-id]'); if (!card || event.target.closest('button.icon-button')) return;
    const asset = ctx.getState().assets.find(a => a.id === card.dataset.id); if (!asset) return;
    event.dataTransfer.setData('application/x-creative-asset', JSON.stringify({ id: asset.id })); event.dataTransfer.effectAllowed = 'copy'; sync();
  });
  window.addEventListener('message', async event => {
    if (event.origin !== location.origin || event.source !== parent || event.data?.channel !== 'creative-canvas-v5') return;
    if(event.data.type==='add-complete'){ctx.notify(event.data.error||`已放入画布 · ${event.data.count} 份素材`);return;}
    if (event.data.type === 'close-overlays') { ctx.closeOverlays(); return; }
    if (event.data.type === 'request-catalog') { sync(); return; }
    if (event.data.type === 'view') { ctx.navigate({ view: event.data.view === 'collections' ? 'collections' : 'library', group: '', q: '' }); return; }
    if (event.data.type === 'collect-market') {
      const { requestId, asset } = event.data;
      try {
        if (!asset || !['image', 'text', 'link'].includes(asset.kind) || typeof asset.id !== 'string' || typeof asset.title !== 'string') throw new Error('示例资产格式无效。');
        if (asset.src && new URL(asset.src, location.href).origin !== location.origin) throw new Error('只接收本地示例图片。');
        if (!ctx.getState()) throw new Error('个人资产库尚未打开，请稍后重试。');
        const next = structuredClone(ctx.getState());
        if (!next.assets.some(a => a.id === asset.id)) { next.assets.unshift({ id: asset.id, kind: asset.kind, title: asset.title.slice(0, 200), text: String(asset.text || '').slice(0, 10000), src: asset.src || '', width: Number(asset.width) || 4, height: Number(asset.height) || 4, tags: Array.isArray(asset.tags) ? asset.tags.map(String).slice(0, 20) : [], source: asset.source || '', description: String(asset.description || ''), note: '', created: Date.now() }); await ctx.commit(next); ctx.render(); }
        sync(); send({ type: 'collected', requestId, id: asset.id });
      } catch (error) { send({ type: 'collect-error', requestId, message: error.message }); }
    }
  });
  return { sync, add };
}
