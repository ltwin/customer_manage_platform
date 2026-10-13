# V6-076 图片编辑一期：验证记录

对应[需求条目](../../requirements/creative-workspace-v6/items/V6-076.md)、[产品设计](../../../docs/product/creative-canvas-system/image-editing.md) §10、[后端设计](../../../docs/product/creative-canvas-system/modules/image-editing-backend.md)；实现说明见[开发说明](../../../docs/dev/creative-image-edit.md)。

## 分步提交

| 步骤 | 内容 | 提交 |
|---|---|---|
| 1 | 编解码穿刺：worker 镜像、`creativeimageedit` 编解码器、样张与资源实测 | [穿刺记录](image-editing-spike.md) |
| 2 | 候选队列与失败恢复 | `cb942de` |
| 3 | 同步采用、HTTP / OpenAPI | `b2d3e44` |
| 4 | UI 与 Agent 接入；受理错误与采用统一；`generate_node` 返回 `apply_state` | 与本记录同一提交 |

## 第 4 步门禁（2026-10-13）

- 后端：`go build ./...`；`golangci-lint run` 覆盖 creativecanvas / creativeimageedit / creativeagent / creativegeneration / cmd，0 issues；`go test -count=1 ./internal/creativecanvas/... ./internal/creativeimageedit/... ./internal/creativeagent/... ./internal/creativegeneration/...` 全部通过（dbtest 用真实 PostgreSQL 容器）。
- 前端：`make check-frontend`（oxlint、`tsc -b` + 构建、486 项单测）通过。
- 浏览器：`node scripts/creative-image-edit.e2e.mjs` 7 项通过（纯 mock，Vite 5173）；回归 `creative-node-toolbar.e2e.mjs`、`creative-canvas-interaction.e2e.mjs` 通过。
- 编解码样张：`make check-image-codec`（vips 8.16.1 容器）通过。

## AC 证据

「本机已验证」指上面门禁里的自动化证据；「待验收」是还缺真实环境或真实模型的部分。整体 AC 仍按条目要求在 owner 走查后勾选。

| AC | 证据 | 状态 |
|---|---|---|
| AC-01 入口一致 | e2e「入口」：工具条与右键菜单同为「裁切 / 压缩」；节点正在生成、GIF 源禁用并说明原因；单测 `imageEditGate` 覆盖只读、上传中、不可读、空节点、多选、非图片、功能关闭 | 本机已验证 |
| AC-02 格式说明与真实大小 | e2e「压缩」：应用前说明 PNG 的输出规则，应用后显示原始 → 输出、减少比例和输出格式，数值取自候选的 `source_size` / `output`；后端 `output` 来自校验后的真实文件（第 2 步 dbtest） | 本机已验证；真实后端走查待验收 |
| AC-03 尺寸、方向、透明、固定源 | 编解码样张测试（方向 1–8、透明 PNG/WebP 不加底色、16 位、ICC）；面板与 Agent 都在打开 / 首次调用时固定源修订 | 样张测试通过；真实走查待验收 |
| AC-04 调质量不处理、只编码一次 | e2e：调档位与数值后 0 次受理请求，应用后 1 次受理 1 次采用，处理中按钮禁用；dbtest 同一操作重放返回原候选 | 本机已验证 |
| AC-05 未减小 | e2e「未减小」：提示且 0 次采用、面板保留可重试；dbtest `TestOutcomesThatAreNotAResult`；Agent 返回 `not_reduced` | 本机已验证 |
| AC-06 裁切交互、缩放后准确 | e2e「裁切」：1:1 / 16:9 / 自由、固定比例拖角保持正方形、拖动选区不移动节点、放大画布后选区与源像素比例一致、重置、取消与 Esc 不发请求；单测覆盖全部比例、边界限制、取整 | 本机已验证 |
| AC-07 裁切产生真实文件 | e2e：受理参数为整数像素框且等于工具条显示的尺寸，采用后节点按新比例变高；dbtest `TestApplyCropUndoRedoAndRights` | 本机已验证；真实下载走查待验收 |
| AC-08 只影响当前节点、撤销重做 | 第 3 步 dbtest（多引用、尺寸、撤销 / 重做）；前端撤销走服务端变更历史 | 本机已验证 |
| AC-09 失败与响应丢失 | e2e「采用响应丢失」：只产生一个版本，重发请求体与原请求相同；单测覆盖未提交即断线时原样重发；Agent 采用未知时返回 `unconfirmed` | 本机已验证 |
| AC-10 迟到结果不覆盖 | e2e「编码中取消」：丢弃候选、0 次采用；裁切中源图被换会自动退出；dbtest 来源 / 执行槽 / 权限守卫，受理与采用错误一致（`TestCreateSharesApplySourceErrors`） | 本机已验证 |
| AC-11 Agent 真实调用 | dbtest `TestAgentCompress*`：真实受理、等待、同步采用、只采用一次、超时丢弃、拒绝；`TestAssistantCompressesAnImage`：工具只在开启时提供，结果与拒绝如实转述 | **待验收：缺真实模型的工具调用记录** |
| AC-12 手工与 Agent 同一规则 | Agent 复用 `Create` / `Apply`；`agentRefusal` 与 HTTP 错误映射一一对应 | 本机已验证 |
| AC-13 候选回收与额度 | 第 2 步 dbtest `TestUnappliedResultsAreCollected`、`TestRenderBecomesReadyWithoutBookingQuota`；Agent 与界面在不采用时都主动丢弃 | 本机已验证 |
| AC-14 桌面与窄屏 | e2e「窄屏」：390×844 下压缩为全屏面板、按钮在视口内，裁切工具条在视口内；截图 `/tmp/creative-image-edit-mobile.png` | 本机已验证；真机触屏走查待验收 |

## 待 owner 验收

1. 在接了真实 worker 的环境里用真实照片各走一遍压缩、裁切、撤销、下载（AC-02 / 03 / 07 / 14）。
2. 用真实模型让助手压缩一张图，保存工具调用记录（AC-11）。
3. 质量档位 90 / 80 / 60 仍是建议值，需样张校准后冻结（产品稿 §4.2）。
