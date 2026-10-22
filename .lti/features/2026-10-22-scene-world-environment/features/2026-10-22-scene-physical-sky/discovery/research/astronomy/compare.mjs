// astronomy-engine 与 NOAA 太阳计算器的逐点对照。
// 运行：ASTRONOMY_ENGINE=<astronomy-engine/esm/astronomy.js 的绝对路径> node compare.mjs
// 不设 ASTRONOMY_ENGINE 时按包名 import（装进 frontend 之后可直接用）。
// 输出：compare-results.json（全部数据点和统计）。
import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { localToUtcMs, utcOffsetMs } from './local-time.mjs';
import { noaaSolarPosition, noaaSunriseSunsetUtcMs } from './noaa-solar.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const enginePath = process.env.ASTRONOMY_ENGINE;
const A = await import(enginePath ? pathToFileURL(enginePath).href : 'astronomy-engine');

export const LOCATIONS = {
  shanghai: { name: '上海', latitude: 31.2304, longitude: 121.4737, timeZone: 'Asia/Shanghai' },
  beijing: { name: '北京', latitude: 39.9042, longitude: 116.4074, timeZone: 'Asia/Shanghai' },
  chengdu: { name: '成都', latitude: 30.5728, longitude: 104.0668, timeZone: 'Asia/Shanghai' },
  sydney: { name: '悉尼', latitude: -33.8688, longitude: 151.2093, timeZone: 'Australia/Sydney' },
};
const DATES = ['2026-03-20', '2026-06-21', '2026-12-21'];
const MIN_ALT = -5; // 只统计 astronomy-engine 视高度不低于 −5° 的点

const pad = (n) => String(n).padStart(2, '0');
const hhmm = (mins) => `${pad(Math.floor(mins / 60))}:${pad(mins % 60)}`;
const wrap180 = (d) => ((((d + 180) % 360) + 360) % 360) - 180;
const r = (x, k = 4) => (x === null ? null : Number(x.toFixed(k)));

/** 两个（高度, 方位）方向之间的角距（度）。 */
function separation(alt1, az1, alt2, az2) {
  const d = Math.PI / 180;
  const c =
    Math.sin(alt1 * d) * Math.sin(alt2 * d) + Math.cos(alt1 * d) * Math.cos(alt2 * d) * Math.cos((az1 - az2) * d);
  return Math.acos(Math.min(1, Math.max(-1, c))) / d;
}

/** astronomy-engine：视位置（'normal' 折射）与几何位置。 */
export function aeSun(utcMs, loc) {
  const time = A.MakeTime(new Date(utcMs));
  const observer = new A.Observer(loc.latitude, loc.longitude, 0);
  const eq = A.Equator(A.Body.Sun, time, observer, true, true); // 当天赤道坐标 + 光行差，Horizon 要求 ofdate=true
  const app = A.Horizon(time, observer, eq.ra, eq.dec, 'normal');
  const geo = A.Horizon(time, observer, eq.ra, eq.dec);
  return { altitude: app.altitude, azimuth: app.azimuth, altitudeGeom: geo.altitude };
}

function samplePoint(loc, date, mins) {
  const [y, m, d] = date.split('-').map(Number);
  const utcMs = localToUtcMs(date, hhmm(mins), loc.timeZone);
  const tzHours = utcOffsetMs(utcMs, loc.timeZone) / 3_600_000;
  const ae = aeSun(utcMs, loc);
  const noaa = noaaSolarPosition({ year: y, month: m, day: d, minutes: mins, latitude: loc.latitude, longitude: loc.longitude, tzHours });
  return {
    local: hhmm(mins),
    utc: new Date(utcMs).toISOString(),
    ae: { alt: r(ae.altitude), az: r(ae.azimuth), altGeom: r(ae.altitudeGeom) },
    noaa: { alt: r(noaa.elevation), az: r(noaa.azimuth), altGeom: r(noaa.exoatmElevation) },
    dAlt: r(ae.altitude - noaa.elevation),
    dAltGeom: r(ae.altitudeGeom - noaa.exoatmElevation),
    dAz: r(wrap180(ae.azimuth - noaa.azimuth)),
    sep: r(separation(ae.altitude, ae.azimuth, noaa.elevation, noaa.azimuth)),
  };
}

function riseSet(loc, date) {
  const [y, m, d] = date.split('-').map(Number);
  // 注意：FlexibleDateTime 的 number 表示「J2000 起的 UT 天数」，不是毫秒；必须传 Date 或 AstroTime。
  const start = A.MakeTime(new Date(localToUtcMs(date, '00:00', loc.timeZone)));
  const tzHours = utcOffsetMs(localToUtcMs(date, '12:00', loc.timeZone), loc.timeZone) / 3_600_000;
  const observer = new A.Observer(loc.latitude, loc.longitude, 0);
  const ms = (t) => (t === null ? null : t.date.getTime());
  const aeRise = ms(A.SearchRiseSet(A.Body.Sun, observer, +1, start, 1));
  const aeSet = ms(A.SearchRiseSet(A.Body.Sun, observer, -1, start, 1));
  // NOAA 口径的另一种写法：太阳中心的几何高度过 −0.833°
  const aeRise833 = ms(A.SearchAltitude(A.Body.Sun, observer, +1, start, 1, -0.833));
  const aeSet833 = ms(A.SearchAltitude(A.Body.Sun, observer, -1, start, 1, -0.833));
  const noaa = noaaSunriseSunsetUtcMs({ year: y, month: m, day: d, latitude: loc.latitude, longitude: loc.longitude, tzHours });
  const iso = (x) => (x === null ? null : new Date(x).toISOString());
  const sec = (a, b) => (a === null || b === null ? null : r((a - b) / 1000, 1));
  return {
    tzHours,
    ae: { rise: iso(aeRise), set: iso(aeSet) },
    aeCenter0833: { rise: iso(aeRise833), set: iso(aeSet833) },
    noaa: { rise: iso(noaa.rise), set: iso(noaa.set) },
    dRiseSec: sec(aeRise, noaa.rise),
    dSetSec: sec(aeSet, noaa.set),
    dRise0833Sec: sec(aeRise833, noaa.rise),
    dSet0833Sec: sec(aeSet833, noaa.set),
  };
}

function stats(points) {
  const maxAbs = (key, filter = () => true) => {
    const xs = points.filter(filter).map((p) => Math.abs(p[key]));
    return xs.length ? r(Math.max(...xs)) : null;
  };
  const visible = (p) => p.ae.alt >= 0 && p.noaa.alt >= 0;
  return {
    n: points.length,
    nVisible: points.filter(visible).length,
    maxAbsDAlt: maxAbs('dAlt'),
    maxAbsDAltVisible: maxAbs('dAlt', visible),
    maxAbsDAltGeom: maxAbs('dAltGeom'),
    maxAbsDAz: maxAbs('dAz'),
    maxAbsDAzVisible: maxAbs('dAz', visible),
    maxSep: maxAbs('sep'),
    maxSepVisible: maxAbs('sep', visible),
  };
}

function runCase(locKey, date, fromMins, toMins, filterAlt) {
  const loc = LOCATIONS[locKey];
  const points = [];
  for (let mins = fromMins; mins <= toMins; mins += 15) {
    const p = samplePoint(loc, date, mins);
    if (filterAlt && p.ae.alt < MIN_ALT) continue;
    points.push(p);
  }
  return { location: locKey, ...loc, date, window: `${hhmm(fromMins)}–${hhmm(toMins)}`, filter: filterAlt ? `ae.alt >= ${MIN_ALT}` : 'none', stats: stats(points), riseSet: riseSet(loc, date), points };
}

const cases = [];
// 1. 上海 2026-06-21，05:00–20:00 每 15 分钟，不过滤高度
cases.push(runCase('shanghai', '2026-06-21', 5 * 60, 20 * 60, false));
// 2. 四地 × 三天，全天每 15 分钟，只留 −5° 以上
for (const locKey of Object.keys(LOCATIONS)) {
  for (const date of DATES) cases.push(runCase(locKey, date, 0, 23 * 60 + 45, true));
}

const out = {
  generated: '2026-10-22',
  astronomyEngineVersion: '2.1.19',
  node: process.version,
  noaaSource: 'https://gml.noaa.gov/grad/solcalc/main.js (calcAzEl, calcSunriseSet), refraction per https://gml.noaa.gov/grad/solcalc/calcdetails.html',
  conventions: {
    azimuth: 'degrees clockwise from north, both sides',
    aeAltitude: "Horizon(..., 'normal') of Equator(Body.Sun, t, observer, ofdate=true, aberration=true); observer height 0 m",
    noaaAltitude: 'calcAzEl elevation (NOAA refraction approximation)',
    altGeom: 'no refraction on either side',
    sep: 'great-circle angle between the two apparent directions',
    riseSet: 'ae = SearchRiseSet (upper limb, 34 arcmin refraction); aeCenter0833 = SearchAltitude(center, -0.833); noaa = calcSunriseSet (center at -0.833)',
  },
  summary: cases.map((c) => ({ location: c.location, date: c.date, window: c.window, filter: c.filter, ...c.stats, dRiseSec: c.riseSet.dRiseSec, dSetSec: c.riseSet.dSetSec, dRise0833Sec: c.riseSet.dRise0833Sec, dSet0833Sec: c.riseSet.dSet0833Sec })),
  cases,
};
writeFileSync(join(here, 'compare-results.json'), `${JSON.stringify(out, null, 1)}\n`);
console.table(out.summary);
