// JPL Horizons 第三方抽查：太阳视位置（含折射）和日出日落时刻。
// 运行：ASTRONOMY_ENGINE=<astronomy-engine/esm/astronomy.js 的绝对路径> node horizons.mjs
// 需要联网，调用 https://ssd.jpl.nasa.gov/api/horizons.api 。输出 horizons-results.json。
import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { localToUtcMs, utcOffsetMs } from './local-time.mjs';
import { noaaSolarPosition, noaaSunriseSunsetUtcMs } from './noaa-solar.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const enginePath = process.env.ASTRONOMY_ENGINE;
const A = await import(enginePath ? pathToFileURL(enginePath).href : 'astronomy-engine');

const API = 'https://ssd.jpl.nasa.gov/api/horizons.api';
const LOC = {
  shanghai: { latitude: 31.2304, longitude: 121.4737, timeZone: 'Asia/Shanghai' },
  beijing: { latitude: 39.9042, longitude: 116.4074, timeZone: 'Asia/Shanghai' },
  chengdu: { latitude: 30.5728, longitude: 104.0668, timeZone: 'Asia/Shanghai' },
  sydney: { latitude: -33.8688, longitude: 151.2093, timeZone: 'Australia/Sydney' },
};
const jd = (ms) => ms / 86_400_000 + 2440587.5;
const r = (x, k = 4) => Number(x.toFixed(k));
const wrap180 = (d) => ((((d + 180) % 360) + 360) % 360) - 180;

async function horizons(params) {
  const q = new URLSearchParams({
    format: 'json',
    COMMAND: "'10'",
    OBJ_DATA: "'NO'",
    MAKE_EPHEM: "'YES'",
    EPHEM_TYPE: "'OBSERVER'",
    CENTER: "'coord@399'",
    COORD_TYPE: "'GEODETIC'",
    TIME_TYPE: "'UT'",
    QUANTITIES: "'4'",
    ANG_FORMAT: "'DEG'",
    EXTRA_PREC: "'YES'",
    CSV_FORMAT: "'YES'",
    ...params,
  });
  const res = await fetch(`${API}?${q}`);
  if (!res.ok) throw new Error(`Horizons HTTP ${res.status}`);
  const body = await res.json();
  const text = body.result;
  const soe = text.indexOf('$$SOE');
  const eoe = text.indexOf('$$EOE');
  if (soe < 0) throw new Error(`Horizons error: ${text.slice(0, 400)}`);
  const source = /Target body name: .*\{source: ([^}]+)\}/.exec(text)?.[1] ?? '';
  const rows = text
    .slice(soe + 5, eoe)
    .trim()
    .split('\n')
    .map((line) => line.split(',').map((s) => s.trim()))
    .map((cols) => ({ time: cols[0], az: Number(cols[3]), el: Number(cols[4]) }));
  return { source, rows };
}

const site = (loc) => `'${loc.longitude},${loc.latitude},0'`;

// ---------- 1. 视位置抽查 ----------
const POINTS = [
  ['shanghai', '2026-06-21', '12:00'],
  ['shanghai', '2026-06-21', '06:00'],
  ['beijing', '2026-03-20', '06:30'],
  ['chengdu', '2026-06-21', '13:00'],
  ['sydney', '2026-12-21', '18:30'],
];
const positions = [];
for (const [key, date, time] of POINTS) {
  const loc = LOC[key];
  const utcMs = localToUtcMs(date, time, loc.timeZone);
  const h = await horizons({ SITE_COORD: site(loc), TLIST: `'${jd(utcMs).toFixed(9)}'`, TLIST_TYPE: "'JD'", APPARENT: "'REFRACTED'" });
  const t = A.MakeTime(new Date(utcMs));
  const obs = new A.Observer(loc.latitude, loc.longitude, 0);
  const eq = A.Equator(A.Body.Sun, t, obs, true, true);
  const ae = A.Horizon(t, obs, eq.ra, eq.dec, 'normal');
  const [y, m, d] = date.split('-').map(Number);
  const [hh, mm] = time.split(':').map(Number);
  const noaa = noaaSolarPosition({ year: y, month: m, day: d, minutes: hh * 60 + mm, latitude: loc.latitude, longitude: loc.longitude, tzHours: utcOffsetMs(utcMs, loc.timeZone) / 3_600_000 });
  const hz = h.rows[0];
  positions.push({
    location: key, date, local: time, utc: new Date(utcMs).toISOString(), horizonsSource: h.source,
    horizons: { az: hz.az, alt: hz.el },
    ae: { az: r(ae.azimuth, 5), alt: r(ae.altitude, 5) },
    noaa: { az: r(noaa.azimuth, 5), alt: r(noaa.elevation, 5) },
    aeMinusHorizons: { dAz: r(wrap180(ae.azimuth - hz.az), 5), dAlt: r(ae.altitude - hz.el, 5) },
    noaaMinusHorizons: { dAz: r(wrap180(noaa.azimuth - hz.az), 5), dAlt: r(noaa.elevation - hz.el, 5) },
  });
}

// ---------- 2. 日出日落：Horizons 无折射高度逐分钟，插值求太阳中心过 −0.833° ----------
const EVENTS = [
  ['shanghai', '2026-06-21', 'rise'],
  ['sydney', '2026-06-21', 'set'],
];
const events = [];
for (const [key, date, kind] of EVENTS) {
  const loc = LOC[key];
  const [y, m, d] = date.split('-').map(Number);
  const tzHours = utcOffsetMs(localToUtcMs(date, '12:00', loc.timeZone), loc.timeZone) / 3_600_000;
  const noaa = noaaSunriseSunsetUtcMs({ year: y, month: m, day: d, latitude: loc.latitude, longitude: loc.longitude, tzHours });
  const guess = kind === 'rise' ? noaa.rise : noaa.set;
  const start = Math.floor(guess / 60_000) * 60_000 - 10 * 60_000;
  const iso = (ms) => new Date(ms).toISOString().replace('T', ' ').slice(0, 16);
  const h = await horizons({ SITE_COORD: site(loc), START_TIME: `'${iso(start)}'`, STOP_TIME: `'${iso(start + 20 * 60_000)}'`, STEP_SIZE: "'1m'", APPARENT: "'AIRLESS'" });
  let crossing = null;
  for (let i = 1; i < h.rows.length; i++) {
    const a = h.rows[i - 1].el + 0.833;
    const b = h.rows[i].el + 0.833;
    if (a === 0 || Math.sign(a) !== Math.sign(b)) {
      crossing = start + (i - 1 + a / (a - b)) * 60_000;
      break;
    }
  }
  const obs = new A.Observer(loc.latitude, loc.longitude, 0);
  const dir = kind === 'rise' ? +1 : -1;
  const day0 = A.MakeTime(new Date(localToUtcMs(date, '00:00', loc.timeZone)));
  const aeRiseSet = A.SearchRiseSet(A.Body.Sun, obs, dir, day0, 1).date.getTime();
  const aeCenter = A.SearchAltitude(A.Body.Sun, obs, dir, day0, 1, -0.833).date.getTime();
  events.push({
    location: key, date, kind, horizonsSource: h.source,
    horizonsCenterMinus0833: new Date(Math.round(crossing)).toISOString(),
    noaa: new Date(Math.round(guess)).toISOString(),
    aeSearchRiseSet: new Date(aeRiseSet).toISOString(),
    aeSearchAltitude0833: new Date(aeCenter).toISOString(),
    noaaMinusHorizonsSec: r((guess - crossing) / 1000, 1),
    aeSearchRiseSetMinusHorizonsSec: r((aeRiseSet - crossing) / 1000, 1),
    aeSearchAltitude0833MinusHorizonsSec: r((aeCenter - crossing) / 1000, 1),
  });
}

const out = { generated: '2026-10-22', api: API, astronomyEngineVersion: '2.1.19', positions, events };
writeFileSync(join(here, 'horizons-results.json'), `${JSON.stringify(out, null, 2)}\n`);
console.log(JSON.stringify(out, null, 2));
