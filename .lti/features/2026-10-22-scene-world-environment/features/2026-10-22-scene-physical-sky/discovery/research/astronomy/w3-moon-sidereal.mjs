// W3 留底：月亮地平坐标、月相、恒星时的 API 用法，并与 JPL Horizons 抽查一个点。
// 运行：ASTRONOMY_ENGINE=<astronomy-engine/esm/astronomy.js 的绝对路径> node w3-moon-sidereal.mjs
// 输出 w3-moon-sidereal-results.json。
import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { localToUtcMs } from './local-time.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const enginePath = process.env.ASTRONOMY_ENGINE;
const A = await import(enginePath ? pathToFileURL(enginePath).href : 'astronomy-engine');

const loc = { latitude: 31.2304, longitude: 121.4737 };
const utcMs = localToUtcMs('2026-10-01', '21:00', 'Asia/Shanghai'); // 2026-10-01 21:00 上海（亏凸月，月亮刚升起）
const time = A.MakeTime(new Date(utcMs));
const observer = new A.Observer(loc.latitude, loc.longitude, 0);

// 月亮：站心（topocentric）赤道坐标 → 地平坐标。月亮视差约 1°，必须传 observer。
const moonEq = A.Equator(A.Body.Moon, time, observer, true, true);
const moonHor = A.Horizon(time, observer, moonEq.ra, moonEq.dec, 'normal');
// 月相：MoonPhase 返回日月黄经差（0 新月、90 上弦、180 满月、270 下弦）
const phaseLon = A.MoonPhase(time);
// Illumination：phase_angle（日-月-地夹角，0 为满）、phase_fraction（被照亮比例 0–1）、mag（星等）
const illum = A.Illumination(A.Body.Moon, time);
// 恒星时：SiderealTime 返回格林尼治视恒星时 GAST（小时，[0, 24)）；地方恒星时 = GAST + 东经 / 15
const gast = A.SiderealTime(time);
const last = (((gast + loc.longitude / 15) % 24) + 24) % 24;
// 星表（J2000 赤道）一次性转到地平：Rotation_EQJ_HOR + RotateVector + HorizonFromVector
const rot = A.Rotation_EQJ_HOR(time, observer);

const local = {
  utc: new Date(utcMs).toISOString(),
  moon: { altitude: moonHor.altitude, azimuth: moonHor.azimuth, distKm: moonEq.dist * A.KM_PER_AU },
  phaseLongitudeDeg: phaseLon,
  phaseAngleDeg: illum.phase_angle,
  phaseFraction: illum.phase_fraction,
  gastHours: gast,
  lastHours: last,
  rotationEqjHorRow0: rot.rot[0],
};

// Horizons：月亮视地平坐标（4）、地方视恒星时（7）、被照亮比例（10）、相角 S-T-O（24）
const q = new URLSearchParams({
  format: 'json', COMMAND: "'301'", OBJ_DATA: "'NO'", MAKE_EPHEM: "'YES'", EPHEM_TYPE: "'OBSERVER'",
  CENTER: "'coord@399'", COORD_TYPE: "'GEODETIC'", SITE_COORD: `'${loc.longitude},${loc.latitude},0'`,
  TLIST: `'${(utcMs / 86_400_000 + 2440587.5).toFixed(9)}'`, TLIST_TYPE: "'JD'", TIME_TYPE: "'UT'",
  QUANTITIES: "'4,7,10,24'", APPARENT: "'REFRACTED'", ANG_FORMAT: "'DEG'", EXTRA_PREC: "'YES'", CSV_FORMAT: "'YES'",
});
let horizons = null;
try {
  const res = await fetch(`https://ssd.jpl.nasa.gov/api/horizons.api?${q}`);
  const text = (await res.json()).result;
  const header = text.slice(0, text.indexOf('$$SOE')).trim().split('\n').filter((l) => l.includes(',')).pop();
  const row = text.slice(text.indexOf('$$SOE') + 5, text.indexOf('$$EOE')).trim();
  horizons = { header: header.split(',').map((s) => s.trim()), row: row.split(',').map((s) => s.trim()) };
} catch (e) {
  horizons = { error: String(e) };
}

const out = { generated: '2026-10-22', astronomyEngineVersion: '2.1.19', location: loc, local, horizons };
writeFileSync(join(here, 'w3-moon-sidereal-results.json'), `${JSON.stringify(out, null, 2)}\n`);
console.log(JSON.stringify(out, null, 2));
