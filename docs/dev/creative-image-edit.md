# 图片编辑编解码（creativeimageedit）

设计见[后端设计](../product/creative-canvas-system/modules/image-editing-backend.md)（§3 选型、§8 编码策略、§10 资源）；穿刺结论见 [V6-076 编解码穿刺](../../.codestable/features/2026-10-12-image-editing/image-editing-spike.md)。

## 组成

- `creativeimageedit.Codec` 通过 `vips` / `vipsheader` 子进程执行固定策略 `image-edit-v1`，不经过 shell。源文件按已校验的类型显式使用 `jpegload` / `pngload` / `webpload`，中间文件为无损 `.v`，只在末尾做一次有损编码，输出只保留 sRGB ICC。
- 启动子进程前，按源类型调用 Go 的 JPEG / PNG / WebP 头部解码器检查单边与总像素上限，超限时不完整解码、不创建渲染目录；libvips 加载后再次复核尺寸及颜色策略。尺寸预检测试不依赖本机安装 vips。
- 每次渲染在 `TempRoot` 下的私有 `render-*` 目录进行；源文件暂存为同一根目录下的 `source-*` 文件。正常退出或失败时删除各自的临时产物，`SweepTemp` 在 worker 启动时清理被杀进程留下的渲染目录和源文件。Service 与 Codec 必须配置同一个临时根目录。子进程有独立进程组，取消时整组 SIGKILL。
- Linux 上经 `choom -n 1000 --` 启动 vips，让 OOM 时内核先选中 vips 而不是 worker；这只降低风险，不是隔离。
- `SelfCheck` 在功能开启时由 worker 启动调用：核对 vips 版本、子进程 OOM 评分，并实际跑通三种格式和一次 ICC 转换。

## 候选与队列

- `creativeimageedit.Service`：`Create` 在点击应用时受理一个候选（固定源修订、节点忙、账号并发与频率上限都在同一事务里检查），`Get` 读取，`Discard` 丢弃，`Work` 是 River `image_edit` 队列（并发 1）的任务，`SweepExpired` / `RunSweep` 每 5 分钟把过期候选记为 expired。
- 结果写入顺序：先由 creativemedia 的上传校验器校验编码结果，再在事务里认领候选并写入 verifying 的 blob 行，事务外上传对象，最后把 blob 置为 ready（不计额度）、候选置为 ready（30 分钟内可采用）。认领后写对象失败时释放认领，半成品 blob 转为 deleting；若 ready 落库失败、释放认领失败或进程异常退出，重投递会在账号写屏障下将旧认领的半成品交给 GC，再以新 blob 身份重试。仍存活的旧写入无法通过 blob 状态与候选身份复核。已 ready 或终结的候选重投递不写任何 blob，包含上次提交成功但响应未知的情况。 worker 上下文提前取消时保留候选并返回可重试错误；只有锁内数据库时间达到候选自身期限才记为 expired，旧尝试取消不回收新尝试的认领。
- 开关：`CREATIVE_IMAGE_EDIT=on` 只在 worker 读取；开启要求 `CREATIVE_MEDIA_GC_MODE=delete`，并在启动时 `SweepTemp` + `SelfCheck`，任一失败即拒绝启动。HTTP 接口与采用在第 3 步。
- 数据库测试在 `creativeimageedit/dbtest`，用假渲染器；编解码样张测试留在包内，因为它们在没有 Docker 的 vips 容器里跑。

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
