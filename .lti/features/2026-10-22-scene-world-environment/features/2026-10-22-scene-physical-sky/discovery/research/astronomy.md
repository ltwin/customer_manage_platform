# 天文计算调研：astronomy-engine 的版本、体积、精度与时区换算

- 日期：2026-10-22
- 范围：W2 物理天空的天文计算（Epic design「技术选型 / 天文计算」要求 W2 锁定版本、核对体积）。只调研，不改产品代码。
- 脚本与数据：同级目录 `astronomy/`（见第 8 节）。npm 包装在会话临时目录，没有装进 `frontend/`。

## 结论

1. **锁定 `astronomy-engine@2.1.19`。** 这是 npm 上的最新版，2023-12-14 发布，MIT，零依赖。包里有 ESM 构建并声明 `sideEffects: false`，可以 tree-shake。
2. **体积小。** W2 加 W3 要用的 9 个 API 打包后 56.4 KB（minified），gzip 24.6 KB。整库 106.3 KB，gzip 43.6 KB。
3. **太阳位置远好于验收线。** 太阳在地平线以上时，与 NOAA 的高度差不超过 0.006°，方位差不超过 0.017°（验收线 0.5°）。日出日落时刻差不超过 3.0 秒（验收线 2 分钟）。JPL Horizons 抽查的差值更小。
4. **地平线以下两边的折射模型不同。** astronomy-engine 的 `'normal'` 在地平线以下仍保留约 0.6° 的折射抬升，NOAA 的会迅速衰减到 0。几何高度低于 −2.4° 时，两边的视高度差超过 0.5°（最大 0.55°）。不加折射时，两边差不超过 0.005°。**AC-2 的单元测试要限定在太阳视高度 ≥ 0，或改比几何高度。**
5. **时区换算函数通过全部用例。** 20 个用例在 Node 24、Chromium 145、WebKit 26 下全部通过。两条特殊规则合起来等价于 Temporal 的 `disambiguation: 'earlier'`。
6. **astronomy-engine 不读当前时间。** 源码里没有 `Date.now()`，也没有无参的 `new Date()`。要避开的是我们自己的写法，见第 5 节。
7. **主要风险**：规则「不存在的本地时刻」的字面解读需要 owner 确认；太阳接近天顶时方位不稳定，验收要改用角距；时间参数传 number 会被当成 J2000 起的天数；库维护不活跃。详见第 7 节。

## 1. 版本与包

| 项 | 值 | 来源 |
|---|---|---|
| 最新版本 | 2.1.19（`dist-tags.latest`） | `npm view astronomy-engine` |
| 发布时间 | 2023-12-14T18:20:31Z，距今约 34 个月，满足「至少两周前」 | npm `time` 字段；GitHub release v2.1.19 |
| 许可 | MIT | `package.json`、GitHub 仓库 |
| 运行时依赖 | 无（`package.json` 没有 `dependencies`） | `npm view` |
| 模块格式 | `exports`：`import` → `./esm/astronomy.js`，`require` → `./astronomy.js`，`types` → `./astronomy.d.ts` | `package.json` |
| tree-shake | `sideEffects: false`，ESM 逐个 `export function`；实测按需引入只打进约一半代码（第 2 节） | rolldown 实测 |
| 解包大小 | 1.84 MB（含 README、CJS、浏览器版和 d.ts，不影响产物） | `dist.unpackedSize` |
| 完整性 | `sha512-8yWKNf7UeNbH458h3sAJ6ZgAjE5jTXp/mNNRFoC20j2SHwZIjAQeEsBB2Q3uCFRaTCCJRv33K2XhkhZQMXoX6w==` | `dist.integrity` |
| 维护状态 | 仓库未归档；最后一次 push 2025-01-27；49 个 open issue；2023-12 之后没有新版本 | GitHub API |

- TypeScript：`exports` 里 `types` 条件排在 `import` 之后。TS 先按 `import` 找 `esm/astronomy.d.ts`，找不到后回落到 `astronomy.d.ts`。用前端 `tsconfig.app.json` 的选项（`moduleResolution: bundler`、`strict`、`verbatimModuleSyntax`、`erasableSyntaxOnly`）做类型检查，能通过（tsc 5.9.3）。
- `Body` 是库里 `declare enum` 声明的枚举。在我们的代码里使用它不违反 `erasableSyntaxOnly`。
- 引入时写 `"astronomy-engine": "2.1.19"`（不带 `^`），lockfile 里核对上面的 integrity。

## 2. 包体积

打包工具是前端已装的 rolldown 1.1.4（Vite 8 的底层打包器，minify 用 Oxc）。格式为 ESM。入口把引入的 API 挂到全局，防止被整体摇掉。

| 入口 | 引入的 API | minified | gzip | brotli | 未 minify |
|---|---|---:|---:|---:|---:|
| W2 太阳 | `MakeTime` `Observer` `Equator` `Horizon` `SearchRiseSet` `Body` | 53.8 KB | 23.6 KB | 19.7 KB | 129.3 KB |
| **W2 + W3** | 上面 6 个加 `MoonPhase` `Illumination` `SiderealTime` | **56.4 KB** | **24.6 KB** | 20.7 KB | 143.8 KB |
| W2 + W3 + 星表旋转 | 再加 `Rotation_EQJ_HOR` `RotateVector` `HorizonFromVector` | 58.3 KB | 25.2 KB | 21.1 KB | 154.6 KB |
| 整库 | `import * as Astronomy` | 106.3 KB | 43.6 KB | 36.2 KB | 334.3 KB |
| 参考：包内自带 `astronomy.browser.min.js` | 整库，UMD | 116.4 KB | 46.8 KB | — | — |

- 单位：1 KB = 1000 字节；gzip 用 level 9。
- 太阳一项就占了大头：地球的 VSOP87 表、章动和月亮模型都会被 `Equator(Body.Sun, …)` 用到。W3 加月亮和恒星时只多 1 KB（gzip）。
- design 已约定只在渲染端的代码块里按需加载，24.6 KB（gzip）不会进入编辑器首屏。

## 3. 精度

### 3.1 方法

- **NOAA 参照实现**：`astronomy/noaa-solar.mjs`，逐函数移植自 NOAA 太阳计算器的 `https://gml.noaa.gov/grad/solcalc/main.js`（2026-10-22 下载）。
  - 用到的函数：`calcAzEl`（太阳位置）、`calcSunriseSet`（日出日落，迭代两次）、`calcRefraction`（折射）。
  - 公式出处：Jean Meeus《Astronomical Algorithms》，见 `https://gml.noaa.gov/grad/solcalc/calcdetails.html`。同一页给出折射的分段近似和日出日落的 0.833° 口径。与 `NOAA_Solar_Calculations_day.xls` 是同一套公式；电子表格的儒略日近似只适用于 1901–2099，网页版不用该近似。
  - 移植核对：把原 `main.js` 的计算段放进 Node `vm`，与移植版在 4 地 × 3 天 × 96 个时刻和 24 次日出日落上逐点比较，1176 个值的差全部为 0。
  - NOAA 的折射近似（输入是未折射的高度 h，单位角秒，再除以 3600 得到度）：

    | h | 折射 |
    |---|---|
    | > 85° | 0 |
    | 5°–85° | 58.1 / tan h − 0.07 / tan³ h + 0.000086 / tan⁵ h |
    | −0.575°–5° | 1735 − 518.2 h + 103.4 h² − 12.79 h³ + 0.711 h⁴ |
    | < −0.575° | −20.774 / tan h |

  - 日出日落：太阳中心的几何高度为 −0.833°（34′ 折射加 16′ 视半径），即 `calcHourAngleSunrise` 里的 `cos(90.833°)`。
  - NOAA 页面现在声明「不再维护，不保证准确」。它的网页版用 `moment-timezone-with-data-1970-2030`，1970 年以前和 2030 年以后的当地时间偏移可能不对；页面显示精确到 0.01° 和分钟。所以测试的参照数据取自移植版，不从网页抄。
  - `https://gml.noaa.gov/grad/solcalc/solareqns.PDF` 是另一套更粗的「分数年」近似，计算器本身不用它，本调研也没有用。
- **astronomy-engine 的写法**（`astronomy/compare.mjs` 的 `aeSun`）：

  ```js
  const time = MakeTime(new Date(utcMs));
  const observer = new Observer(lat, lon, 0);
  const eq = Equator(Body.Sun, time, observer, true, true); // 当天赤道坐标 + 光行差
  const hor = Horizon(time, observer, eq.ra, eq.dec, 'normal'); // 视位置
  ```

  `Horizon` 要求输入当天的赤道坐标，所以 `ofdate` 必须是 `true`。
- **方位约定一致**：两边都从正北顺时针计。
  - astronomy-engine：`Horizon` 源码注释为 "degrees eastward from north"。
  - NOAA：`calcAzEl` 在上午（时角为负）给出小于 180° 的方位。
  - 实测：上海 2026-06-21 06:00 三方都是 70.14°（astronomy-engine、NOAA、Horizons）。
- **统计口径**：
  - 高度差：两边的视高度（各自的折射）相减。
  - 几何高度差：两边都不加折射。
  - 方位差：折到 [−180°, 180°]。
  - 角距：两个视方向之间的球面夹角，反映画面上太阳实际偏了多少。
  - 「可见」：两边的视高度都 ≥ 0。

### 3.2 上海 2026-06-21，05:00–20:00 每 15 分钟

共 61 个点，其中 56 个点太阳在地平线以上；不按高度过滤。

| 指标 | 值 |
|---|---|
| 可见时最大高度差 | 0.0054° |
| 可见时最大方位差 | 0.0151° |
| 可见时最大角距 | 0.0055° |
| 全部点的最大几何高度差 | 0.0042° |
| 全部点的最大高度差（含地平线以下） | 0.5515°（19:30，视高度约 −5.5°，见 3.4） |
| 日出（astronomy-engine `SearchRiseSet` / NOAA） | 04:50:26.7 / 04:50:25.2，差 +1.5 s |
| 日落 | 19:01:15.1 / 19:01:17.7，差 −2.6 s |

### 3.3 四地 × 三天，全天每 15 分钟，只留视高度 ≥ −5° 的点

| 地点 | 日期 | 点数（可见） | 可见时最大高度差 | 可见时最大方位差 | 可见时最大角距 | 最大几何高度差 | 含地平线以下的最大高度差 | 日出差 | 日落差 |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| 上海 | 03-20 | 51（49） | 0.0053° | 0.0032° | 0.0055° | 0.0043° | 0.534° | +0.8 s | −0.4 s |
| 上海 | 06-21 | 60（56） | 0.0054° | 0.0151° | 0.0055° | 0.0042° | 0.548° | +1.5 s | −2.6 s |
| 上海 | 12-21 | 44（40） | 0.0055° | 0.0035° | 0.0058° | 0.0048° | 0.547° | −1.4 s | −0.1 s |
| 北京 | 03-20 | 52（48） | 0.0055° | 0.0026° | 0.0056° | 0.0045° | 0.545° | +1.0 s | −0.6 s |
| 北京 | 06-21 | 64（59） | 0.0053° | 0.0074° | 0.0054° | 0.0040° | 0.550° | +1.9 s | −3.0 s |
| 北京 | 12-21 | 41（37） | 0.0053° | 0.0032° | 0.0057° | 0.0045° | 0.545° | −1.6 s | 0.0 s |
| 成都 | 03-20 | 51（48） | 0.0042° | 0.0033° | 0.0046° | 0.0043° | 0.541° | +0.8 s | −0.4 s |
| 成都 | 06-21 | 60（56） | 0.0046° | 0.0164° | 0.0047° | 0.0042° | 0.546° | +1.5 s | −2.6 s |
| 成都 | 12-21 | 44（41） | 0.0051° | 0.0036° | 0.0055° | 0.0048° | 0.549° | −1.4 s | −0.1 s |
| 悉尼 | 03-20 | 51（49） | 0.0031° | 0.0029° | 0.0038° | 0.0019° | 0.533° | +0.2 s | +0.3 s |
| 悉尼 | 06-21 | 43（39） | 0.0042° | 0.0025° | 0.0044° | 0.0040° | 0.546° | +1.5 s | −2.7 s |
| 悉尼 | 12-21 | 62（58） | 0.0058° | 0.0157° | 0.0060° | 0.0050° | 0.553° | −1.4 s | −0.1 s |

- 日出日落差 = astronomy-engine `SearchRiseSet` − NOAA `calcSunriseSet`。
- 悉尼 03-20 和 12-21 为 AEDT（+11），06-21 为 AEST（+10），由时区换算函数给出。
- 最大方位差都出现在正午前后、太阳高度约 80° 时。高度越接近 90°，同样的位置误差在方位上放大得越多（第 7 节）。
- 以「太阳中心几何高度 −0.833°」的 NOAA 口径改用 `SearchAltitude(Body.Sun, observer, ±1, start, 1, -0.833)` 求日出日落，与 NOAA 的差不超过 1.6 s。`SearchRiseSet` 按上缘加 34′ 折射、用当天真实的太阳视半径（15.7′–16.3′），所以与固定 −0.833° 有 1–2 秒的季节性差别。两种写法都远在 2 分钟以内；design 写的「NOAA 口径」直接用 `SearchRiseSet` 即可。

### 3.4 地平线以下的折射分叉

两边在几何高度 −0.575° 以上一致（差不超过 0.002°），以下分叉：

| 几何高度 | astronomy-engine `'normal'` | NOAA | 差 |
|---:|---:|---:|---:|
| 5° | 0.161° | 0.160° | 0.002° |
| 0° | 0.483° | 0.482° | 0.001° |
| −0.575° | 0.574° | 0.575° | −0.001° |
| −0.833° | 0.618° | 0.397° | 0.221° |
| −1° | 0.647° | 0.331° | 0.316° |
| −2° | 0.639° | 0.165° | 0.474° |
| −3° | 0.632° | 0.110° | 0.522° |
| −5° | 0.618° | 0.066° | 0.552° |
| −10° | 0.581° | 0.033° | 0.549° |

- astronomy-engine 的做法：Saemundsson 公式在 −1° 处截断，再随高度线性衰减到天底为 0（`Refraction` 源码）。
- NOAA 的做法：−0.575° 以下改用 −20.774 / tan h，很快趋于 0。
- 差值超过 0.1° 的起点：几何高度 −0.67°（视高度约 −0.08°）。超过 0.5° 的起点：几何高度 −2.43°。
- 地平线以下的折射看不到，两种模型都只是约定。对画面的影响：晨昏蒙影的时间比 NOAA 口径约早亮、晚黑 2–3 分钟（0.55° 除以太阳在地平线附近每分钟约 0.2° 的高度变化）。
- 建议：
  - AC-2 的测试数据点只取太阳视高度 ≥ 0 的时段；地平线以下另比几何高度，阈值仍可写 0.5°（实测 0.005°）。
  - 渲染照常用 `'normal'`（design 的约定不变）。如果 W2 穿刺发现蒙影时序需要更接近 NOAA，可以在地平线以下改用几何方向，这是一行代码的事。

### 3.5 JPL Horizons 第三方抽查

通过 `https://ssd.jpl.nasa.gov/api/horizons.api` 查询（星历 DE441，站心坐标，海拔 0，`APPARENT='REFRACTED'`，方位同样从正北顺时针）。

| 地点 | 当地时刻 | Horizons 方位 / 高度 | astronomy-engine 差（方位 / 高度） | NOAA 差（方位 / 高度） |
|---|---|---|---|---|
| 上海 | 2026-06-21 12:00 | 186.98966° / 82.15527° | +0.0034° / −0.0000° | −0.0118° / +0.0006° |
| 上海 | 2026-06-21 06:00 | 70.13831° / 12.81479° | +0.0001° / +0.0003° | −0.0011° / −0.0012° |
| 北京 | 2026-03-20 06:30 | 91.49349° / 1.69448° | −0.0004° / −0.0003° | −0.0015° / +0.0052° |
| 成都 | 2026-06-21 13:00 | 169.96217° / 82.76327° | +0.0033° / +0.0001° | −0.0132° / +0.0001° |
| 悉尼 | 2026-12-21 18:30 | 253.02128° / 17.41368° | −0.0003° / −0.0003° | +0.0010° / +0.0028° |

日出日落：取 Horizons 无折射高度的逐分钟表，插值求太阳中心过 −0.833° 的时刻。

| 事件 | Horizons | NOAA 差 | `SearchRiseSet` 差 | `SearchAltitude(−0.833)` 差 |
|---|---|---:|---:|---:|
| 上海 2026-06-21 日出 | 2026-06-20T20:50:25.5Z | −0.3 s | +1.2 s | −0.1 s |
| 悉尼 2026-06-21 日落 | 2026-06-21T06:53:48.8Z | +1.2 s | −1.4 s | −0.1 s |

astronomy-engine 比 NOAA 更接近 Horizons，两者都远在验收线以内。

### 3.6 其他精度相关的事实

- 上游宣称的精度：±1 角分（GitHub README，基于 VSOP87 和 NOVAS C 3.1，用 NOVAS 和 JPL Horizons 做单元测试）。
- ΔT（TT − UT）用 Espenak–Meeus 多项式。2026 年给出 75.1 s，实际约 69 s（TT − UTC = 69.184 s，UT1 − UTC 在 ±0.9 s 内）。6 秒误差对太阳的影响小于 0.0001°，对月亮约 3″。
- 库把 UTC 当成 UT1，地球自转相位的误差不超过 0.9 s，即不超过 0.004°。
- 1900 年和 2100 年的输入都能正常计算（上海 6 月 21 日正午的太阳高度分别为 82.156° 和 82.151°）。
- 速度（Node 24，Apple Silicon）：一次太阳视位置约 3 µs，一次月亮视位置约 6 µs，一次 `SearchRiseSet` 约 21 µs。拖动时刻滑杆时逐帧计算没有压力。

## 4. 当地时间 → UTC

### 4.1 函数

`astronomy/local-time.mjs` 的 `localToUtcMs(date, localTime, timeZone)`：

1. 校验 `YYYY-MM-DD` 和 `HH:MM`，日期必须存在。
2. 把当地日期和时刻当成 UTC 编码，得到「墙上时间」`wall`。
3. 用 `Intl.DateTimeFormat`（`hourCycle: 'h23'`，带秒）的 `formatToParts` 求某个瞬间的 UTC 偏移。分别取 `wall − 1 天` 和 `wall + 1 天` 两处的偏移，作为切换前、切换后的偏移。
4. 两个候选瞬间是 `wall − 偏移`。能还原出同一墙上时间的才是有效解。
   - 有一个解：普通时刻。
   - 有两个解：重复时刻，取较早的那个。
   - 没有解：不存在的时刻，用切换之后的偏移，返回 `wall − 切换后偏移`。
5. 格式化器按时区名缓存。时区名不是字符串时直接抛 `RangeError`：传 `undefined` 时 Intl 会取本机时区，结果随机器而变。

- 函数内没有 `Date.now()`，也没有无参的 `new Date()`。测试里把这两者替换成抛错，函数照常工作。
- 偏移精确到秒，所以 1901 年前上海的地方平时 +8:05:43 也能处理。
- 假设同一时区在任意 48 小时内最多切换一次偏移。现实的 tzdata 里成立，包括萨摩亚 2011 年跳过整天的那次。

### 4.2 规则的解读（需要 owner 确认）

design 写的是「不存在的本地时刻：按时钟跳过之后的同一偏移计算」。本函数按字面理解为「用切换之后的偏移」：

- 纽约 2026-03-08 02:30 → 用 EDT（−4）→ 06:30Z。这个瞬间在当地实际显示为 01:30 EST。
- 再加上「重复时刻取较早的」，两条合起来就是 Temporal 的 `disambiguation: 'earlier'`：永远取较早的那个瞬间。规则统一，好解释。

另一种常见约定是 Temporal 的 `'compatible'`（也是 JS `Date`、java.time 和 Python `fold=0` 的做法）。它对不存在的时刻用切换之前的偏移，纽约 02:30 → 07:30Z，当地显示为 03:30 EDT。两种约定只在不存在的时刻上不同，各差一小时：

| 用例 | 本函数（字面解读） | `'compatible'` |
|---|---|---|
| 纽约 2026-03-08 02:30 | 06:30Z（显示 01:30 EST） | 07:30Z（显示 03:30 EDT） |
| 悉尼 2026-10-04 02:30 | 2026-10-03T15:30Z | 2026-10-03T16:30Z |

拖动滑杆经过跳变时，两种约定都会让某一小时的画面与相邻一小时重复，没有哪种更连续。建议保持字面解读，并在 design 里补一句「等价于 Temporal `'earlier'`」，免得以后有人按「向后顺延」改实现。

另外，Go 的 `time.Date` 对这两种时刻的取法会随偏移的正负变化（纽约给 06:30Z / 05:30Z，与本函数一致；悉尼给 16:30Z / 16:30Z，与本函数相反），Go 文档也写明 "not guaranteed"。服务端以后如果要换算 UTC，必须按同一规则自己实现，不能直接用 `time.Date`（`astronomy/go-time-date-check.go`）。

### 4.3 用例与结果

期望值先手算，再用 Python 3.14 `zoneinfo`（系统 tzdata，按 PEP 495 的 `fold` 语义）独立核对。

| 时区 | 当地时刻 | 类别 | 期望 UTC | 说明 |
|---|---|---|---|---|
| America/New_York | 2026-03-08 02:30 | 不存在 | 2026-03-08T06:30:00Z | 02:00 EST 跳到 03:00 EDT，用 −4 |
| America/New_York | 2026-11-01 01:30 | 重复 | 2026-11-01T05:30:00Z | 取较早的 EDT（−4） |
| Australia/Sydney | 2026-04-05 02:30 | 重复 | 2026-04-04T15:30:00Z | 03:00 AEDT 拨回 02:00 AEST，取较早的 +11 |
| Australia/Sydney | 2026-10-04 02:30 | 不存在 | 2026-10-03T15:30:00Z | 02:00 AEST 跳到 03:00 AEDT，用 +11 |
| Asia/Shanghai | 2026-06-21 12:00 | 普通 | 2026-06-21T04:00:00Z | +8 |
| Asia/Shanghai | 2026-06-21 00:00 | 普通 | 2026-06-20T16:00:00Z | 当地零点在 UTC 前一天 |
| Asia/Shanghai | 1988-07-01 12:00 | 普通 | 1988-07-01T03:00:00Z | 中国 1986–1991 实行过夏令时，+9 |
| Asia/Shanghai | 1900-01-01 00:00 | 普通 | 1899-12-31T15:54:17Z | 1901 年前为地方平时 +8:05:43 |
| America/New_York | 1900-01-01 00:00 | 普通 | 1900-01-01T05:00:00Z | EST |
| Asia/Shanghai | 2100-12-31 23:59 | 普通 | 2100-12-31T15:59:00Z | 上边界 |
| America/New_York | 2100-12-31 23:59 | 普通 | 2101-01-01T04:59:00Z | 跨到 UTC 次年 |
| Australia/Sydney | 2100-12-31 12:00 | 普通 | 2100-12-31T01:00:00Z | 夏令时规则外推到 2100 年，+11 |
| Australia/Lord_Howe | 2026-10-04 02:15 | 不存在 | 2026-10-03T15:15:00Z | 半小时的夏令时跳变 |
| Australia/Lord_Howe | 2026-04-05 01:45 | 重复 | 2026-04-04T14:45:00Z | 半小时的重复区间 |
| Pacific/Apia | 2011-12-30 12:00 | 不存在 | 2011-12-29T22:00:00Z | 萨摩亚跳过整天，用 +14 |
| Asia/Kathmandu | 2026-06-21 12:00 | 普通 | 2026-06-21T06:15:00Z | +5:45 |
| America/New_York | 2026-03-08 01:59 / 03:00 | 普通 | 06:59Z / 07:00Z | 跳变前后各一分钟 |
| America/New_York | 2026-11-01 00:59 / 02:00 | 普通 | 04:59Z / 07:00Z | 重复区间前后各一分钟 |

| 环境 | 结果 |
|---|---|
| Node 24.12.0（ICU 77.1，tzdata 2025b），`node --test` | 25 项全部通过（20 个换算用例，加上无效输入、不读当前时间、LMT 偏移、经度退路） |
| Chromium 145（Playwright） | 20 个换算用例全部通过 |
| WebKit 26（Playwright，即 Safari 的引擎） | 20 个换算用例全部通过 |
| Firefox | 本机没有 Playwright 的 Firefox，未测 |

注意：1900 年那条依赖浏览器的 ICU 保留了 1970 年以前的 tzdata。Chromium 和 WebKit 都保留了，结果带 +8:05:43。

### 4.4 时区名无效时的行为

- `new Intl.DateTimeFormat('en-US', { timeZone })` 在名字无效时抛 `RangeError`。V8 和 JavaScriptCore 都如此，消息文本不同。实测会抛的值：`'Mars/Phobos'`、`'Asia/Shangha'`、`'Local'`、`''`。
- `timeZone: undefined` 不抛错，会取本机时区，所以函数自己挡住它。
- Intl 比服务端宽松：
  - 接受小写 `'asia/shanghai'`，并规范化为 `Asia/Shanghai`。
  - 接受 `'+08:00'` 这类偏移写法（Go 的 `LoadLocation` 拒绝它）。
  - Chromium 把 `Europe/Kyiv` 规范化为旧名 `Europe/Kiev`，WebKit 保留新名。两者都能算。
  - 服务端先校验，所以浏览器宽松不会造成问题。
- 服务端的小坑：在 macOS（大小写不敏感的文件系统）上，Go 的 `LoadLocation("asia/shanghai")` 能成功。Go 先查系统的 zoneinfo 目录，嵌入的 `time/tzdata` 只是最后的退路。Linux 的文件系统和嵌入数据都区分大小写，同一个名字预计会失败（按 Go 的查找顺序推断，未在 Linux 实测）。开发机和生产的校验结果会不一致；建议服务端校验时再比较一次 `loc.String() == name` 或查一张规范名表。这不属于本子项的前端范围，记在这里供后端实现时参考。

### 4.5 经度退路「偏移 = round(经度 / 15) 小时」

它算出的是理论上的「航海时区」，不考虑夏令时，也不考虑国家统一用一个时区的情况：

| 地点 | 经度 | round(经度 / 15) | 实际偏移 | 误差 |
|---|---:|---:|---:|---:|
| 上海 | 121.47 | +8 | +8 | 0 |
| 北京 | 116.41 | +8 | +8 | 0 |
| 成都 | 104.07 | +7 | +8 | 1 h |
| 拉萨 | 91.1 | +6 | +8 | 2 h |
| 喀什 | 75.99 | +5 | +8 | 3 h |
| 悉尼 | 151.21 | +10 | +10 / +11（夏令时） | 0 / 1 h |
| 孟买 | 72.88 | +5 | +5:30 | 0.5 h |
| 马德里 | −3.70 | 0 | +1 / +2 | 1–2 h |
| 纽约 | −74.01 | −5 | −5 / −4 | 0 / 1 h |

- 1 小时的偏移误差相当于时角差 15°，日出日落附近的太阳高度会差 10° 左右，画面明显不对。
- 但这条退路几乎不会在上表的地点触发：`Asia/Shanghai` 等常用名所有浏览器都认识。真正可能触发的是近几年新增或改名的时区，例如 `Europe/Kyiv`（2022 年改名）、`America/Ciudad_Juarez`（2022 年新增）、`Pacific/Kanton`（2021 年改名）。这些地点 round(经度 / 15) 多半等于标准时，只差夏令时的 1 小时。
- 结论：作为极少见情况下的降级可以接受，前提是像 design 写的那样在环境选项卡里提示。可以再加两点低成本的改进：
  - 先查一张很小的「新名 → 旧名」别名表（`Europe/Kyiv` → `Europe/Kiev`、`America/Nuuk` → `America/Godthab`、`Pacific/Kanton` → `Pacific/Enderbury`），旧浏览器上用旧名重试。
  - 城市数据里的时区名在打包时用 Chromium 和 WebKit 各跑一遍 Intl 校验。
- 更隐蔽的风险是浏览器的 tzdata 过旧但时区名有效，这时不会抛错，偏移会悄悄算错（例如某国新近取消夏令时）。前端无法检测，只能接受。

## 5. 确定性：不读当前时间

- 在 `esm/astronomy.js`、`astronomy.js`、`astronomy.browser.js` 三个构建里检索：
  - `Date.now`：0 处。
  - 无参 `new Date()`：0 处。
  - `performance.now`、`Math.random`：0 处。
  - 带参数的 `new Date(…)`：4 处，都是由输入推出来的：J2000 常量、由天数构造 `AstroTime`、月相搜索加 6 天、`Seasons(year)` 的起点。
- 所以 astronomy-engine 没有任何 API 会读当前时间。`MakeTime(undefined)` 不会退回「现在」，而是抛错。
- 要避开的是我们自己的写法：
  - 不写 `MakeTime(new Date())`、`new Date()`、`Date.now()`。README 的示例常用 `new Date()` 表示「现在」，照抄会违反约定；contracts 里的静态检查会拦住 `world/` 和 `render/` 里的这两种写法。
  - **时间参数不传 number。** `FlexibleDateTime` 的 number 表示「J2000 起的 UT 天数」，不是毫秒。传 `Date.UTC(…)` 的结果会被当成约 1.8 万亿天，抛出 "Object is too distant for light-travel solver."（本调研踩过一次）。`world/astro.ts` 统一用 `MakeTime(new Date(utcMs))` 生成 `AstroTime`，再把它传给其他 API。
  - 不调用 `SetDeltaTFunction`：它改的是模块级的全局状态，会影响同一页面里所有的计算。
  - W3 不用 `DefineStar` 加载星表：它写全局的 `Body.Star1…Star8` 槽位，只有 8 个。星表用 `Rotation_EQJ_HOR` 一次性旋转（第 6 节）。
- 其他行为：
  - 库里的模块级缓存（`cache_e_tilt`、`sidereal_time_cache`）只按输入时刻记忆结果，不影响确定性。
  - 出错时库抛出的是字符串，不是 `Error`。`astro.ts` 捕获后要自己包装。
  - `AstroTime` 直接持有传入的 `Date` 对象，不要在之后修改它。

## 6. 给 W3 留底：月亮、月相、恒星时

| 用途 | API 与写法 | 说明与精度 |
|---|---|---|
| 月亮地平坐标 | `const eq = Equator(Body.Moon, time, observer, true, true); const hor = Horizon(time, observer, eq.ra, eq.dec, 'normal');` | 必须传 `observer` 得到站心坐标：月亮的视差约 1°。`eq.dist` 为距离（AU，乘 `KM_PER_AU` 得 km）。抽查上海 2026-10-01 21:00，与 Horizons 相差 0.0005°（方位）和 0.0006°（高度） |
| 月相（黄经差） | `MoonPhase(time)` → [0, 360)：0 新月、90 上弦、180 满月、270 下弦 | 地心的日月黄经差，用来区分盈亏（判断亮边朝哪一侧） |
| 相角与被照亮比例 | `const il = Illumination(Body.Moon, time); il.phase_angle; il.phase_fraction; il.mag;` | `phase_angle`：日-月-地夹角，0 为满月。`phase_fraction`：被照亮比例 0–1。两者都是地心值：上例相角 63.25°，Horizons 的站心值为 63.97°；被照亮比例 0.725 对 0.719。月亮在地平线附近时差别最大（约 0.7°），画面上看不出 |
| 格林尼治恒星时 | `SiderealTime(time)` → GAST，单位为恒星时小时，[0, 24) | 视恒星时，含章动 |
| 地方恒星时 | `(SiderealTime(time) + longitude / 15) mod 24` | 上例为 21.783485 h，Horizons 为 21.783478 h，差 0.03 s |
| 星表转地平 | `const rot = Rotation_EQJ_HOR(time, observer); HorizonFromVector(RotateVector(rot, vecJ2000), 'normal')` | 矩阵的地平分量为 x 北、y 西、z 天顶。整张星表可以只算一次矩阵，在着色器里乘；比逐星调用 `Horizon` 快得多。多引入这 3 个 API 只增加 0.5 KB（gzip） |
| 月相时刻搜索（可选） | `SearchMoonPhase(targetLon, start, limitDays)`、`SearchMoonQuarter(start)` | 编辑器若要提供「跳到下一个满月」可用 |

数据见 `astronomy/w3-moon-sidereal-results.json`。

## 7. 风险

1. **「不存在的本地时刻」的规则解读**（第 4.2 节）：字面解读与常见的 `'compatible'` 约定差一小时。需要 owner 确认后写进 design。
2. **验收口径**：
   - AC-2 写的是「高度和方位相差不超过 0.5°」。太阳接近天顶时方位不稳定：广州（23.13°N）2026-06-21 12:29，太阳高度 89.68°，两边方位差 0.37°，而实际角距只有 0.002°。纬度再接近回归线时方位差会超过 0.5°。单元测试建议比角距，或排除高度 85° 以上的点。
   - 地平线以下的折射模型分叉（第 3.4 节）。测试只取视高度 ≥ 0 的点，或改比几何高度。
3. **number 时间参数的陷阱**（第 5 节）：在 `astro.ts` 的类型层面只暴露接收 UTC 毫秒或 `AstroTime` 的包装函数，不把 `FlexibleDateTime` 直接漏给调用方。
4. **库的维护**：2023-12 之后没有新版本，仓库最后一次 push 是 2025-01。好在零依赖、纯计算、已锁定版本，风险主要是遇到 bug 时得自己打补丁。
5. **浏览器 tzdata 过旧**：时区名有效但规则过时，偏移会悄悄算错，前端无法检测（第 4.5 节）。
6. **服务端校验的大小写差异**：macOS 开发机能通过、Linux 生产会拒绝（第 4.4 节）。
7. **NOAA 计算器不再维护**：测试的参照数据以移植版和 Horizons 为准，网页只做人工复核；网页的时区数据只覆盖 1970–2030。

## 8. 脚本与复现

都在 `astronomy/` 下。依赖 astronomy-engine 的脚本用环境变量 `ASTRONOMY_ENGINE` 指向 `esm/astronomy.js`；不设时按包名 import，装进 `frontend` 后可以直接用。

| 文件 | 作用 | 运行 |
|---|---|---|
| `noaa-solar.mjs` | NOAA 太阳位置和日出日落算法的移植，可直接用作单元测试的参照 | 被其他脚本引用 |
| `local-time.mjs` | `localToUtcMs`、`utcOffsetMs`、`fallbackOffsetHours` | 被其他脚本引用 |
| `local-time.cases.mjs` | 20 个时区换算用例和期望值 | 被测试引用 |
| `local-time.test.mjs` | Node 测试（25 项） | `node --test local-time.test.mjs` |
| `local-time.browser-check.mjs` | 在 Chromium 和 WebKit 里跑同一组用例，并记录 Intl 对别名和无效名的行为 | `node local-time.browser-check.mjs`（用 `frontend/node_modules/playwright`） |
| `local-time.browser-results.json` | 上面的结果 | — |
| `go-time-date-check.go` | Go `time.Date` 对特殊时刻的取法 | `go run go-time-date-check.go` |
| `compare.mjs` | astronomy-engine 与 NOAA 的逐点对照 | `ASTRONOMY_ENGINE=… node compare.mjs` |
| `compare-results.json` | 全部数据点（UTC、两边的高度、方位、几何高度、差值、角距）、每组统计和日出日落 | — |
| `horizons.mjs` / `horizons-results.json` | JPL Horizons 抽查（需要联网） | `ASTRONOMY_ENGINE=… node horizons.mjs` |
| `w3-moon-sidereal.mjs` / `w3-moon-sidereal-results.json` | 月亮、月相、恒星时的用法和 Horizons 抽查 | `ASTRONOMY_ENGINE=… node w3-moon-sidereal.mjs` |
| `bundle-size.mjs` / `bundle-size.json` | 用 rolldown 测体积（虚拟入口，不写临时文件） | `ASTRONOMY_ENGINE=… node bundle-size.mjs` |

本次的临时安装位置：会话临时目录下的 `astro/`，执行的是 `npm install astronomy-engine@2.1.19 --save-exact`。仓库的 `frontend/package.json` 和 lockfile 都没有改动。

写进单元测试时：

- 把 `noaa-solar.mjs` 和 `local-time.cases.mjs` 移成测试夹具。
- 从 `compare-results.json` 里挑出每个地点和日期的若干点（含日出日落），连同 `horizons-results.json` 的 5 个点，作为 `world/astro.ts` 的固定数据。
- 测试本身不需要联网。

## 来源

- npm 包：<https://www.npmjs.com/package/astronomy-engine>（`npm view astronomy-engine --json`，2026-10-22）
- 源码与 README：<https://github.com/cosinekitty/astronomy>（v2.1.19；精度声明见 README「accurate to within ±1 arcminute」）
- NOAA 太阳计算器：<https://gml.noaa.gov/grad/solcalc/>；算法源码 <https://gml.noaa.gov/grad/solcalc/main.js>；说明与折射公式 <https://gml.noaa.gov/grad/solcalc/calcdetails.html>；电子表格 <https://gml.noaa.gov/grad/solcalc/NOAA_Solar_Calculations_day.xls>
- JPL Horizons API：<https://ssd.jpl.nasa.gov/api/horizons.api>（DE441）
- Temporal 的 disambiguation 语义：<https://tc39.es/proposal-temporal/docs/zoneddatetime.html>（`'earlier'` / `'compatible'`）
- Python `zoneinfo` 与 PEP 495（`fold`）：<https://peps.python.org/pep-0495/>
