// 档位选择：
//   "all"      全部
//   "<N>"      全球按人口前 N
//   "cn+<N>"   中国大陆、香港、澳门、台湾的全部条目 + 其余地区按人口前 N
//   "cn+<N>z"  同上，但其余地区只留有中文名的条目
// 结果按人口降序。
const CN_GROUP = new Set(['CN', 'HK', 'MO', 'TW']);

export function selectTier(records, tier) {
  const sorted = [...records].sort((a, b) => b.pop - a.pop);
  if (!tier || tier === 'all') return sorted;
  const m = /^cn\+(\d+)(z?)$/.exec(tier);
  if (m) {
    const cn = sorted.filter((r) => CN_GROUP.has(r.cc));
    const rest = sorted.filter((r) => !CN_GROUP.has(r.cc) && (!m[2] || r.zh)).slice(0, Number(m[1]));
    return [...cn, ...rest].sort((a, b) => b.pop - a.pop);
  }
  return sorted.slice(0, Number(tier));
}
