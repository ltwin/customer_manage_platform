// NOAA 太阳计算器的太阳位置与日出日落算法，逐函数移植自
//   https://gml.noaa.gov/grad/solcalc/main.js        （2026-10-22 下载）
// 公式出处：Jean Meeus《Astronomical Algorithms》，见
//   https://gml.noaa.gov/grad/solcalc/calcdetails.html（含大气折射分段近似、日出日落 0.833° 口径）
// 与 NOAA 的 NOAA_Solar_Calculations_day.xls 同一套公式；电子表格的儒略日近似只适用于 1901–2099，
// 网页版（本移植）不用该近似。
// 只做了三处改动：去掉 DOM / 全局变量；去掉 isNumber 改用 Number.isFinite；导出纯函数。
// 约定：经度东为正，时区偏移东为正（小时），方位从正北顺时针。

const degToRad = (d) => (Math.PI * d) / 180;
const radToDeg = (r) => (180 * r) / Math.PI;

export function calcTimeJulianCent(jd) {
  return (jd - 2451545.0) / 36525.0;
}

function calcDateFromJD(jd) {
  const z = Math.floor(jd + 0.5);
  const f = jd + 0.5 - z;
  let A;
  if (z < 2299161) {
    A = z;
  } else {
    const alpha = Math.floor((z - 1867216.25) / 36524.25);
    A = z + 1 + alpha - Math.floor(alpha / 4);
  }
  const B = A + 1524;
  const C = Math.floor((B - 122.1) / 365.25);
  const D = Math.floor(365.25 * C);
  const E = Math.floor((B - D) / 30.6001);
  const day = B - D - Math.floor(30.6001 * E) + f;
  const month = E < 14 ? E - 1 : E - 13;
  const year = month > 2 ? C - 4716 : C - 4715;
  return { year, month, day };
}

function calcGeomMeanLongSun(t) {
  let L0 = 280.46646 + t * (36000.76983 + t * 0.0003032);
  while (L0 > 360.0) L0 -= 360.0;
  while (L0 < 0.0) L0 += 360.0;
  return L0;
}
function calcGeomMeanAnomalySun(t) {
  return 357.52911 + t * (35999.05029 - 0.0001537 * t);
}
function calcEccentricityEarthOrbit(t) {
  return 0.016708634 - t * (0.000042037 + 0.0000001267 * t);
}
function calcSunEqOfCenter(t) {
  const mrad = degToRad(calcGeomMeanAnomalySun(t));
  return (
    Math.sin(mrad) * (1.914602 - t * (0.004817 + 0.000014 * t)) +
    Math.sin(2 * mrad) * (0.019993 - 0.000101 * t) +
    Math.sin(3 * mrad) * 0.000289
  );
}
function calcSunTrueLong(t) {
  return calcGeomMeanLongSun(t) + calcSunEqOfCenter(t);
}
function calcSunApparentLong(t) {
  const omega = 125.04 - 1934.136 * t;
  return calcSunTrueLong(t) - 0.00569 - 0.00478 * Math.sin(degToRad(omega));
}
function calcMeanObliquityOfEcliptic(t) {
  const seconds = 21.448 - t * (46.815 + t * (0.00059 - t * 0.001813));
  return 23.0 + (26.0 + seconds / 60.0) / 60.0;
}
function calcObliquityCorrection(t) {
  const omega = 125.04 - 1934.136 * t;
  return calcMeanObliquityOfEcliptic(t) + 0.00256 * Math.cos(degToRad(omega));
}
export function calcSunDeclination(t) {
  const e = calcObliquityCorrection(t);
  const lambda = calcSunApparentLong(t);
  return radToDeg(Math.asin(Math.sin(degToRad(e)) * Math.sin(degToRad(lambda))));
}
export function calcEquationOfTime(t) {
  const epsilon = calcObliquityCorrection(t);
  const l0 = calcGeomMeanLongSun(t);
  const e = calcEccentricityEarthOrbit(t);
  const m = calcGeomMeanAnomalySun(t);
  let y = Math.tan(degToRad(epsilon) / 2.0);
  y *= y;
  const sin2l0 = Math.sin(2.0 * degToRad(l0));
  const sinm = Math.sin(degToRad(m));
  const cos2l0 = Math.cos(2.0 * degToRad(l0));
  const sin4l0 = Math.sin(4.0 * degToRad(l0));
  const sin2m = Math.sin(2.0 * degToRad(m));
  const Etime =
    y * sin2l0 - 2.0 * e * sinm + 4.0 * e * y * sinm * cos2l0 - 0.5 * y * y * sin4l0 - 1.25 * e * e * sin2m;
  return radToDeg(Etime) * 4.0; // 分钟
}
function calcHourAngleSunrise(lat, solarDec) {
  const latRad = degToRad(lat);
  const sdRad = degToRad(solarDec);
  const HAarg = Math.cos(degToRad(90.833)) / (Math.cos(latRad) * Math.cos(sdRad)) - Math.tan(latRad) * Math.tan(sdRad);
  return Math.acos(HAarg); // 弧度；日落取负
}

/** NOAA 的格里历 → 儒略日（当天 0h UT）。 */
export function getJD(year, month, day) {
  if (month <= 2) {
    year -= 1;
    month += 12;
  }
  const A = Math.floor(year / 100);
  const B = 2 - A + Math.floor(A / 4);
  return Math.floor(365.25 * (year + 4716)) + Math.floor(30.6001 * (month + 1)) + day + B - 1524.5;
}

/** NOAA 的大气折射近似（度），输入是未折射的高度（度）。 */
export function calcRefraction(elev) {
  if (elev > 85.0) return 0.0;
  const te = Math.tan(degToRad(elev));
  let correction;
  if (elev > 5.0) {
    correction = 58.1 / te - 0.07 / (te * te * te) + 0.000086 / (te * te * te * te * te);
  } else if (elev > -0.575) {
    correction = 1735.0 + elev * (-518.2 + elev * (103.4 + elev * (-12.79 + elev * 0.711)));
  } else {
    correction = -20.774 / te;
  }
  return correction / 3600.0;
}

/**
 * NOAA calcAzEl。T 为儒略世纪（以 UT 计，NOAA 不加 ΔT）；localtime 为当地分钟；zone 为 UTC 偏移小时。
 * 返回方位（从正北顺时针）、含折射的高度、未折射的高度。
 */
export function calcAzEl(T, localtime, latitude, longitude, zone) {
  const eqTime = calcEquationOfTime(T);
  const theta = calcSunDeclination(T);
  const solarTimeFix = eqTime + 4.0 * longitude - 60.0 * zone;
  let trueSolarTime = localtime + solarTimeFix;
  while (trueSolarTime > 1440) trueSolarTime -= 1440;
  let hourAngle = trueSolarTime / 4.0 - 180.0;
  if (hourAngle < -180) hourAngle += 360.0;
  const haRad = degToRad(hourAngle);
  let csz =
    Math.sin(degToRad(latitude)) * Math.sin(degToRad(theta)) +
    Math.cos(degToRad(latitude)) * Math.cos(degToRad(theta)) * Math.cos(haRad);
  if (csz > 1.0) csz = 1.0;
  else if (csz < -1.0) csz = -1.0;
  const zenith = radToDeg(Math.acos(csz));
  const azDenom = Math.cos(degToRad(latitude)) * Math.sin(degToRad(zenith));
  let azimuth;
  if (Math.abs(azDenom) > 0.001) {
    let azRad = (Math.sin(degToRad(latitude)) * Math.cos(degToRad(zenith)) - Math.sin(degToRad(theta))) / azDenom;
    if (Math.abs(azRad) > 1.0) azRad = azRad < 0 ? -1.0 : 1.0;
    azimuth = 180.0 - radToDeg(Math.acos(azRad));
    if (hourAngle > 0.0) azimuth = -azimuth;
  } else {
    azimuth = latitude > 0.0 ? 180.0 : 0.0;
  }
  if (azimuth < 0.0) azimuth += 360.0;
  const exoatmElevation = 90.0 - zenith;
  const refractionCorrection = calcRefraction(exoatmElevation);
  const elevation = 90.0 - (zenith - refractionCorrection);
  return { azimuth, elevation, exoatmElevation, declination: theta, eqTime };
}

function calcSunriseSetUTC(rise, JD, latitude, longitude) {
  const t = calcTimeJulianCent(JD);
  const eqTime = calcEquationOfTime(t);
  const solarDec = calcSunDeclination(t);
  let hourAngle = calcHourAngleSunrise(latitude, solarDec);
  if (!rise) hourAngle = -hourAngle;
  const delta = longitude + radToDeg(hourAngle);
  return 720 - 4.0 * delta - eqTime; // 分钟
}

/** NOAA calcSunriseSet（不含极昼极夜的前后搜索分支）。返回 { jday, timelocal }，timelocal 为当地分钟。 */
export function calcSunriseSet(rise, JD, latitude, longitude, timezone) {
  const timeUTC = calcSunriseSetUTC(rise, JD, latitude, longitude);
  const newTimeUTC = calcSunriseSetUTC(rise, JD + timeUTC / 1440.0, latitude, longitude);
  if (!Number.isFinite(newTimeUTC)) return null; // 极昼或极夜
  let timeLocal = newTimeUTC + timezone * 60.0;
  let jday = JD;
  if (timeLocal < 0.0 || timeLocal >= 1440.0) {
    const increment = timeLocal < 0 ? 1 : -1;
    while (timeLocal < 0.0 || timeLocal >= 1440.0) {
      timeLocal += increment * 1440.0;
      jday -= increment;
    }
  }
  return { jday, timelocal: timeLocal };
}

// ---------- 便于对照的包装（按 NOAA 网页 calculate() 的取值方式组装输入） ----------

/** 太阳位置：当地年月日 + 当地分钟 + UTC 偏移（小时）。 */
export function noaaSolarPosition({ year, month, day, minutes, latitude, longitude, tzHours }) {
  const jday = getJD(year, month, day);
  const T = calcTimeJulianCent(jday + minutes / 1440.0 - tzHours / 24.0);
  return calcAzEl(T, minutes, latitude, longitude, tzHours);
}

/** 日出 / 日落的 UTC 毫秒。 */
export function noaaSunriseSunsetUtcMs({ year, month, day, latitude, longitude, tzHours }) {
  const JD = getJD(year, month, day);
  const toMs = (r) => (r === null ? null : (r.jday - 2440587.5) * 86_400_000 + (r.timelocal - tzHours * 60) * 60_000);
  return {
    rise: toMs(calcSunriseSet(true, JD, latitude, longitude, tzHours)),
    set: toMs(calcSunriseSet(false, JD, latitude, longitude, tzHours)),
  };
}

export { calcDateFromJD };
