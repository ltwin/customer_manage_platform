/* 创作空间先导原型 v3 · 共享状态层
 *
 * 只服务原型走查：数据存在浏览器 localStorage，跨页面保持，可一键重置。
 * 数据形状刻意对齐 Epic 共享语言（卡片由账号持有、空间归属经成员关系表达、
 * 布局与内容分离、搬入批次保留原文），但本文件不是 API、数据库或字段契约。
 */
(function () {
  'use strict';

  const KEY = 'creative-workspace-proto-v3';
  const IMG = '../../../../frontend/public/marketing/';

  const uid = (p) => p + '_' + Math.random().toString(36).slice(2, 8);
  const now = () => Date.now();

  // ---- 演示用经营对象：只读，只用于关联与跳转 ----
  const DEMO = {
    customers: [
      { id: 'c_1', name: '林小满' },
      { id: 'c_2', name: '阿宁' },
      { id: 'c_3', name: '周与安' },
      { id: 'c_4', name: '陈木' },
    ],
    orders: [
      { id: 'o_1', title: '汉服外拍 · 林小满', date: '2026-08-16', status: '已交付', customer: 'c_1' },
      { id: 'o_2', title: 'cos 棚拍 · 阿宁', date: '2026-09-12', status: '已定档', customer: 'c_2' },
      { id: 'o_3', title: '毕业写真 · 周与安', date: '2026-09-20', status: '已定档', customer: 'c_3' },
      { id: 'o_4', title: '街拍 · 陈木', date: '2026-07-02', status: '已删除', customer: 'c_4', deleted: true },
    ],
  };

  function placeholder(label, tone) {
    // 中性色占位图：只作为演示素材，不含任何品牌色
    const t = tone || 0;
    const l = 82 - (t % 5) * 6;
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 500"><rect width="400" height="500" fill="hsl(40 8% ${l}%)"/><circle cx="${120 + (t * 37) % 160}" cy="${140 + (t * 53) % 200}" r="${70 + (t * 11) % 60}" fill="hsl(40 6% ${l - 12}%)"/><text x="20" y="470" font-family="system-ui" font-size="22" fill="hsl(40 5% ${l - 40}%)">${label}</text></svg>`;
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(svg);
  }

  function seed() {
    const t = now();
    const day = 86400000;
    const spaces = [
      { id: 'ws_inbox', kind: 'inbox', name: '', link: null, archived: false, createdAt: t - 40 * day, openedAt: t - 2 * day },
      { id: 'ws_1', kind: 'project', name: '', link: { type: 'order', id: 'o_1' }, archived: false, createdAt: t - 30 * day, openedAt: t - 20 * day },
      { id: 'ws_2', kind: 'project', name: '暗调机能风', link: { type: 'order', id: 'o_2' }, archived: false, createdAt: t - 6 * day, openedAt: t - 1 * day },
      { id: 'ws_3', kind: 'project', name: '', link: { type: 'customer', id: 'c_3' }, archived: false, createdAt: t - 5 * day, openedAt: t - 3 * day },
      { id: 'ws_4', kind: 'project', name: '', link: { type: 'order', id: 'o_4' }, archived: false, createdAt: t - 60 * day, openedAt: t - 58 * day },
      { id: 'ws_5', kind: 'project', name: '海边黄昏', link: null, archived: false, createdAt: t - 12 * day, openedAt: t - 9 * day },
      { id: 'ws_6', kind: 'project', name: '', link: null, archived: false, createdAt: t - 15 * day, openedAt: t - 15 * day },
      { id: 'ws_7', kind: 'project', name: '旧宅 · 民国感', link: null, archived: false, createdAt: t - 22 * day, openedAt: t - 21 * day },
      { id: 'ws_8', kind: 'project', name: '', link: { type: 'customer', id: 'c_4' }, archived: false, createdAt: t - 25 * day, openedAt: t - 24 * day },
      { id: 'ws_9', kind: 'project', name: '雨夜霓虹', link: null, archived: false, createdAt: t - 33 * day, openedAt: t - 30 * day },
      { id: 'ws_10', kind: 'project', name: '', link: null, archived: false, createdAt: t - 44 * day, openedAt: t - 44 * day },
      { id: 'ws_11', kind: 'project', name: '雪地 · 白', link: null, archived: false, createdAt: t - 50 * day, openedAt: t - 47 * day },
    ];
    const cards = [];
    const memberships = [];
    const groups = [];
    const batches = [];
    let order = 0;
    const put = (spaceId, card, groupId) => {
      cards.push(Object.assign({ id: uid('card'), rev: 1, createdAt: t - 3 * day, archived: false }, card));
      memberships.push({ cardId: cards[cards.length - 1].id, spaceId, order: order++, groupId: groupId || null });
      return cards[cards.length - 1];
    };
    // 汉服外拍（已交付）
    put('ws_1', { type: 'image', src: IMG + 'work-1.jpg', caption: '', sourceClass: 'unknown_web' });
    put('ws_1', { type: 'image', src: placeholder('回廊 · 逆光', 1), caption: '', sourceClass: 'unknown_web' });
    put('ws_1', { type: 'text', text: '回廊尽头，侧逆光，衣袖带一点风' });
    // 暗调机能风（本周棚拍）
    groups.push({ id: 'g_1', spaceId: 'ws_2', name: '第一套 · 走廊', order: 0 });
    groups.push({ id: 'g_2', spaceId: 'ws_2', name: '第二套 · 硬光', order: 1 });
    const b1 = { id: 'batch_1', spaceId: 'ws_2', createdAt: t - 5 * day, rawText: '阿宁：这次想拍暗一点的\n阿宁：像上次那组走廊的感觉但更冷\n阿宁：[图片]\n我：背光加一点烟？\n阿宁：可以！还有要一张正面硬光的，眼神要凶\n阿宁：道具我带了个面罩' };
    batches.push(b1);
    put('ws_2', { type: 'image', src: IMG + 'work-2.jpg', caption: '', sourceClass: 'unknown_web' }, 'g_1');
    put('ws_2', { type: 'image', src: placeholder('走廊 · 冷调', 2), caption: '', sourceClass: 'unknown_web' }, 'g_1');
    put('ws_2', { type: 'text', text: '像上次那组走廊的感觉但更冷', batchId: 'batch_1', batchSeq: 2 }, 'g_1');
    put('ws_2', { type: 'text', text: '背光加一点烟？', batchId: 'batch_1', batchSeq: 4 }, 'g_1');
    put('ws_2', { type: 'image', src: placeholder('硬光 · 正面', 3), caption: '', sourceClass: 'unknown_web' }, 'g_2');
    put('ws_2', { type: 'text', text: '要一张正面硬光的，眼神要凶', batchId: 'batch_1', batchSeq: 5 }, 'g_2');
    put('ws_2', { type: 'text', text: '道具：面罩（阿宁自带）', batchId: 'batch_1', batchSeq: 6 }, 'g_2');
    put('ws_2', { type: 'link', url: 'https://www.xiaohongshu.com/explore/6f3a9c…' });
    put('ws_2', { type: 'image', src: placeholder('烟雾 · 背光', 4), caption: '', sourceClass: 'unknown_web' });
    // 毕业写真
    put('ws_3', { type: 'image', src: IMG + 'hero-studio.jpg', caption: '', sourceClass: 'unknown_web' });
    put('ws_3', { type: 'text', text: '学士服 + 操场看台，下午四点后' });
    // 已删除订单的空间
    put('ws_4', { type: 'image', src: placeholder('街头 · 夜', 5), caption: '', sourceClass: 'unknown_web' });
    // 其他空间少量素材
    put('ws_5', { type: 'image', src: placeholder('海边 · 黄昏', 6), caption: '', sourceClass: 'unknown_web' });
    put('ws_5', { type: 'image', src: placeholder('剪影', 7), caption: '', sourceClass: 'unknown_web' });
    put('ws_7', { type: 'image', src: placeholder('旧宅 · 窗', 8), caption: '', sourceClass: 'unknown_web' });
    put('ws_9', { type: 'image', src: placeholder('霓虹 · 雨', 9), caption: '', sourceClass: 'unknown_web' });
    put('ws_11', { type: 'image', src: placeholder('雪地', 10), caption: '', sourceClass: 'unknown_web' });
    // 未归类
    put('ws_inbox', { type: 'image', src: placeholder('逆光 · 奔跑', 11), caption: '', sourceClass: 'unknown_web' });
    put('ws_inbox', { type: 'text', text: '低机位 + 长焦压缩，等一个人跑过来' });
    put('ws_inbox', { type: 'link', url: 'https://www.bilibili.com/video/BV1xx…' });

    const shootItems = [
      { id: 'si_1', spaceId: 'ws_2', order: 0, title: '走廊 · 冷调背光', desc: '像上次那组走廊的感觉但更冷；背光加一点烟', refSrc: cards[3].src, sourceCardId: cards[3].id, sourceRev: 1, status: 'active', result: null },
      { id: 'si_2', spaceId: 'ws_2', order: 1, title: '正面硬光 · 眼神凶', desc: '硬光，正面，面罩在手上', refSrc: cards[7].src, sourceCardId: cards[7].id, sourceRev: 1, status: 'active', result: null },
    ];
    const memos = [
      { id: uid('memo'), spaceId: 'ws_2', text: '烟饼两块，打火机', checked: false, order: 0 },
      { id: uid('memo'), spaceId: 'ws_2', text: '备用电池充满', checked: true, order: 1 },
    ];
    return { version: 1, spaces, cards, memberships, groups, batches, shootItems, memos, sessions: [] };
  }

  let state = null;
  function load() {
    if (state) return state;
    try {
      const raw = localStorage.getItem(KEY);
      if (raw) { state = JSON.parse(raw); return state; }
    } catch (e) { /* 损坏则重建 */ }
    state = seed();
    save();
    return state;
  }
  function save() {
    try { localStorage.setItem(KEY, JSON.stringify(state)); return true; }
    catch (e) { return false; }
  }
  function reset() { localStorage.removeItem(KEY); state = null; load(); }

  // ---- 读取 ----
  const S = () => load();
  const getSpace = (id) => S().spaces.find((s) => s.id === id) || null;
  const inbox = () => S().spaces.find((s) => s.kind === 'inbox');
  function linkTarget(link) {
    if (!link) return null;
    if (link.type === 'order') {
      const o = DEMO.orders.find((x) => x.id === link.id);
      return o ? { type: 'order', id: o.id, name: o.title, broken: !!o.deleted, sub: o.date + ' · ' + o.status } : { type: 'order', broken: true, name: '' };
    }
    const c = DEMO.customers.find((x) => x.id === link.id);
    return c ? { type: 'customer', id: c.id, name: c.name, broken: false } : { type: 'customer', broken: true, name: '' };
  }
  function displayName(space) {
    if (space.kind === 'inbox') return { text: '未归类', fallback: false };
    if (space.name && space.name.trim()) return { text: space.name.trim(), fallback: false };
    const t = linkTarget(space.link);
    if (t && t.name) return { text: t.name, fallback: true };
    return { text: '未命名空间', fallback: true };
  }
  function listSpaces() {
    const st = S();
    const projects = st.spaces.filter((s) => s.kind === 'project' && !s.archived).sort((a, b) => b.openedAt - a.openedAt);
    return [inbox()].concat(projects);
  }
  function cardsInSpace(spaceId) {
    const st = S();
    return st.memberships
      .filter((m) => m.spaceId === spaceId)
      .map((m) => ({ m, card: st.cards.find((c) => c.id === m.cardId) }))
      .filter((x) => x.card && !x.card.archived)
      .sort((a, b) => a.m.order - b.m.order);
  }
  function groupsInSpace(spaceId) {
    return S().groups.filter((g) => g.spaceId === spaceId).sort((a, b) => a.order - b.order);
  }
  function spaceImages(spaceId, n) {
    return cardsInSpace(spaceId).filter((x) => x.card.type === 'image').slice(0, n || 4).map((x) => x.card.src);
  }
  const shootList = (spaceId) => S().shootItems.filter((i) => i.spaceId === spaceId && i.status === 'active').sort((a, b) => a.order - b.order);
  const memosIn = (spaceId) => S().memos.filter((m) => m.spaceId === spaceId).sort((a, b) => a.order - b.order);
  const getCard = (id) => S().cards.find((c) => c.id === id) || null;
  function sourceState(item) {
    const c = getCard(item.sourceCardId);
    if (!c || c.archived) return 'unavailable';
    const m = S().memberships.find((x) => x.cardId === c.id);
    return m ? 'available' : 'unavailable';
  }

  // ---- 写入 ----
  function touch(space) { space.openedAt = now(); save(); }
  function createSpace() {
    const s = { id: uid('ws'), kind: 'project', name: '', link: null, archived: false, createdAt: now(), openedAt: now() };
    S().spaces.push(s); save(); return s;
  }
  function renameSpace(id, name) { const s = getSpace(id); if (s && s.kind !== 'inbox') { s.name = name; save(); } }
  // 归档只是把空间从台账移开；卡片由账号持有，成员关系原样保留，随时可恢复。
  function archiveSpace(id) { const s = getSpace(id); if (s && s.kind !== 'inbox') { s.archived = true; s.archivedAt = now(); save(); } }
  function unarchiveSpace(id) { const s = getSpace(id); if (s) { s.archived = false; delete s.archivedAt; save(); } }
  function listArchived() { return S().spaces.filter((s) => s.kind === 'project' && s.archived).sort((a, b) => (b.archivedAt || 0) - (a.archivedAt || 0)); }
  function setLink(id, link) { const s = getSpace(id); if (s && s.kind !== 'inbox') { s.link = link; save(); } }

  function nextOrder(spaceId) {
    const ms = S().memberships.filter((m) => m.spaceId === spaceId);
    return ms.length ? Math.max.apply(null, ms.map((m) => m.order)) + 1 : 0;
  }
  function addCard(spaceId, card, groupId) {
    const c = Object.assign({ id: uid('card'), rev: 1, createdAt: now(), archived: false }, card);
    S().cards.push(c);
    S().memberships.push({ cardId: c.id, spaceId, order: nextOrder(spaceId), groupId: groupId || null });
    return c;
  }
  const looksLikeUrl = (s) => /^(https?:\/\/|www\.)\S+$/i.test(s.trim());
  function importText(spaceId, text) {
    const raw = text.replace(/\r/g, '');
    const lines = raw.split('\n').map((l) => l.trim()).filter(Boolean);
    if (!lines.length) return { cards: [], batch: null };
    const batch = { id: uid('batch'), spaceId, createdAt: now(), rawText: raw };
    S().batches.push(batch);
    const made = lines.map((line, i) =>
      looksLikeUrl(line)
        ? addCard(spaceId, { type: 'link', url: line, batchId: batch.id, batchSeq: i + 1 })
        : addCard(spaceId, { type: 'text', text: line, batchId: batch.id, batchSeq: i + 1 })
    );
    save();
    return { cards: made, batch };
  }
  function importImages(spaceId, dataUrls) {
    const batch = { id: uid('batch'), spaceId, createdAt: now(), rawText: null };
    S().batches.push(batch);
    // 素材来源分类默认最严格：unknown_web（不可用于任何生成用途）。原型不暴露该字段。
    const made = dataUrls.map((src, i) => addCard(spaceId, { type: 'image', src, caption: '', sourceClass: 'unknown_web', batchId: batch.id, batchSeq: i + 1 }));
    const ok = save();
    return { cards: made, batch, persisted: ok };
  }
  function archiveCard(cardId) {
    const c = getCard(cardId); if (!c) return;
    c.archived = true; c.rev += 1;
    S().memberships = S().memberships.filter((m) => m.cardId !== cardId);
    save();
  }
  function moveCard(cardId, toSpaceId) {
    const m = S().memberships.find((x) => x.cardId === cardId);
    if (!m) return;
    m.spaceId = toSpaceId; m.groupId = null; m.order = nextOrder(toSpaceId);
    save();
  }
  function reorder(spaceId, cardId, beforeCardId, groupId) {
    const st = S();
    const ms = st.memberships.filter((m) => m.spaceId === spaceId).sort((a, b) => a.order - b.order);
    const moving = ms.find((m) => m.cardId === cardId); if (!moving) return;
    const rest = ms.filter((m) => m !== moving);
    let idx = beforeCardId ? rest.findIndex((m) => m.cardId === beforeCardId) : rest.length;
    if (idx < 0) idx = rest.length;
    rest.splice(idx, 0, moving);
    moving.groupId = groupId === undefined ? moving.groupId : groupId;
    rest.forEach((m, i) => { m.order = i; });
    save();
  }
  function createGroup(spaceId, name, cardIds) {
    const gs = groupsInSpace(spaceId);
    const g = { id: uid('g'), spaceId, name: name || ('分组 ' + (gs.length + 1)), order: gs.length };
    S().groups.push(g);
    (cardIds || []).forEach((id) => { const m = S().memberships.find((x) => x.cardId === id && x.spaceId === spaceId); if (m) m.groupId = g.id; });
    save(); return g;
  }
  function renameGroup(id, name) { const g = S().groups.find((x) => x.id === id); if (g) { g.name = name; save(); } }
  function dissolveGroup(id) {
    S().memberships.forEach((m) => { if (m.groupId === id) m.groupId = null; });
    S().groups = S().groups.filter((g) => g.id !== id);
    save();
  }
  function setGroup(cardIds, groupId) {
    cardIds.forEach((id) => { const m = S().memberships.find((x) => x.cardId === id); if (m) m.groupId = groupId; });
    save();
  }

  function createShootItems(spaceId, cardIds, opts) {
    // 值拷贝：从卡片拷出可见字段，记录来源 ID 与 revision；此后与来源无任何同步。
    const list = shootList(spaceId);
    let order = list.length;
    const made = [];
    const single = opts && opts.merge && cardIds.length > 1;
    if (single) {
      const cs = cardIds.map(getCard).filter(Boolean);
      const img = cs.find((c) => c.type === 'image');
      const texts = cs.filter((c) => c.type === 'text').map((c) => c.text);
      const item = {
        id: uid('si'), spaceId, order: order++,
        title: texts[0] || '要拍的画面 ' + (order),
        desc: texts.slice(1).join('\n'),
        refSrc: img ? img.src : null,
        sourceCardId: (img || cs[0]).id, sourceRev: (img || cs[0]).rev,
        sourceCardIds: cs.map((c) => c.id),
        status: 'active', result: null,
      };
      S().shootItems.push(item); made.push(item);
    } else {
      cardIds.forEach((id) => {
        const c = getCard(id); if (!c) return;
        const item = {
          id: uid('si'), spaceId, order: order++,
          title: c.type === 'text' ? c.text : c.type === 'link' ? c.url : ('要拍的画面 ' + order),
          desc: '',
          refSrc: c.type === 'image' ? c.src : null,
          sourceCardId: c.id, sourceRev: c.rev, sourceCardIds: [c.id],
          status: 'active', result: null,
        };
        S().shootItems.push(item); made.push(item);
      });
    }
    save(); return made;
  }
  function updateShootItem(id, patch) { const i = S().shootItems.find((x) => x.id === id); if (i) { Object.assign(i, patch); save(); } }
  function tombstone(id) { updateShootItem(id, { status: 'tombstone' }); }
  function setResult(id, result) { updateShootItem(id, { result }); }

  function addMemos(spaceId, text) {
    const lines = text.replace(/\r/g, '').split('\n').map((l) => l.trim()).filter(Boolean);
    let order = memosIn(spaceId).length;
    lines.forEach((t) => S().memos.push({ id: uid('memo'), spaceId, text: t, checked: false, order: order++ }));
    save(); return lines.length;
  }
  function toggleMemo(id) { const m = S().memos.find((x) => x.id === id); if (m) { m.checked = !m.checked; save(); } }
  function deleteMemo(id) { S().memos = S().memos.filter((m) => m.id !== id); save(); }
  function moveMemo(id, dir) {
    const m = S().memos.find((x) => x.id === id); if (!m) return;
    const list = memosIn(m.spaceId); const i = list.indexOf(m); const j = i + dir;
    if (j < 0 || j >= list.length) return;
    const o = list[j].order; list[j].order = m.order; m.order = o; save();
  }

  // ---- 通用 UI 小件 ----
  let toastTimer = null;
  function toast(msg, warn) {
    let el = document.querySelector('.toast');
    if (!el) { el = document.createElement('div'); el.className = 'toast'; el.setAttribute('role', 'status'); document.body.appendChild(el); }
    el.textContent = msg; el.classList.toggle('warn', !!warn); el.classList.add('show');
    clearTimeout(toastTimer); toastTimer = setTimeout(() => el.classList.remove('show'), 2200);
  }
  function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
  function host(url) { try { return new URL(url.startsWith('http') ? url : 'https://' + url).host.replace(/^www\./, ''); } catch (e) { return url; } }
  function ago(ts) {
    const d = Math.round((now() - ts) / 86400000);
    if (d <= 0) return '今天'; if (d === 1) return '昨天'; if (d < 30) return d + ' 天前'; return Math.round(d / 30) + ' 个月前';
  }
  function readFilesAsDataUrls(files, done) {
    const imgs = Array.from(files).filter((f) => f.type.startsWith('image/'));
    const rejected = Array.from(files).filter((f) => f.type.startsWith('video/'));
    if (!imgs.length) return done([], rejected);
    const out = new Array(imgs.length); let n = 0;
    imgs.forEach((f, i) => {
      const r = new FileReader();
      r.onload = () => {
        // 走查环境下压一遍尺寸，避免 localStorage 撑爆；正式产品走对象存储，不在这里定型
        const im = new Image();
        im.onload = () => {
          const max = 720; const k = Math.min(1, max / Math.max(im.width, im.height));
          const cv = document.createElement('canvas'); cv.width = Math.round(im.width * k); cv.height = Math.round(im.height * k);
          cv.getContext('2d').drawImage(im, 0, 0, cv.width, cv.height);
          out[i] = cv.toDataURL('image/jpeg', 0.78);
          if (++n === imgs.length) done(out, rejected);
        };
        im.src = r.result;
      };
      r.readAsDataURL(f);
    });
  }
  function protobar(extra) {
    const bar = document.createElement('div'); bar.className = 'protobar'; bar.setAttribute('aria-label', '原型控制');
    bar.innerHTML = '<span>原型控制 · 正式产品不显示</span>' + (extra || '') +
      '<button class="btn" data-proto="theme">切换暗色</button><button class="btn" data-proto="reset">重置示例数据</button><button class="btn" data-proto="hide">收起</button>';
    document.body.appendChild(bar);
    const tg = document.createElement('button'); tg.className = 'protobar-toggle'; tg.textContent = '⋯'; tg.title = '原型控制'; document.body.appendChild(tg);
    tg.addEventListener('click', () => { document.body.classList.remove('hide-proto'); localStorage.setItem(KEY + ':proto', 'show'); });
    bar.addEventListener('click', (e) => {
      const b = e.target.closest('[data-proto]'); if (!b) return;
      const k = b.dataset.proto;
      if (k === 'theme') { const h = document.documentElement; h.dataset.theme = h.dataset.theme === 'dark' ? '' : 'dark'; localStorage.setItem(KEY + ':theme', h.dataset.theme); }
      if (k === 'reset') { if (confirm('重置为示例数据？你搬入的素材会清空。')) { reset(); location.reload(); } }
      if (k === 'hide') { document.body.classList.add('hide-proto'); localStorage.setItem(KEY + ':proto', 'hide'); }
    });
    // 默认收起：走查者看不到控制条，只有主持人需要时点 ⋯ 展开
    if (localStorage.getItem(KEY + ':proto') !== 'show') document.body.classList.add('hide-proto');
    const th = localStorage.getItem(KEY + ':theme'); if (th) document.documentElement.dataset.theme = th;
  }
  const q = (k) => new URLSearchParams(location.search).get(k);

  window.Proto = {
    DEMO, load, save, reset, getSpace, inbox, listSpaces, displayName, linkTarget, cardsInSpace, groupsInSpace, spaceImages,
    shootList, memosIn, getCard, sourceState, touch, createSpace, renameSpace, archiveSpace, unarchiveSpace, listArchived, setLink,
    importText, importImages, archiveCard, moveCard, reorder, createGroup, renameGroup, dissolveGroup, setGroup,
    createShootItems, updateShootItem, tombstone, setResult, addMemos, toggleMemo, deleteMemo, moveMemo,
    toast, esc, host, ago, readFilesAsDataUrls, protobar, q, uid, now,
  };
})();
