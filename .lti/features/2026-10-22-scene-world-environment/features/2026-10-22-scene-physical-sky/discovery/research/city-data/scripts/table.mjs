// 把几份覆盖率结果拼成 Markdown 表格（清单逐项）
// 用法：node table.mjs <标题1>=<cov1.json> <标题2>=<cov2.json> ...
import { readFileSync } from 'node:fs';

const cols = process.argv.slice(2).map((a) => {
  const i = a.indexOf('=');
  return { title: a.slice(0, i), data: JSON.parse(readFileSync(a.slice(i + 1), 'utf8')) };
});
const cell = (it) => {
  if (!it || it.status === 'miss') return '—';
  const m = it.matched;
  if (it.status === 'zh') {
    const notes = [];
    if (!m.tz) notes.push('无时区');
    if (it.zhRank && it.zhRank > 1) notes.push(`排第 ${it.zhRank}`);
    return `${m.zh || m.zht}${notes.length ? `（${notes.join('，')}）` : ''}`;
  }
  return `英文 ${m.en}${m.zh ? `（中文名「${m.zh}」）` : ''}${m.km > 20 ? `，偏 ${m.km} km` : ''}`;
};
console.log(`| 清单项 | ${cols.map((c) => c.title).join(' | ')} |`);
console.log(`|---|${cols.map(() => '---').join('|')}|`);
cols[0].data.items.forEach((it, i) => {
  console.log(`| ${it.item} | ${cols.map((c) => cell(c.data.items[i])).join(' | ')} |`);
});
console.log(
  `| **合计（找到 / 中文命中）** | ${cols.map((c) => `${c.data.checklist.found} / ${c.data.checklist.zh}`).join(' | ')} |`,
);
