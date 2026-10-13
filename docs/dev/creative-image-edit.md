# 图片编辑编解码（creativeimageedit）

设计见[后端设计](../product/creative-canvas-system/modules/image-editing-backend.md)（§3 选型、§8 编码策略、§10 资源）；穿刺结论见 [V6-076 编解码穿刺](../../.codestable/features/2026-10-12-image-editing/image-editing-spike.md)。

## 组成

- `creativeimageedit.Codec` 通过 `vips` / `vipsheader` 子进程执行固定策略 `image-edit-v1`，不经过 shell。源文件按已校验的类型显式使用 `jpegload` / `pngload` / `webpload`，中间文件为无损 `.v`，只在末尾做一次有损编码，输出只保留 sRGB ICC。
- 启动子进程前，按源类型调用 Go 的 JPEG / PNG / WebP 头部解码器检查单边与总像素上限，超限时不完整解码、不创建渲染目录；libvips 加载后再次复核尺寸及颜色策略。尺寸预检测试不依赖本机安装 vips。
- 每次渲染在 `TempRoot` 下的私有 `render-*` 目录进行；失败或取消时删除整个目录，`SweepTemp` 在 worker 启动时清理被杀进程留下的目录。子进程有独立进程组，取消时整组 SIGKILL。
- Linux 上经 `choom -n 1000 --` 启动 vips，让 OOM 时内核先选中 vips 而不是 worker；这只降低风险，不是隔离。
- `SelfCheck` 在功能开启时由 worker 启动调用：核对 vips 版本、子进程 OOM 评分，并实际跑通三种格式和一次 ICC 转换。

## 镜像

`docker build --target creative-worker -t <tag> .` 构建 worker 镜像：Alpine 3.22 + `vips-tools=8.16.1-r0` + `util-linux-misc`（choom）+ `ffmpeg=6.1.2-r2`（ffprobe，音视频上传校验），以 `app` 用户运行。默认目标仍是 server 镜像。升级 libvips 时同时改 Dockerfile 与 Makefile 的 `VIPS_APK`，并重跑下面两项。

## 验证

- `make check-image-codec`：在同版本 vips 的容器里跑样张测试（方向 1–8、格式策略、透明、16 位、灰度、P3 / CMYK / 损坏 ICC、APNG / 动画 WebP、取消清理、SelfCheck）。本机没有 vips 时这些测试直接跳过，`make check` 不会覆盖它们。
- 资源穿刺（测量，不是门禁）：

  ```sh
  docker run --rm -v "$PWD/backend":/src -w /src -e IMAGE_EDIT_SPIKE=1 golang:1.25-alpine3.22 \
    sh -c 'apk add --no-cache vips-tools=8.16.1-r0 util-linux-misc >/dev/null && \
      go test ./internal/creativeimageedit/ -run TestResourceSpike -count=1 -v -timeout 30m'
  ```

  每一步在单独的子进程里执行，由一个很小的父进程启动。Go 用 vfork 启动子进程，Linux 会把父进程的 RSS 高水位带过 exec；父进程若持有大图，每个子进程的 `ru_maxrss` 都会被污染。同理，worker 里拿到的子进程 `ru_maxrss` 不能当作 vips 的内存观测。
