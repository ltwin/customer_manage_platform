// 运行：node --test local-time.test.mjs
// 期望值手算得出，并用 Python zoneinfo（PEP 495 fold 语义）独立核对过，见 astronomy.md「时区换算」。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { localToUtcMs, utcOffsetMs, fallbackOffsetHours } from './local-time.mjs';
import { CASES } from './local-time.cases.mjs';

for (const [tz, date, time, expected, kind, note] of CASES) {
  test(`${tz} ${date} ${time} (${kind}) ${note}`, () => {
    assert.equal(new Date(localToUtcMs(date, time, tz)).toISOString(), expected);
  });
}

test('无效时区名：Intl 抛 RangeError', () => {
  for (const tz of ['Mars/Phobos', 'Asia/Shangha', 'Local', '']) {
    assert.throws(() => localToUtcMs('2026-06-21', '12:00', tz), RangeError, tz);
  }
  // undefined 会让 Intl 取本机时区，函数必须自己挡住
  assert.throws(() => localToUtcMs('2026-06-21', '12:00', undefined), RangeError);
});

test('不存在的日期和时刻：抛 RangeError', () => {
  assert.throws(() => localToUtcMs('2026-02-29', '12:00', 'Asia/Shanghai'), RangeError);
  assert.throws(() => localToUtcMs('2026-06-21', '24:00', 'Asia/Shanghai'), RangeError);
  assert.throws(() => localToUtcMs('2026-6-21', '12:00', 'Asia/Shanghai'), RangeError);
});

test('不读当前时间：Date.now 和无参 new Date 被替换成抛错后仍能算', () => {
  const RealDate = globalThis.Date;
  class GuardDate extends RealDate {
    constructor(...args) {
      if (args.length === 0) throw new Error('new Date() without args');
      super(...args);
    }
    static now() {
      throw new Error('Date.now()');
    }
  }
  globalThis.Date = GuardDate;
  try {
    assert.equal(localToUtcMs('2026-03-08', '02:30', 'Europe/Berlin'), RealDate.UTC(2026, 2, 8, 1, 30));
  } finally {
    globalThis.Date = RealDate;
  }
});

test('utcOffsetMs 能取到带秒的 LMT 偏移', () => {
  assert.equal(utcOffsetMs(Date.UTC(1899, 11, 31, 16), 'Asia/Shanghai'), (8 * 3600 + 5 * 60 + 43) * 1000);
});

test('经度退路 round(经度 / 15)', () => {
  assert.equal(fallbackOffsetHours(121.4737), 8); // 上海
  assert.equal(fallbackOffsetHours(104.0668), 7); // 成都：比北京时间少 1 小时
  assert.equal(fallbackOffsetHours(151.2093), 10); // 悉尼：不含夏令时
});
