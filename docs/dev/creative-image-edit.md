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
- 开关：`CREATIVE_IMAGE_EDIT=on` 由 API 与 worker 共同读取，两侧必须配置一致；开启要求 `CREATIVE_MEDIA_GC_MODE=delete`。worker 启动时额外执行 `SweepTemp` + `SelfCheck`，任一失败即拒绝启动；API 不运行编解码器。关闭时 capabilities 返回 `available=false`，候选及采用接口返回 503。
- 数据库测试在 `creativeimageedit/dbtest`，用假渲染器；编解码样张测试留在包内，因为它们在没有 Docker 的 vips 容器里跑。

## HTTP 与同步采用

- `GET /api/v1/creative/image-edit-capabilities` 返回领域策略、当前媒体服务的限制与开关，不用 worker 心跳决定是否显示能力；查询候选和能力均使用只读快照，不取得账号写屏障。
- `POST /api/v1/creative/image-edit-renders` 接受 `operation_id`（与 `Idempotency-Key` 一致）、`canvas_id`、`node_id`、`source_revision_id`、`action` 和 `parameters`，返回 202。压缩参数为 `{quality}`；裁切为完整整数框 `{left, top, width, height}`，每个值不超过 12000，整体选区还必须满足运行配置与实际图像边界；计算边界用减法避免整数溢出。摄影师入口的 `caller_kind` 固定由服务器赋值。
- `GET /api/v1/creative/image-edit-renders/{id}` 查询状态。编码失败以 `state/error_code` 表达。已知的源字节数放在顶层 `source_size`；只有 ready/applied 才提供真实校验后的 `output`，失败/未减小/丢弃/过期均不伪造输出事实。`POST .../{id}/discard` 幂等丢弃未完成候选；已采用的候选不会被此操作撤销。
- `POST /api/v1/creative/canvases/{id}/nodes/{node_id}/image-edits/apply` 采用已有结果并同步返回 200。沿用画布命令信封 `operation_id/client_created_at/payload` 与 `Idempotency-Key`；payload 只有 `render_id/expected_source_revision_id/expected_data_revision`，修订值为十进制字符串。重试必须保留完整信封（含创建时间），画布与节点只取路径。
- `Apply` 使用 `creativeops.Executor`：先恢复已有回执，再检查当前权限、可写画布、来源和数据修订、执行槽、候选期限。`BindEditBlobsInTx` 只绑定已经校验的原始/展示 blob 并计入额度，不做对象读写、复制或重新编码；复用来源声明并通过 `RetainDerivativeRights` 保留传递授权。`PrepareImageEditInTx` 一次性锁定并检查目标，返回与事务绑定的目标；其 `ApplyInTx` 经 `makeVersion(edit)`、`fitMediaSize` 和 `persistGraph` 保存可撤销变更；最后与候选 applied、操作回执一起提交。
- 结果返回 `version_id/content_revision_id/change_id/result_revision` 及标准画布变更字段（节点最新数据修订在 `object_results`）。来源冲突、节点忙或未 ready 返回 409；只有数据修订变化（如标题/意图改变）时返回 `creative_revision_conflict`，刷新修订后可重试原候选；图片被清空或替换才返回 `creative_image_edit_source_changed`。由候选占用节点时，忙错误的 `details.render_id` 指向现有候选。过期/丢弃返回 410；额度不足返回既有 429，候选保持 ready。取消或失败的事务不留下版本、额度或成功回执。原请求即使在撤销后重放也只返回原回执，不重新采用。

机器契约在 `api/openapi.yaml`；手工注册的认证路由与 Go/TypeScript 生成物同时维护。数据库测试 `creativeimageedit/dbtest/apply_test.go`、`http_test.go` 覆盖额度回滚、字节身份、尺寸与撤销/重做、来源/权限/归档/修订/执行槽守卫、采用与回收/丢弃/重复请求竞争，以及 HTTP 鉴权、账号隔离、参数边界和功能关闭。

受理与采用用同一组来源守卫：`creativecanvas.CheckImageEditSourceInTx`（受理）和 `PrepareImageEditInTx`（采用）共用节点检查，节点被清空或换图都返回 `ErrSourceChanged`（409 `creative_image_edit_source_changed`），节点正在执行返回 `ErrNodeBusy`（409，无 `render_id`）；只有候选占着节点时才是带 `render_id` 的 `BusyError`。

## UI

- 入口：单选、有图片内容的图片节点，在工具条「图片编辑 ▾」和右键 / 更多菜单的「图片编辑」分组里提供「裁切」「压缩」。能力接口 `available=false` 或读取失败时不展示；只读、上传中、原文件不可读、格式不在 `input_mime_types`、节点正在生成时禁用并在提示里说明原因（`imageEditModel.imageEditGate`）。
- 流程：`imageEditModel.runImageEdit` 是点击「应用」后的唯一流程——创建候选 → 每秒查询 → ready 后同步采用。等待中取消（关面板、取消裁切、离开页面）只丢弃候选；采用一旦发出就不能取消，响应丢失时只读候选状态并原样重发同一请求体（含 `client_created_at`），不重新编码。标题等元数据变化导致的 `creative_revision_conflict` 会带上新数据修订重试同一候选；其他明确拒绝会丢弃候选，不占着节点。节点上留有别处未完成的候选时，界面提供「放弃那项编辑并重试」。
- 压缩面板（`ImageEditor.CompressPanel`）：打开时固定源修订；显示原始大小、格式、像素尺寸和输出格式规则；调质量不发请求，输入框里 Enter 不触发应用；应用后显示真实的原始 → 输出大小、减少比例和输出格式。窄屏（≤ 760px）为全屏面板。
- 裁切（`ImageEditor.CropStage` / `CropControls`，会话在 `imageEdit.useImageCrop`）：在节点内覆盖图片，选区始终以方向校正后的源像素表示，屏幕位移按图片实际显示宽度换算，画布缩放和高 DPI 不影响范围；应用时四边各自取整。裁切期间节点不能拖动，连线端口、缩放把手、右键菜单和画布快捷键都停用，只保留缩放视图；Esc 或「取消」退出且不发请求；源图在别处被换掉时自动退出。工具条跟着被裁的节点，取代选区工具条。
- 采用成功后画布经事件流刷新，撤销 / 重做走服务端变更历史，前端不另记。

## Agent

- 工具 `compress_image`（`creativeagent/image_tools.go`）只在 `CREATIVE_IMAGE_EDIT=on` 时注册，参数为 `node_id`、可选 `quality`（缺省 80）和可选 `content_revision_id`（固定要压的版本）。
- 实际流程在 `creativeimageedit.CompressForAgent`，与面板走同一个 `Create` / `Apply`：操作标识由 `run_id + tool step` 派生，重放先按操作标识找到原候选，不重新读取节点当来源；在一次调用内每 2 秒查询，等待上限 90 秒且不超过运行期限。结果 `state` 只有 `applied` 表示节点已更新；`not_reduced` / `failed` / `not_applied` 都没有改动并给出原因；等待到期还没发出采用就丢弃候选；采用已发出但结果未知时只读候选状态，返回 `unconfirmed`，不会重发也不会说成未应用。拒绝原因（来源已变、节点忙、额度不足等）与 HTTP 错误一一对应。
- `generate_node` 同时返回 `apply_state`：生成成功后继续等到结果落到节点上；`conflicted` / `discarded` / `expired` 明确是「没有采用」，只有 `applied` 才算节点已更新（`AgentGenerationResult.Adopted`）。

## 部署开关预检

`production-preflight.sh` 在 API env 开启图片编辑时要求 `--creative-worker-env /实际部署/worker.env`，校验两份 env 的 `CREATIVE_IMAGE_EDIT` 一致且 `CREATIVE_MEDIA_GC_MODE=delete`。可以单独执行 `python3 scripts/lib/creative_image_edit_config.py --api-env /实际部署/api.env --worker-env /实际部署/worker.env`。只读取配置数据，不执行 dotenv，也不输出凭证；该检查保证所提供的部署配置一致，不承诺 worker 心跳或可用性。现有 Compose 未定义 worker，不能用 API 的 env 文件冒充独立 worker 的实际配置。

图片编辑关闭时 API 不解析 GC 模式，避免无关配置影响 API 启动；worker 的 GC 本身仍独立校验。配置回归与 56 项部署预检夹具进入 `make check-ops`。

## 镜像

`docker build --target creative-worker -t <tag> .` 构建 worker 镜像：Alpine 3.22 + `vips-tools=8.16.1-r0` + `util-linux-misc`（choom）+ `ffmpeg=6.1.2-r2`（ffprobe，音视频上传校验），以 `app` 用户运行。默认目标仍是 server 镜像。升级 libvips 时同时改 Dockerfile 与 Makefile 的 `VIPS_APK`，并重跑下面两项。

## 验证

- 前端：`scripts/creative-image-edit.test.ts`（几何、入口状态、应用流程各分支）进入 `make check-frontend`；浏览器回归 `scripts/creative-image-edit.e2e.mjs` 是纯 mock，只需 Vite（`npx vite --port 5173`），`CREATIVE_QA_CHECK=<名称片段>` 可只跑部分检查。
- 后端：`creativeimageedit/dbtest/agent_test.go`（Agent 压缩只采用一次、超时丢弃、拒绝），`creativeagent/image_tools_test.go`（工具只在开启时提供、结果与拒绝如实转述）。
- `make check-image-codec`：在同版本 vips 的容器里跑样张测试（方向 1–8、格式策略、透明、16 位、灰度、P3 / CMYK / 损坏 ICC、APNG / 动画 WebP、取消清理、SelfCheck）。本机没有 vips 时这些测试直接跳过，`make check` 不会覆盖它们。
- 资源穿刺（测量，不是门禁）：

  ```sh
  docker run --rm -v "$PWD/backend":/src -w /src -e IMAGE_EDIT_SPIKE=1 golang:1.25-alpine3.22 \
    sh -c 'apk add --no-cache vips-tools=8.16.1-r0 util-linux-misc >/dev/null && \
      go test ./internal/creativeimageedit/ -run TestResourceSpike -count=1 -v -timeout 30m'
  ```

  每一步在单独的子进程里执行，由一个很小的父进程启动。Go 用 vfork 启动子进程，Linux 会把父进程的 RSS 高水位带过 exec；父进程若持有大图，每个子进程的 `ru_maxrss` 都会被污染。同理，worker 里拿到的子进程 `ru_maxrss` 不能当作 vips 的内存观测。
