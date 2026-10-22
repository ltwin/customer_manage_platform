// Chinese names: telling Han script from kana and hangul, and a heuristic for
// traditional characters (a set of characters common in place names that
// only traditional Chinese uses). Not a conversion; the build lists what it
// flags for a person to check (W2 research city-data.md).
const TRAD_ONLY = new Set(
  (
    '臺灣東門國華爾蘭羅馬亞倫廣場島縣鄉區開發鎮義陽關聖達維納蘇聯薩瓦錫魯烏齊濱廈長衛龍鳳愛賓頓諾瑪貝紐約洛磯舊金山' +
    '奧蒙圖倫德遜爾樂蘭鄧盧爾灣滬廣漢陝貴雲遼寧蘇濟澤寶樓萊陳鄭劉楊趙吳黃張費閩粵瀋陽臺營連鐵嶺錦齊遠鶴雞雙鴨' +
    '韓韋萬鄂夢烏魯黑龍騰衝豐熱濱灤滄龜嶼漳譚鍾懷寧銀邊險陸鄰滿顏應淵濰臨溫婁潛澗灘賴礦鋼網嚴鞍鞏橋線錢鐘'
  ).split(''),
);
// Characters simplified Chinese writes the same way slip into the list; take them out.
for (const c of '山约洛金旧德漳鞍蒙鄂黑瓦') TRAD_ONLY.delete(c);

export const hasCJK = (s) => typeof s === 'string' && /[㐀-鿿豈-﫿]/.test(s);
export const hasKana = (s) => typeof s === 'string' && /[぀-ヿ]/.test(s);
export const hasHangul = (s) => typeof s === 'string' && /[가-힯]/.test(s);
export const hasTradChar = (s) => {
  if (typeof s !== 'string') return false;
  for (const c of s) if (TRAD_ONLY.has(c)) return true;
  return false;
};
// A usable Chinese name: Han characters, no kana or hangul.
export const isZhName = (s) => hasCJK(s) && !hasKana(s) && !hasHangul(s);
