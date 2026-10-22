// 当地日期 + 当地时刻 + IANA 时区名 → UTC 毫秒。
// 只用 Intl.DateTimeFormat（浏览器和 Node 都有），不读当前时间：
// 本文件里不出现 Date.now() 和无参的 new Date()。
//
// 规则（Epic design「关键机制 / 地点与时间」）：
// - 不存在的本地时刻（时钟向前跳）：按时钟跳过之后的偏移计算。
// - 重复的本地时刻（时钟向后拨）：取较早的那一个。
// 两条合起来等价于 Temporal 的 disambiguation: 'earlier'。
//
// 假设：同一时区在任意 48 小时内最多切换一次偏移（现实 tzdata 中成立）。
// 适用范围：1900-01-01 … 2100-12-31（数据契约的日期范围）。

const DAY_MS = 86_400_000;
const formatterCache = new Map();

/** 取（并缓存）某时区的格式化器。时区名无效时 Intl 抛 RangeError。 */
function formatterFor(timeZone) {
  // timeZone 为 undefined 时 Intl 会取本机时区（结果随机器而变），必须挡住。
  if (typeof timeZone !== 'string') throw new RangeError(`timeZone must be a string: ${String(timeZone)}`);
  let fmt = formatterCache.get(timeZone);
  if (fmt === undefined) {
    fmt = new Intl.DateTimeFormat('en-US', {
      timeZone,
      calendar: 'gregory',
      numberingSystem: 'latn',
      hourCycle: 'h23', // 避免午夜显示成 24 点
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
    formatterCache.set(timeZone, fmt);
  }
  return fmt;
}

/** 某个 UTC 瞬间在该时区的墙上时间，按「假装是 UTC」编码成毫秒。 */
function wallClockMs(utcMs, fmt) {
  const fields = {};
  for (const part of fmt.formatToParts(utcMs)) {
    if (part.type !== 'literal') fields[part.type] = Number(part.value);
  }
  return Date.UTC(fields.year, fields.month - 1, fields.day, fields.hour, fields.minute, fields.second);
}

/** 某个 UTC 瞬间的 UTC 偏移（毫秒，东为正）。精确到秒（LMT 这类偏移带秒）。 */
export function utcOffsetMs(utcMs, timeZone) {
  const fmt = formatterFor(timeZone);
  const whole = Math.floor(utcMs / 1000) * 1000;
  return wallClockMs(whole, fmt) - whole;
}

const DATE_RE = /^(\d{4})-(\d{2})-(\d{2})$/;
const TIME_RE = /^(\d{2}):(\d{2})$/;

/**
 * @param {string} date `YYYY-MM-DD`，当地日期
 * @param {string} localTime `HH:MM`，当地时刻
 * @param {string} timeZone IANA 时区名
 * @returns {number} UTC 毫秒
 */
export function localToUtcMs(date, localTime, timeZone) {
  const dm = DATE_RE.exec(date);
  const tm = TIME_RE.exec(localTime);
  if (!dm || !tm) throw new RangeError(`bad date/time: ${date} ${localTime}`);
  const [y, mo, d] = [Number(dm[1]), Number(dm[2]), Number(dm[3])];
  const [h, mi] = [Number(tm[1]), Number(tm[2])];
  if (h > 23 || mi > 59) throw new RangeError(`bad time: ${localTime}`);
  // 年份 0–99 会被 Date.UTC 当成 19xx；数据契约限定 1900–2100，不会触发。
  const wall = Date.UTC(y, mo - 1, d, h, mi);
  const check = new Date(wall);
  if (check.getUTCFullYear() !== y || check.getUTCMonth() !== mo - 1 || check.getUTCDate() !== d) {
    throw new RangeError(`date does not exist: ${date}`);
  }

  const fmt = formatterFor(timeZone);
  const offsetAt = (utcMs) => {
    const whole = Math.floor(utcMs / 1000) * 1000;
    return wallClockMs(whole, fmt) - whole;
  };
  // 前后各一天的偏移：切换前的偏移、切换后的偏移。
  const before = offsetAt(wall - DAY_MS);
  const after = offsetAt(wall + DAY_MS);
  const candidates = before === after ? [before] : [before, after];
  // 能还原出同一墙上时间的瞬间才是有效解。
  const valid = candidates.map((o) => wall - o).filter((t) => offsetAt(t) === wall - t);
  if (valid.length > 0) return Math.min(...valid); // 重复时刻取较早的那个
  return wall - after; // 不存在的时刻：用跳过之后的偏移
}

/** 退路：时区名在本机 Intl 不可用时，按经度推的偏移（小时）。 */
export function fallbackOffsetHours(longitudeDeg) {
  return Math.round(longitudeDeg / 15);
}
