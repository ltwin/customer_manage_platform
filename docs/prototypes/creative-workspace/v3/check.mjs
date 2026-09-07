// 原型 inline script 语法检查（走查前用）：node check.mjs
import { readFileSync } from 'node:fs';
const dir = new URL('./', import.meta.url);
let bad = 0;
for (const f of ['proto.js']) {
  try { new Function(readFileSync(new URL(f, dir), 'utf8')); console.log(f, 'OK'); } catch (e) { console.error(f, e.message); bad++; }
}
for (const f of ['index.html', 'workspaces.html', 'workspace.html', 'live.html']) {
  const s = readFileSync(new URL(f, dir), 'utf8');
  for (const m of s.matchAll(/<script>([\s\S]*?)<\/script>/g)) {
    try { new Function(m[1]); } catch (e) { console.error(f, e.message); bad++; }
  }
  console.log(f, 'inline JS OK');
}
process.exit(bad ? 1 : 0);
