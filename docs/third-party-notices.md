# 第三方数据与代码声明

这里记录随前端分发、需要署名或保留声明的第三方数据和代码。npm 依赖的许可证以各自包内为准；本文件只收有署名义务或我们改过的那些。

## GeoNames（城市数据）

- **用在哪里**：3D 场景编辑器物理天空的城市搜索，`frontend/src/creative-scene/world/cities.json`，由 `frontend/scripts/cities/build.mjs` 生成。
- **来源**：GeoNames，<https://www.geonames.org/>，`cities15000`（每日导出，取数日期见 `cities.json` 的 `attribution.fetched`）。
- **许可证**：Creative Commons Attribution 4.0（CC BY 4.0），<https://creativecommons.org/licenses/by/4.0/>。
- **我们的修改**：
  - 只留名称、坐标、时区和国家代码，坐标取到 0.01°，人口只用来排序，不入包；
  - 中文名取自 Wikidata 标签（CC0）和 GeoNames 的中文别名；
  - 按补丁表 `frontend/scripts/cities/patches.json` 补了地点、改了名称和坐标；
  - 新疆的条目时区改为 `Asia/Shanghai`。
- **免责声明**（GeoNames 原文）：The Data is provided "as is" without warranty or any representation of accuracy, timeliness or completeness.
- **界面上的署名**：城市搜索下拉底部一行「城市数据：GeoNames（CC BY 4.0），已修改」，链接到来源和许可证。

## Wikidata（城市的中文名）

- **用在哪里**：同上，城市的简体和繁体中文名。
- **许可证**：CC0（<https://www.wikidata.org/wiki/Wikidata:Licensing>），没有署名义务，在此致谢。

## astronomy-engine（太阳位置）

- **用在哪里**：3D 场景编辑器物理天空的太阳位置与日出日落，`frontend/src/creative-scene/world/astro.ts`。
- **版本**：2.1.19（npm `astronomy-engine`）。
- **许可证**：MIT，Copyright (c) 2019-2023 Don Cross <cosinekitty@gmail.com>。许可证全文随包分发（`astronomy.js` 文件头）。
