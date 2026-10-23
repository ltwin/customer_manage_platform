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

## astronomy-engine（太阳、月亮与星空的位置）

- **用在哪里**：3D 场景编辑器物理天空的太阳位置与日出日落，W3 起还有月亮的位置、月相与月出月落、恒星时与坐标旋转，`frontend/src/creative-scene/world/astro.ts`。
- **版本**：2.1.19（npm `astronomy-engine`）。
- **许可证**：MIT，Copyright (c) 2019-2023 Don Cross <cosinekitty@gmail.com>。许可证全文随包分发（`astronomy.js` 文件头）。

## Yale 亮星星表（BSC5，星空）

- **用在哪里**：3D 场景编辑器物理天空的星星，`frontend/src/creative-scene/world/nightSky/stars.bin`（视星等不超过 6.5 的 8404 颗，按列打包），由 `frontend/scripts/night-sky/` 的脚本从原始星表生成。
- **来源**：Hoffleit, D. & Warren Jr., W. H. 1991, *The Bright Star Catalogue, 5th Revised Ed.*，经 CDS/VizieR（目录 V/50）分发，<https://cdsarc.cds.unistra.fr/viz-bin/cat/V/50>。
- **许可**：CDS 与 Harvard 的发布页没有许可或版权声明；按 CDS「先看 ReadMe」的规则可以打包使用，并按惯例署名引用（W3 调研，owner 2026-10-23 接受这一判断）。
- **改动**：只取赤经、赤纬、V 星等和 B−V 四列，缺 B−V 的星按 0.6 处理。
- **界面上的署名**：「环境」选项卡「夜空」一组底部一行「星表：Yale 亮星星表（Hoffleit & Warren 1991，经 CDS/VizieR）；月面：NASA SVS」。

## NASA SVS CGI Moon Kit（月面贴图）

- **用在哪里**：3D 场景编辑器物理天空的月盘纹理，`frontend/src/creative-scene/world/nightSky/moon.webp`。
- **来源**：NASA Goddard Space Flight Center Scientific Visualization Studio，CGI Moon Kit（ID 4720），<https://svs.gsfc.nasa.gov/4720>；底层数据为 LRO LROC。
- **许可**：公有领域（NASA 的影像与数据通常不受版权保护），在此致谢 NASA/GSFC SVS。
- **改动**：彩色图 `lroc_color_2k.jpg` 降到 1024 × 512、转为灰度、均值归一，编码为 WebP（来源地址与校验和见 `frontend/scripts/night-sky/build-moon.mjs`）。

## 银河

- 银河由渲染端程序化生成（`frontend/src/creative-scene/world/nightSky/milkyWay.ts`），不使用第三方图像。
