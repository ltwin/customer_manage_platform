# 夜空资产调研：星表、银河全天图、月面贴图的授权与体积

> 调研日期：2026-10-22。场景：商业闭源 SaaS，静态资源随前端打包分发（three.js r186，WebGPU + WebGL2，TSL）。
> 只写已核实的事实；推断与法律判断单独标注「判断」，查不到的标「未核实」。本文不是法律意见，正式上线前建议让法务过一遍署名页文案。

## 结论

- **星表首选：Yale Bright Star Catalogue 5th Revised Ed.（BSC5，CDS V/50）。** 只要 Hipparcos、Tycho-2、Gaia 这三家，CDS ReadMe 和 ESA 官方页现在都标的是 **CC BY-NC 3.0 IGO**，商业使用要先拿到 ESA 的授权。HYG 用的是 CC BY-SA 4.0（v4.x），v3.x 及更早是 CC BY-SA 2.5，带 ShareAlike，而且上游掺了 Hipparcos。BSC5 的 CDS ReadMe 没有任何 License 或 Copyright 行，Harvard 的发布页也没写许可条款；CDS 的规定是「先看 ReadMe 有没有 copyright」。照这条规定读，BSC5 没有声明限制。商业打包可以做，但要在产品的署名或开源声明页里引用 Hoffleit & Warren 1991 和 CDS/VizieR（这是 CDS 的要求，不是许可证条款）。
- **星表体积：** BSC5 里 V ≤ 6.5 的共 **8,404 颗**。按 f32 RA/Dec + u8 Vmag + u8 B−V、SoA 排布，原始 82 KiB，gzip 后 **约 67 KiB**，brotli 后约 65 KiB。同样内容写成 JSON 数组，原始 237 KiB，gzip 后约 89 KiB。
- **银河首选：NASA SVS「Deep Star Maps 2020」（ID 4851）的 `milkyway_2020_*` 无亮星版。** SVS 帮助页的原话是「All of our content is in the public domain (unless otherwise noted)」，NASA 媒体指南明确允许把它用作「texture maps」。这一版去掉了 Hipparcos 和 Tycho 的亮星，只剩更暗的 Gaia DR2/UCAC3 背景星，所以不会和星表重复绘制。署名写「NASA/Goddard Space Flight Center Scientific Visualization Studio. Gaia DR2: ESA/Gaia/DPAC」，不得暗示 NASA 背书。**剩余风险：** 贴图由 Gaia DR2 星点渲染而来，Gaia 数据现行许可是 CC BY-NC 3.0 IGO。NASA 页面只把 Gaia 写成署名（credit），没标成第三方版权，所以这条链在法律上是否需要处理，**未核实**（建议法务确认，或邮件问 ESA data.licences@esa.int）。
- **银河备选：ESO「The Milky Way panorama」（eso0932a，ESO/S. Brunier）。** 许可是 CC BY 4.0，可以商用、可以改作，但署名必须「clear and visible」、文字不得改动，改动也要注明。它是照片，含全部前景亮星（会和星表重复），像素 6000×3000。其余候选都不适合：Gaia 全天图是 CC BY-SA 3.0 IGO，底层数据又是 BY-NC；Mellinger 的商用需要付费授权；Stellarium 的贴图来自 Mellinger 授予 Stellarium 的许可，不能转授给我们。
- **银河分辨率：** 推荐 **2048×1024**，先做一次 σ≈1.5 px 的高斯模糊去掉残留星点，实测 **WebP q80 约 141 KiB、AVIF q60 约 124 KiB**。不模糊的 WebP 是 342 KiB，4096×2048 不模糊的 WebP 是 2.1 MiB。2K 每像素约 10.5′，8K 成片 60° 视场下大约放大 22 倍。银河是低频的面亮度，夜景下又暗，这个放大倍数可以接受。
- **月面首选：NASA SVS「CGI Moon Kit」（ID 4720）。** 本身是公有领域，色彩图取自 LROC 的 PDS 产品 Hapke Normalized WAC Mosaic，LROC 官方条款写明「PDS…are in the public domain」。可以降采样、转灰度。用 1024×512 灰度 WebP q80，**约 49 KiB**。8K 成片里月盘约 80 px，1K 贴图在月盘中心大约有 4 倍过采样，够用；2K 版（WebP q80 约 206 KiB）留给长焦特写。**备选：** USGS 的 Clementine UVVIS 750 nm 全球镶嵌，元数据写的是「Use Constraints: Public domain」，本身就是灰度反照率图。
- **月面朝向：** `astronomy-engine`（MIT，项目已依赖 2.1.19）提供 `RotationAxis(Body.Moon, date)`，返回极轴的 RA/Dec 和本初子午线角 `spin`（IAU WGCCRE 2015），还提供 `Libration(date)`，返回 `elat/elon/mlat/mlon/dist_km/diam_deg`。另有 `Rotation_EQJ_GAL()`，可以把银道坐标的银河图转到赤道坐标。

## 来源（访问日期均为 2026-10-22）

| # | 来源 | URL | 备注 |
|---|---|---|---|
| S1 | CDS ReadMe：V/50 Bright Star Catalogue 5th Rev. Ed. | https://cdsarc.cds.unistra.fr/ftp/V/50/ReadMe | 没有 License 或 Copyright 行；Hoffleit D., Warren Jr W.H., NSSDC/ADC 1991；catalog 共 9110 条记录 |
| S2 | CDS VizieR Licence 页 | https://cds.unistra.fr/vizier-org/licences_vizier.html | 原文：「The commercial usage of the data is subject to rules depending of the origin」「Please refer to the ReadMe file associated to the catalogue to verify if a "copyright" exists」 |
| S3 | Harvard/SAO TDC BSC5 发布页 | http://tdc-www.harvard.edu/catalogs/bsc5.html | 只有文件说明（9,110 颗星，BSC5 二进制 291,548 B），没有许可条款 |
| S4 | HEASARC BSC5P | https://heasarc.gsfc.nasa.gov/W3Browse/star-catalog/bsc5p.html | Provenance 说明 HEASARC 1995 年从 ADC 或 CDS 取得；没有许可条款 |
| S5 | CDS ReadMe：I/239 Hipparcos & Tycho | https://cdsarc.cds.unistra.fr/ftp/I/239/ReadMe | 原文：`License: CC-BY-NC-3.0 IGO` |
| S6 | CDS ReadMe：I/259 Tycho-2 | https://cdsarc.cds.unistra.fr/ftp/I/259/ReadMe | 原文：`License: CC-BY-NC-3.0 IGO` |
| S7 | ESA Gaia Data License | https://www.cosmos.esa.int/web/gaia-users/license | 原文：「Gaia data are distributed under the CC BY-NC 3.0 IGO license.」 |
| S8 | ESA ESDC：Terms and Conditions for the use of data in the ESA Space Science Archives | https://www.cosmos.esa.int/web/esdc/terms-and-conditions | 原文：「Prior to any commercial use … a detailed request for authorisation/licence shall be made … data.licences@esa.int」；credit 写法：Gaia 为「ESA, Gaia DPAC」，Hipparcos 为「ESA」 |
| S9 | HYG 仓库 README 和 LICENSE（Codeberg） | https://codeberg.org/astronexus/hyg （raw：`/raw/branch/main/README.md`、`/LICENSE`） | 原文：「Versions since v4.0 are licensed as above (CC-BY-SA 4.0). Earlier versions use the CC-BY-SA v2.5 license.」；当前版本 v4.4 |
| S10 | NASA SVS 4851 Deep Star Maps 2020 | https://svs.gsfc.nasa.gov/4851 | 访问时官网返回 503；页面内容取自 Wayback 快照 20260101173237（`web.archive.org/web/20260101173237/https://svs.gsfc.nasa.gov/4851/`）。资产文件已从官网直接下载成功 |
| S11 | NASA SVS Help / FAQ | https://svs.gsfc.nasa.gov/help/ | 取自 Wayback 快照 20261017104718。原文：「All of our content is in the public domain (unless otherwise noted), meaning that it is free to download, use, and redistribute for whatever purposes you see fit.」 |
| S12 | NASA Images and Media Usage Guidelines | https://www.nasa.gov/nasa-brand-center/images-and-media/ | 原文：「files used in the rendition of 3-dimensional models, such as texture maps … generally are not subject to copyright in the United States」「If the NASA material is to be used for commercial purposes … it must not explicitly or implicitly convey NASA's endorsement」「NASA should be acknowledged as the source of the material.」 |
| S13 | ESO Copyright Notice | https://www.eso.org/public/copyright/ | 访问时 ESO 官网在维护（Temporarily unavailable），内容取自 Wayback 快照 20261007132036。原文：「licensed under a Creative Commons Attribution 4.0 International License … provided the credit is clear and visible」「The license allows to modify ESO material, provided that you indicate what those modifications are.」 |
| S14 | ESO eso0932a The Milky Way panorama | https://www.eso.org/public/images/eso0932a/ | 同上，取自 Wayback 2026 快照。Credit：ESO/S. Brunier；6000×3000 px；Fullsize Original 27.7 MB |
| S15 | ESA/Gaia：The colour of the sky from Gaia's EDR3（equirectangular） | https://sci.esa.int/web/gaia/-/the-colour-of-the-sky-from-gaia-s-early-data-release-3-equirectangular-projection | 「Copyright: ESA/Gaia/DPAC; CC BY-SA 3.0 IGO. Acknowledgement: A. Moitinho」；提供 2k/4k/16k PNG |
| S16 | ESA/Gaia：Gaia's sky in colour（DR2，equirectangular） | https://sci.esa.int/web/gaia/-/60196-gaia-s-sky-in-colour-equirectangular-projection | 「Copyright: ESA/Gaia/DPAC, CC BY-SA 3.0 IGO」 |
| S17 | ESA 网站图片和视频使用条款 | https://www.esa.int/ESA_Multimedia/Copyright_Notice_Images | 第 (iv) 条：CC BY-SA 3.0 IGO 素材按该许可使用；第 (v) 条：其余素材商用需另行书面授权。最后更新 2024-01-18 |
| S18 | Axel Mellinger Milky Way Panorama 2.0：licenses 页 | http://www.milkywaysky.com/licenses.html | 原文：「Higher resolutions, commercial & educational use: In most cases, this will require payment of a license fee」 |
| S19 | Stellarium CREDITS.md | https://github.com/Stellarium/stellarium/blob/master/CREDITS.md | 4.4 节：银河全景出自 Axel Mellinger，「License: permission given to "Modify and redistribute this image if proper credit to the original image is given."」；Stellarium 主体为 GPL v2（COPYING） |
| S20 | NASA SVS 4720 CGI Moon Kit | https://svs.gsfc.nasa.gov/4720 | 页面取自 Wayback 快照 20261014135139，资产文件从官网直接下载。Credit：「NASA's Scientific Visualization Studio」 |
| S21 | LROC Terms of Use | https://lroc.im-ldi.com/about/terms | 原文：「Data products available through the LROC Data Node of the PDS or through NASA PDS interfaces are in the public domain」「All commercial use of LROC images not downloaded from the LROC PDS archive require prior permission.」 |
| S22 | LROC WAC_HAPKE README（PDS） | https://pds.lroc.asu.edu/data/LRO-L-LROC-5-RDR-V1.0/LROLRC_2001/DATA/MDR/WAC_HAPKE/WAC_HAPKE_README.TXT | SVS 4720 色彩图注明的来源（该链接从 S20 页面取得，README 内容本次没有逐字阅读） |
| S23 | USGS Astropedia：Moon Clementine UVVIS Global Mosaic 118m | https://astrogeology.usgs.gov/search/map/moon_clementine_uvvis_global_mosaic_118m | 「Access Constraints: None」「Use Constraints: Public domain」；灰度 750 nm 反照率；全图 4 GB |
| S24 | USGS Astropedia：Moon LRO LROC WAC Global Morphology Mosaic 100m | https://astrogeology.usgs.gov/search/map/moon_lro_lroc_wac_global_morphology_mosaic_100m | 「Access Constraints: public domain」「Use Constraints: Please cite authors」；入射角「generally angles between 55-75 degrees」 |
| S25 | USGS Copyrights and Credits | https://www.usgs.gov/information-policies-and-instructions/copyrights-and-credits | 原文：「USGS-authored or produced data and information are considered to be in the U.S. Public Domain.」 |
| S26 | astronomy-engine JS API 文档和 npm 包 2.1.19 | https://github.com/cosinekitty/astronomy/blob/master/source/js/README.md ；https://registry.npmjs.org/astronomy-engine/-/astronomy-engine-2.1.19.tgz | 已在 2.1.19 的 `.d.ts` 里确认 `RotationAxis`、`AxisInfo`、`Libration`、`LibrationInfo`、`Rotation_EQJ_GAL`、`Rotation_GAL_EQJ`；LICENSE 为 MIT |

## 一、星表

### 1.1 许可对比

| 候选 | 许可（原文出处） | 商业闭源打包 | 署名 | 判断 |
|---|---|---|---|---|
| **BSC5**（V/50） | ReadMe **没有** License 或 Copyright 行（S1）；Harvard 页（S3）、HEASARC 页（S4）也没写许可条款；CDS 规定先看 ReadMe（S2） | **可行**（判断）：没有声明的限制，内容是位置、星等这类观测事实 | CDS 要求引用作者、出版方（S2）：Hoffleit D. & Warren W.H. Jr. 1991, *The Bright Star Catalogue, 5th Revised Ed. (Preliminary Version)*, NSSDC/ADC；可附「via CDS VizieR V/50」 | **首选**。法律判断：美国法下单纯事实一般不受版权保护；欧盟数据库特别权利的期限问题**未核实** |
| **Hipparcos**（I/239） | `License: CC-BY-NC-3.0 IGO`（S5）；ESA 档案条款：商用前必须向 data.licences@esa.int 申请授权（S8） | **不可直接用**，需要先拿 ESA 的商业授权 | 「ESA」（S8） | 只有在愿意走 ESA 授权流程时才考虑 |
| **Tycho-2**（I/259） | `License: CC-BY-NC-3.0 IGO`（S6） | 同上，不可直接用 | — | 而且 V ≤ 6.5 用不到 Tycho 的深度 |
| **Gaia DR3** | 「Gaia data are distributed under the CC BY-NC 3.0 IGO license」（S7）；商用适用 S8 | 不可直接用，需要 ESA 授权 | 「ESA, Gaia DPAC」（S8） | **任务描述里假设的「CC BY-SA 3.0 IGO / CC BY 4.0」不对**：CC BY-SA 3.0 IGO 是 ESA 发布的 Gaia **图片**的许可（S15、S16），数据是 BY-NC。另外 Gaia 对最亮的星本身就有饱和问题（判断，未核实具体星等阈值） |
| **HYG** | v4.0 起为 **CC BY-SA 4.0**，v3.x 及更早为 **CC BY-SA 2.5**（S9） | 可行但有负担：ShareAlike 要求我们派生出的星表文件也以 CC BY-SA 4.0 提供（判断：SA 只覆盖改编后的数据库，不覆盖应用代码；把星表作为独立文件放进集合不构成改编）；而且 HYG 汇编了 Hipparcos，上游现标 BY-NC（S5），这条链是否干净**未核实** | 需按 CC BY-SA 4.0 署名并给出许可链接 | 备选里排最后 |

### 1.2 体积（基于 BSC5 实测）

方法：从 CDS 下载 `V/50/catalog.gz`（573,921 B，解压后 1,704,879 B），用 `python3 -I` 解析 J2000 RA/Dec（第 76–90 字节）、Vmag（103–107）、B−V（110–114）。

- 有位置和 Vmag 的记录共 9,096 条（9110 条里有 14 条是非恒星条目，没有位置）。
- 不同星等上限下的星数：V ≤ 5.0 有 1,630 颗；≤ 6.0 有 5,080 颗；**≤ 6.5 有 8,404 颗**；≤ 7.0 有 9,050 颗。
- 在 V ≤ 6.5 的子集中：244 颗缺 B−V（编码时需要默认值，例如 0.6）；Vmag 范围 −1.46 到 6.5；B−V 范围 −0.28 到 3.86，其中 25 颗大于 2.0。

| 编码（N = 8,404） | 每颗字节 | 原始 | gzip -9 | brotli -q11 | zstd -19 |
|---|---|---|---|---|---|
| A：f32 RA、f32 Dec、u8 Vmag、u8 B−V，AoS（交错） | 10 | 84,040 | 77,111 | 74,300 | 75,729 |
| **A'：同上，SoA（分列）** | 10 | 84,040 | **68,266** | 66,881 | 65,286 |
| B：int16×3 单位向量 + u8 + u8 | 8 | 67,232 | 66,599 | 67,237 | 67,248 |
| C：u16 RA + i16 Dec + u8 + u8 | 6 | 50,424 | 49,639 | 49,251 | 50,178 |
| JSON 数组 `[[ra,dec,v,bv],…]`（4 位、2 位小数） | — | 242,275 | 90,947 | 80,116 | 83,876 |
| JSON 对象 `[{ra,dec,v,bv},…]` | — | 410,355 | 98,858 | 82,860 | 87,534 |

量化参数：Vmag 线性映射到 [−1.5, 6.5]，步长 0.031 mag；B−V 映射到 [−0.5, 2.0]，步长 0.0098，大于 2.0 的 25 颗被截断（判断：B−V 大于 2 时颜色已经饱和成深红，截断影响不大；如果要保留，把范围改成 [−0.5, 4.0] 即可）。

精度：方案 C 的 RA 步长约 19.8″，Dec 步长约 9.9″。8K 成片 60° 视场下每像素约 28″，C 够用；但长焦（例如 5° 视场，每像素约 2.3″）会出现可见的错位。方案 B 的步长约 1/32767 rad ≈ 6.3″。**建议用 A'（SoA，f32）**：gzip 后约 67 KiB，精度不用操心，而且可以直接作为 `Float32Array` 上传成 instanced attribute。

结论：二进制比 JSON（gzip 后）小约 25%，还省掉解析开销；不过量级上都在百 KiB 以内，JSON 也能接受。

## 二、银河全天图

### 2.1 候选对比

| 候选 | 许可原文 | 商业闭源 | 改作（降采样、去星、调色） | 署名 | 分辨率与体积 | 含前景亮星？ |
|---|---|---|---|---|---|---|
| **NASA SVS 4851 `milkyway_2020`**（S10） | SVS：「All of our content is in the public domain (unless otherwise noted)」（S11）；NASA 指南：texture maps「generally are not subject to copyright in the United States」（S12） | 可行，条件是不得暗示 NASA 背书（S12） | 可以（公有领域，S11 写的是「use … for whatever purposes」） | 页面 credit：「NASA/Goddard Space Flight Center Scientific Visualization Studio. Gaia DR2: ESA/Gaia/DPAC.」（S10） | 赤道坐标（ICRF/J2000）和银道坐标两套，plate carrée，OpenEXR half-float 线性。4K 34.7 MB、8K 130.9 MB、16K 413.9 MB … 64K 3.7 GB（赤道版） | **不含**：「a version of the star map that omits the bright (Hipparcos and Tycho) stars」。保留的是 Tycho 极限（约 11.5 等）以下的 Gaia DR2/UCAC3 星 |
| ESO eso0932a（S14） | CC BY 4.0（S13） | 可行 | 可以，但「indicate what those modifications are」（S13） | 「ESO/S. Brunier」，必须「clear and visible」、文字不得改动（S13） | 6000×3000；Fullsize 27.7 MB；Publication TIFF 4K 12.9 MB | **含**（照片）。页面还提到拍摄期间「bright planets such as Venus and Jupiter」进出星场，成图里是否已经去掉**未核实**。投影按 2:1 比例看是银道坐标的等距柱状投影，但页面没有明说，**未核实** |
| ESA Gaia EDR3/DR2 全天彩色图（S15、S16） | CC BY-SA 3.0 IGO | 按 CC BY-SA 3.0 IGO 可以商用（S17 第 iv 条）；但底层 Gaia 数据是 BY-NC（S7），两者关系**未核实** | 可以，但改作必须同样以 BY-SA 3.0 IGO 发布 | 「ESA/Gaia/DPAC; CC BY-SA 3.0 IGO. Acknowledgement: A. Moitinho」（EDR3） | EDR3 提供 2k、4k、16k PNG（S15 下载链接） | **含**（总通量图，亮星也在里面） |
| Axel Mellinger Panorama 2.0（S18） | 「commercial & educational use: In most cases, this will require payment of a license fee」 | **需付费授权** | 视授权条款而定 | 引用 PASP 121, 1180 (2009) | 个人用途可下载 2000 px 宽的 Mercator 或 Hammer-Aitoff；全分辨率 648 MPixel | 含 |
| Stellarium `textures/milkyway.png`（S19） | 来自 Mellinger 授予 Stellarium 的许可：「Modify and redistribute this image if proper credit to the original image is given.」 | **不建议**：这是作者给 Stellarium 的许可，我们二次取用的效力**未核实**，而且 Mellinger 现行条款对商用收费（S18） | — | — | 1,003,398 B（GitHub API 返回的文件大小） | 未核实 |
| 其他 CC0 或公有领域 | NASA SVS 3895（旧版 Deep Star Maps，S10 写明 4851「is an update to entry 3895」），同属 SVS 公有领域，但内容和是否有无星版**未核实** | — | — | — | — | — |

### 2.2 体积实测（SVS `milkyway_2020_4k.exr`，赤道版）

素材：从 svs.gsfc.nasa.gov 下载 `milkyway_2020_4k.exr`，36,436,668 B，4096×2048，float，4 通道。

G 通道统计（线性）：p50 = 0.0138，p90 = 0.0623，p99 = 0.188，p99.9 = 0.370，max = 1.0。

处理：用 libvips 把 RGB 乘以 2.7（大约让 p99.9 映射到 1.0），转成 8-bit sRGB，再用 Lanczos3 降采样。编码器：`cwebp -m 6`、`avifenc -s 4`。

| 版本 | WebP q80 | WebP q90 | AVIF q60 |
|---|---|---|---|
| 4096×2048 原样 | 2,235,402 | 3,384,482 | 1,677,545 |
| 2048×1024 原样 | 350,372 | 636,352 | 279,758 |
| 1024×512 原样 | 44,940 | 107,250 | 40,685 |
| 2048×1024 灰度 | 350,906 | — | 274,053 |
| **2048×1024，在 4K 上先做 σ=1.5 模糊** | **144,098** | — | **127,127** |
| 2048×1024，在 4K 上先做 σ=3 模糊 | 60,768 | — | 57,692 |
| 4096×2048，σ=3 模糊 | 150,224 | — | 125,758 |
| 1024×512，σ=3 模糊 | 21,256 | — | 21,757 |

（单位：字节）

要点：
- 体积主要花在残留的 Gaia 暗星星点这种高频信息上。轻微模糊就能把体积砍掉 60% 左右，而模糊掉的正是我们不想要的部分，因为点源交给星表画。
- 转灰度几乎不省体积（WebP 和 AVIF 本来就以亮度为主），而且银河有颜色梯度（核球偏黄、尘埃带偏红），**建议保留彩色**。
- **KTX2：本机没有 toktx/basisu，未实测。** 以下按格式规格估算：UASTC LDR 每个 4×4 块 16 B，即 1 B/px。2048×1024 基础层 2.0 MiB，加完整 mip 链约 2.67 MiB（Zstd 超压缩之前；压缩后体积未实测）。ETC1S 体积未实测。显存方面，UASTC 转码成 BC7/ASTC 后是 1 B/px，2K 加 mip 约 2.7 MiB；WebP/AVIF 解码成 RGBA8 是 4 B/px，2K 加 mip 约 10.7 MiB。
- 动态范围：8-bit sRGB 下，最暗处（p1 ≈ 0.00075 × 2.7）只能落到 sRGB 码值个位数左右，暗部渐变可能出现色带。判断：在 shader 里加蓝噪声抖动即可，不必上 HDR 格式。SVS 页面没说明 EXR 的绝对面亮度单位（**未核实**有无定标），亮度需要我们按夜空背景自己标定。

### 2.3 分辨率建议

- 2048 px 覆盖 360°，每像素约 10.5′。8K 成片（7680 px 宽）在 60° 水平视场下每像素约 0.47′，贴图放大约 22 倍；20° 视场下放大约 67 倍。
- 判断：银河是低频面亮度，夜景曝光下又暗，放大后也只是平滑渐变。**推荐 2048×1024，先 σ≈1.5 模糊，WebP q80（约 141 KiB）或 AVIF q60（约 124 KiB）。** 有余量的话可以给 4096×2048 + σ=3（约 147 KiB WebP），细节层级和 2K + σ=1.5 相当，体积几乎一样，好处是长焦下插值更平滑。
- 坐标系：选赤道版（ICRF/J2000），和星表同一坐标系，shader 里只需一次「赤道 → 地平」旋转。如果选银道版，可以用 `Rotation_GAL_EQJ()` 转换（S26）。
- 「星表 V ≤ 6.5」和「贴图里只剩 Tycho 极限以下的星」之间，6.5–11.5 等这一段星没有任何来源。判断：这段星在肉眼极限以下，视觉上影响很小；它们对银河面亮度的贡献在 SVS 的无星版里也确实缺失，银河会稍暗、稍平，可以用整体增益补偿。

### 2.4 程序化生成的可行性（判断，未实测）

- 做法：在银道坐标 (l, b) 里，用银盘按 |b| 指数衰减、核球（l ≈ 0）高斯增强、加上尘埃带的分形噪声吸收（沿 b ≈ 0 的暗带），再叠加 LMC/SMC 两个斑块，直接在 TSL 里按方向求值。银道和赤道之间的转换用 `Rotation_EQJ_GAL()` 的常量矩阵（S26）。
- 优点：零资产体积，没有许可问题，分辨率无限。
- 缺点：辨识度差。天鹅座暗隙、煤袋、船底座、人马座星云这些熟悉的结构很难用解析模型画对；摄影师用户对真实银河形态敏感（判断）。
- 定位：可以作为**兜底**，在 SVS 贴图的法务结论出来之前，或资产加载失败时使用，不作为首选。
- 业界做法：Stellarium 用的是 Mellinger 授权贴图（S19），不是程序化生成。其他产品的做法**未核实**。

## 三、月面贴图

### 3.1 候选对比

| 候选 | 许可原文 | 商业闭源 | 降采样、转灰度 | 署名 | 说明 |
|---|---|---|---|---|---|
| **NASA SVS 4720 CGI Moon Kit**（S20） | SVS 公有领域（S11）+ NASA 指南（S12）；色彩图取自 LROC 的 PDS 产品 Hapke Normalized WAC Mosaic（S20、S22），LROC 条款写明 PDS 数据「are in the public domain」（S21） | 可行，不得暗示 NASA 背书 | 可以 | 「NASA's Scientific Visualization Studio」（S20） | 2025 版色彩图有 EXR 和 16-bit TIFF（4K 59.0 MB、8K、16K），另有 `lroc_color_2k.jpg`（2048×1024，447.2 KB）；2019 版有 1k JPG、2k–16k TIFF。70°N–70°S 以外用 LOLA LDAM 补齐。页面原话：「optimized for aesthetics, not science」。另有 LOLA 位移图 |
| USGS Clementine UVVIS 750 nm Global Mosaic 118m（S23） | 「Use Constraints: Public domain」；USGS 自产数据属于公有领域（S25） | 可行 | 可以 | 判断：可写「USGS Astrogeology / Clementine」（S25 示例：「Credit: U.S. Geological Survey」） | 本身就是灰度反照率图；全图 4 GB，官方 JPEG 样图只有 1024 px |
| USGS LROC WAC Global Morphology Mosaic 100m（S24） | 「Access Constraints: public domain」「Use Constraints: Please cite authors」 | 可行 | 可以 | 引用 LROC Team / ASU | **不适合当反照率图**：这是形貌镶嵌，入射角 55–75°，带地形阴影，渲染时会叠加两层阴影（判断） |
| LROC 网站上的其他图 | 「Images available on the LROC website … are copyrighted」「All commercial use … not downloaded from the LROC PDS archive require prior permission」（S21） | **需授权** | — | — | 直接用素材时，只取 PDS 渠道的产品 |

### 3.2 体积实测

处理：从 SVS 下载 `lroc_color_16bit_srgb_4k.tif`（61,891,324 B，4096×2048，16-bit sRGB），转成 grey16，再转 8-bit，用 Lanczos3 降采样。

| 版本 | WebP q80 | WebP q90 | AVIF q60（单色） | AVIF q75（单色） |
|---|---|---|---|---|
| CGI Moon Kit 灰度 512×256 | 14,032 | — | — | — |
| **CGI Moon Kit 灰度 1024×512** | **49,796** | 94,424 | 65,888 | 96,536 |
| CGI Moon Kit 灰度 2048×1024 | 210,968 | 390,738 | 209,089 | 339,176 |
| CGI Moon Kit 彩色 1024×512 | 50,896 | 98,758 | 70,876 | — |
| CGI Moon Kit 彩色 2048×1024 | 214,700 | 406,858 | 221,179 | — |
| Clementine 灰度 1024×512（基于 USGS 官方 1024 JPEG 样图） | 79,358 | 121,932 | 63,541 | 83,834 |

（单位：字节）

- KTX2：**未实测**。按规格估算，UASTC 是 1 B/px，1024×512 加 mip 约 0.67 MiB（Zstd 压缩前）。单通道灰度可以考虑 BC4 或 EAC R11（0.5 B/px），但 KTX2/Basis 转码到单通道格式的具体路径**未核实**。
- 判断：月面在 8K 成片里约 80 px。1024 宽的贴图在经度方向每像素 0.35°，月盘中心每屏幕像素约对应 1.4° 月面，所以有约 4 倍过采样。**推荐 1024×512 灰度 WebP q80（约 49 KiB）**，2K 版只在长焦特写（月盘超过约 300 px）时按需加载。转灰度对 WebP 体积几乎没有影响，但真实月面的颜色差异很小，灰度乘一个色温就够，还能给 GPU 省通道（判断）。
- Clementine 备选：要做 2K 版必须自己下载 4 GB 的 TIFF 再降采样（`planetarymaps.usgs.gov/mosaic/Lunar_Clementine_UVVIS_750nm_Global_Mosaic_118m_v2.1.tif`）；能否只按范围读取（COG），**未核实**。

### 3.3 朝向：astronomy-engine API（S26，已在 2.1.19 的 `.d.ts` 里确认）

- `RotationAxis(body: Body, date: FlexibleDateTime): AxisInfo`，`body` 可取 `Body.Moon`。`AxisInfo` 包含 `ra` 和 `dec`（极轴在 J2000 赤道坐标中的方向）、`spin`（IAU 本初子午线角 W）、`north`（极轴单位向量）。公式取自 IAU WGCCRE 2015；源码里 Moon 分支包含 E1…E13 的周期项。
- `Libration(date): LibrationInfo`，返回 `elat`、`elon`（地心看到的天平动角，黄道坐标，单位度）、`mlat`、`mlon`、`dist_km`、`diam_deg`。
- 用法（判断）：贴图的经度 0° 对应 IAU 本初子午线。用 `north` 作为月球自转轴，用 `spin` 绕轴旋转，就能摆正月面，天平动也自然包含在内。`Libration` 可以用来校验，或在只需要「近侧朝向 + 小角度偏移」时作为简化路径。IAU 旋转模型和 LROC 地图所用 Mean Earth 坐标系之间的偏差量级**未核实**（在 80 px 月盘上预期不可见）。

## 适用条件与局限

1. **本文不构成法律意见。** BSC5 「没有许可声明」不等于「明确授予了权利」。可用的判断依据是：内容是事实数据，CDS 规则指向 ReadMe，而 ReadMe 里没有版权声明。
2. **SVS 4851 的 Gaia 链条**是这份推荐里最大的不确定点。NASA 页面把 Gaia DR2 只写成 credit，没有标成第三方版权。Gaia DR2 在 2018 年发布时用的是哪个许可，**未核实**；现行 Gaia 数据许可是 BY-NC（S7）。可以按顺序处理：(a) 让法务确认；(b) 邮件问 ESA data.licences@esa.int；(c) 不行就用 ESO eso0932a（CC BY 4.0，要求显式署名）替换，并在 shader 里降低亮星重复的影响，或改用程序化兜底。
3. 访问时 NASA SVS 和 ESO 网站都不可用（503 或维护中），页面文字取自 Wayback 2026 年快照（具体时间戳见来源表）；SVS 的资产文件已从官网直接下载并实测。
4. 体积数据都是本机实测（libwebp 和 libavif 的 Homebrew 版本），不同编码器版本和参数下会有 ±10–20% 的浮动（判断）。KTX2 全部为估算。
5. Brotli 和 zstd 数据只用于对比；静态资源真正上线时的压缩方式取决于部署（阿里云 ECS + 反向代理）的配置，**未核实**。
6. 下载的样本放在会话临时目录（`/private/tmp/claude-501/-Users-samson-workspace-my-project-customer-manage-platform/c337ebc5-4c2e-4156-a918-425a440b744c/scratchpad/nightsky/`），没有进入仓库。
