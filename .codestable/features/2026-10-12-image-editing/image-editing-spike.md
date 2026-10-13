# V6-076 编解码穿刺（实施第 1 步）

对应[后端设计 §11 第 1 步](../../../docs/product/creative-canvas-system/modules/image-editing-backend.md#11-迁移发布与实施切片)。日期 2026-10-12。代码：`backend/internal/creativeimageedit/`（`codec.go`、`source.go`、`selfcheck.go`），镜像目标 `Dockerfile` 的 `creative-worker`，操作说明见 [docs/dev/creative-image-edit.md](../../../docs/dev/creative-image-edit.md)。

## 环境

- 版本锁定：Alpine 3.22，`vips-tools=8.16.1-r0`（libvips 8.16.1），`util-linux-misc`（choom）。
- 测量机：本机 Docker Desktop，arm64，12 核，VM 内存 8 GiB。生产 ECS 是 amd64，资源实测按设计随创意空间发布补做；本表只用于定预算量级。

## 编解码结论

样张测试 8 项，在 `golang:1.25-alpine3.22` 容器和最终 worker 镜像中（`app` 用户）都通过：

| 检查 | 结果 |
|---|---|
| EXIF 方向 1–8（含镜像） | 输出按显示方向摆正，四角颜色与 imaging 的 AutoOrientation 一致，不再带方向标记 |
| 元数据 | 输出没有 EXIF / XMP / IPTC，只保留 ICC |
| 格式策略 | JPEG→JPEG；不透明 PNG（含带全不透明 alpha 通道的 PNG）、灰度 PNG、16 位 PNG→JPEG；透明 PNG→WebP，透明像素仍为 alpha 0；WebP→WebP |
| 裁切 | PNG→PNG、WebP→无损 WebP，逐像素相等；JPEG→JPEG Q95；裁切框按显示方向解释，按存储方向给的框越界被拒 |
| 颜色 | P3 JPEG / WebP 转换为 sRGB 后色值与原 sRGB 相差 ≤ 8；带 profile 的 CMYK 转 sRGB（经色域映射，≤ 24）；无 profile 的 CMYK、损坏的 ICC（PNG / JPEG）返回 `ErrColour` |
| 拒绝 | APNG、动画 WebP、非图片内容、未知类型 → 不支持；像素超限 → 超限；质量越界、裁切越界 → 参数错误；Q100 未变小 → `ErrNotReduced` |
| 取消 | 超时后进程组被杀，临时目录清空 |
| 自检 | worker 镜像中子进程 `oom_score_adj` 为 1000，worker 自身保持 0 |

变异验证：去掉「声明了 profile 但未加载」检查、去掉 autorot、把透明检测改成恒为不透明，对应测试分别失败。

## 与设计的差异（实现已采用，设计已回写）

1. **不设 `VIPS_BLOCK_UNTRUSTED`**：libvips 8.16 把 `vipsload` 标为 untrusted，设置后连自己写的 `.v` 中间文件都读不了。安全边界改为：源文件只经显式 `jpegload` / `pngload` / `webpload` 读取，从不按内容自动识别，SVG / PDF / ImageMagick 等 loader 不可达。
2. **libvips 加载时会静默丢弃无法解析的 ICC**（PNG 和 JPEG 都是），之后会被当成无 profile 的 sRGB 处理。因此在 Go 侧扫描容器结构（PNG iCCP、JPEG APP2 ICC_PROFILE、WebP ICCP）：文件声明了 profile 而 vips 没有加载到，就返回 `ErrColour`。同一次扫描也拒绝 APNG（libvips 只读第一帧且不报告动画）和动画 WebP。
3. **16 位输入输出为 8 位**：无 profile 的 16 位 PNG 经 colourspace 转 8 位 sRGB，有 profile 的经 `icc_transform --depth 8`；裁切 16 位 PNG 也输出 8 位 PNG。
4. **子进程 `ru_maxrss` 不能当 vips 内存观测**：Go 用 vfork 启动子进程，Linux 会把父进程的 RSS 高水位带过 exec，worker 自身的 RSS 会混进来。生产观测不用它；下表是由很小的父进程分别启动子进程测得的。

## 资源实测

每格是单次渲染。「vips」列是全部 vips 子进程中的最大值，「中间文件峰值」是同一时刻存在的 `.v` 文件之和。「Go 校验」是现有上传校验器（完整解码 + 主色 + display）对输出的一次运行。两者先后执行，不会同时占用。

| 图 | 动作 | 源 | 输出 | vips 耗时 | vips 峰值 RSS | 中间文件峰值 | Go 校验耗时 | Go 校验峰值 RSS |
|---|---|---|---|---|---|---|---|---|
| 12MP | 压缩 JPEG Q75 | 2.5 MiB | 0.6 MiB | 170ms | 77.4 MiB | 35.0 MiB | 310ms | 65.2 MiB |
| 12MP | 裁切 JPEG 中央 80% | 2.5 MiB | 2.3 MiB | 210ms | 76.8 MiB | 56.3 MiB | 510ms | 64.0 MiB |
| 12MP | 透明 PNG 压缩 → WebP Q75 | 12.6 MiB | 0.1 MiB | 750ms | 193.3 MiB | 45.9 MiB | 180ms | 118.5 MiB |
| 24MP | 压缩 JPEG Q75 | 5.1 MiB | 1.3 MiB | 240ms | 123.5 MiB | 69.9 MiB | 230ms | 85.8 MiB |
| 24MP | 裁切 JPEG 中央 80% | 5.1 MiB | 3.8 MiB | 330ms | 121.9 MiB | 112.6 MiB | 320ms | 97.6 MiB |
| 24MP | 透明 PNG 压缩 → WebP Q75 | 24.8 MiB | 0.2 MiB | 1.79s | 360.8 MiB | 91.7 MiB | 310ms | 220.9 MiB |
| 60MP | 压缩 JPEG Q75 | 12.6 MiB | 3.2 MiB | 500ms | 207.7 MiB | 174.8 MiB | 480ms | 159.6 MiB |
| 60MP | 裁切 JPEG 中央 80% | 12.6 MiB | 11.2 MiB | 710ms | 251.8 MiB | 281.5 MiB | 1.12s | 182.9 MiB |
| 60MP | 透明 PNG 压缩 → WebP Q75 | 60.9 MiB | 0.4 MiB | 5.65s | 859.2 MiB | 229.2 MiB | 710ms | 537.6 MiB |

样张是带噪声的渐变（近似照片）；60MP 透明 PNG 超过 25 MiB 上传上限，只用来定峰值。

预算结论（单个 image_edit 槽）：

- 内存：最坏是 60MP 透明图转 WebP，vips 约 0.86 GiB，结束后 Go 校验约 0.54 GiB。worker 容器要在自身基线（含并行的 Agent / 生成任务）之上留出约 1 GiB；留不出来时按设计降低 image_edit 的像素上限，并在 capabilities 里如实显示。
- 磁盘：中间文件峰值约 0.3 GiB，在设计给出的每槽 1 GiB 起始预算以内。
- 时延：本机最长 5.7s，远低于 60s 的渲染上限；生产机更慢，留待部署环境实测。

## 镜像问题与处理（owner 2026-10-12 同意一并修）

- **server 镜像原先构建不了**（这次之前就存在）：`frontend/package-lock.json` 缺少 linux-musl 平台需要的可选依赖 `@emnapi/runtime`，`node:24-alpine` 中的 npm 11.16 执行 `npm ci` 失败。已用该容器的 npm 执行 `npm install --package-lock-only` 补齐：只新增 `@emnapi/runtime@1.11.3`，另调整几处 `peer` 标记，没有依赖版本变化。server 镜像构建通过。worker 镜像本来就用单独的 Go 构建阶段，不依赖前端产物。
- **两个镜像原先都没有 ffprobe**：音视频上传在 worker 里校验，API 也按 ffprobe 是否存在决定开放哪些格式。现在 server 与 worker 都装了 `ffmpeg=6.1.2-r2`，在 worker 镜像里生成 h264/aac mp4 与 pcm wav 后 ffprobe 均可识别。镜像体积：server 391 MB，worker 278 MB（本机 arm64）。

## 遗留

- 生产 amd64 / cgroup 下的内存与 OOM 行为，随创意空间发布实测。
