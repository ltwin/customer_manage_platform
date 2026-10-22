// 中文名辅助：CJK 判定、繁体字启发式检测
// 繁体检测用一组「只在繁体里出现、地名常用」的字；命中即判为含繁体字。
// 这是启发式，不是完整的繁简转换，只用来估计比例。
const TRAD_ONLY = new Set(
  (
    '臺灣東門國華爾蘭羅馬亞倫廣場島縣鄉區開發鎮義陽關聖達維納蘇聯薩瓦錫魯烏齊濱廈長衛龍鳳愛賓頓諾瑪貝紐約洛磯舊金山' +
    '奧蒙圖倫德遜爾樂蘭鄧盧爾灣滬廣漢陝貴雲遼寧蘇濟澤寶樓萊陳鄭劉楊趙吳黃張費閩粵瀋陽臺營連鐵嶺錦齊遠鶴雞雙鴨' +
    '韓韋萬鄂夢烏魯黑龍騰衝豐熱濱灤滄龜嶼漳譚鍾懷寧銀邊險陸鄰滿顏應淵濰臨溫婁潛澗灘賴礦鋼網嚴鞍鞏橋線錢鐘'
  ).split(''),
);
// 简体里也合法的字会误入上表（如「山」），这里剔掉明显的简繁同形字
for (const c of '山约洛金旧德漳鞍蒙鄂黑瓦') TRAD_ONLY.delete(c);

export const hasCJK = (s) => typeof s === 'string' && /[㐀-鿿豈-﫿]/.test(s);
export const hasKana = (s) => typeof s === 'string' && /[぀-ヿ]/.test(s);
export const hasHangul = (s) => typeof s === 'string' && /[가-힯]/.test(s);
export const hasTradChar = (s) => {
  if (typeof s !== 'string') return false;
  for (const c of s) if (TRAD_ONLY.has(c)) return true;
  return false;
};
// 是否像一个可用的中文名：含汉字，且不含假名和谚文
export const isZhName = (s) => hasCJK(s) && !hasKana(s) && !hasHangul(s);
