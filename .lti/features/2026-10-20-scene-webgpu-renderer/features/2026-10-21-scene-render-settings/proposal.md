---
risk:
  factors:
    - perf-sensitive
    - broad-impact
behavior: changed
reads:
  - creative/scene
  - creative/documents
decisionsPending: false
---
# 本次交付

## Why

Epic `scene-webgpu-renderer` 基线 v3 的 R2，也是最后一个子项。roadmap 交给 R2 的问题是：**屏幕空间 GI 能否明显改善画面和重绘，代价能否接受；高分辨率导出在浏览器里是否可行**。

R1 已经把全部渲染迁到 `WebGPURenderer` + TSL，渲染器就是「工作台」（2026-10-21 合入 develop）。R2 在这条管线上加能力：属性区的「渲染」选项卡、实时 GI 渲染器、输出分辨率。三者共用 `render` 的新字段和同一个选项卡，由本子项一并交付 `data: 场景文档`。

R0（`scene-webgpu-spike`）已经验证了实时 GI 的组合、帧时间、WebGL2 后端和各档分辨率的耗时与显存，结论在 Epic design 里；R0 留给 R2 的是 SSGI 的降噪接法（Epic 未决事项 10）、直接光与间接光的合成、收敛帧数和导出累积帧数的标定。

## What Changes

1. **场景格式与后端**（交付 `data: 场景文档`）：`render` 新增 `engine`（`workbench` | `realtime_gi`）和 `resolution`（`1k` | `2k` | `4k` | `8k`），读取时补缺省值，完整读取的响应里带齐；`set_render` 按键合并、可撤销；`path_tracing` 写入返回 400。渲染指纹只在 `clay`、`lit` 且不是 `workbench` 时计入 `engine`，`resolution` 不计入，从未改过渲染器的场景指纹不变。OpenAPI 加字段并重新生成。媒体的总像素上限放宽到 8192²。
2. **渲染选项卡**：属性区改成 Blender 式的三个选项卡「渲染」「环境」「物件」。「渲染」里是渲染器（工作台、实时 GI、禁用的「路径追踪 · 规划中」）、输出分辨率、曝光补偿（从环境面板移过来）和当前渲染后端（「WebGPU」或「兼容模式」），附屏幕空间方法的局限说明。
3. **实时 GI 渲染器**：在工作台管线上加 SSGI（项目内 fork，每帧换噪点图样）、SSR、接触阴影、时域重投影与降噪、TRAA；直接光与间接光分开合成，AO 只作用于间接光。渲染器只作用于最终渲染和材质光照（`lit`）：白模、线框、材质预览和线稿总是工作台（owner 2026-10-21 验收前修订）。正交视图回落到工作台并在视口角标提示。灯光图标、摄像机视锥、空物体等辅助对象照常显示，但不参与 AO 和实时 GI。
4. **收敛与累积**：视口在实时 GI 下停止操作后再渲染 N 帧收敛，然后停止重绘；导出和 `render_view` 在线性空间逐帧平均 K 帧。N、K 在本子项标定。工作台不变（N = 1，K = 1）。
5. **输出分辨率**：导出按所选档位定长边，短边按画幅；4K、8K 的 `clay`、`lit` 用 JPEG（质量 0.95），其余仍是 PNG；单张与整批的时限按渲染器、像素数和 K 计算；出图过程中逐帧让出主线程，续租照常执行；设备画不了的档位（纹理上限不够，或 4K、8K 出图时丢失设备）明确失败并说明原因，不悄悄降分辨率。
6. **助手**：`edit_scene` 的 `set_render`、`render_view`、`export_shots` 的工具说明和系统提示加上渲染器与输出分辨率；按「排空后升级」记录。
7. **验证**：AC-4 盒子间的效果用例、渲染选项卡与过时判断用例、各档分辨率导出用例、隐藏标签页连拍确定性（实时 GI）、WebGL2 后端、GI 管线不残留纹理；Metal 上的帧率、收敛和导出耗时；工作台与实时 GI 的重绘对照样例。

## Acceptance Criteria

整体验收留在 Epic；R2 承担 Epic AC-4、AC-5、AC-6、AC-8、AC-9 的全部，AC-3 和 AC-7 的实时 GI 部分，以及未决事项 10。参考设备沿用 R0（M4 Pro，24 GB）。

- **AC-R2-1 渲染选项卡**（Epic AC-5 前半）：**WHEN** 摄影师打开属性区的「渲染」选项卡，**THEN** 看到渲染器（工作台、实时 GI，以及禁用的「路径追踪 · 规划中」）、输出分辨率（1K / 2K / 4K / 8K）、曝光补偿和当前渲染后端；「环境」选项卡里不再有曝光补偿。选中物件时属性区自动切到「物件」，取消选中后回到之前的选项卡。
- **AC-R2-2 设置、撤销、同步与过时**（Epic AC-5 后半）：
  - **WHEN** 摄影师把渲染器换成实时 GI，**THEN** 编辑器的最终渲染、`render_view` 的 `lit`、`lit` 导出都是实时 GI 的画面；Cmd+Z 撤销回工作台；另一个标签页同步显示；已有的 `lit` 导出图提示「场景已更新」，`clay`、`line` 不提示，换回工作台后提示消失。
  - **WHEN** 只改了输出分辨率，**THEN** 已有导出图都不提示过时。
  - **WHEN** 读取一个在 R2 之前创建、从未改过渲染设置的场景，**THEN** 响应里 `render` 带齐 `engine: workbench`、`resolution: 2k`、`exposure_ev`；它的导出图指纹与 R2 之前相同（单测固定 v1 指纹），不会因为本子项变成过时；这个场景之后的任何写入都成功。
  - **WHEN** 请求写入 `engine: path_tracing` 或未知的分辨率，**THEN** 返回 400 并指出字段。
- **AC-R2-3 实时 GI 效果**（Epic AC-4）：**WHEN** 盒子间参考场景（`cornell.json`：红墙、绿墙、白地面、金属球、光滑地板）的渲染器为实时 GI，导出 `lit`，**THEN**
  - 白地面靠近红墙的区域明显偏红，金属球和地板上能看到场景里其他物体的反射，物体与地面的接触处比工作台更暗（按区域测量的数值写进用例，图存进 evidence，owner 看图）；
  - 切回工作台后，导出图与 R1 的工作台参考图在冻结阈值内（平均差 ≤ 0.05，差 > 8 的像素为 0）。
- **AC-R2-4 显示模式与正交视图**：**WHEN** 渲染器为实时 GI，**THEN** 白模、线框、材质预览和 `clay`、`line` 导出与工作台逐像素相同；正交视图按工作台显示，视口角标提示「正交视图按工作台显示」；视口里的灯光图标、摄像机视锥和空物体照常显示，但不遮挡、不压暗周围（有无它们，最终渲染只差它们自己的线条）。
- **AC-R2-5 收敛、累积与确定性**（Epic AC-7 部分、AC-2）：
  - **WHEN** 实时 GI 的视口停止操作，**THEN** 再渲染 N 帧后停止重绘（帧计数不再增长），画面稳定；工作台停止操作后只画 1 帧。
  - **WHEN** 导出实时 GI 的图，**THEN** 盒子间后墙上部的残余噪点（画面减去其高斯模糊后的平均绝对值）低于标定时确定的阈值。
  - **WHEN** 在真实 Chrome 的隐藏标签页里，对同一实时 GI 场景连续两次导出多台摄像机的 `clay`、`line`、`lit`，**THEN** 两次之间、以及与前台导出的同一张图之间，每个通道相差不超过 1，相差超过 8 的像素不超过 0.01%（owner 2026-10-21 定：three 重建材质后着色代码会变，实时 GI 还有偶发的最低位差，不要求逐像素相同；缺帧或受计时器节流影响会是大面积的噪点差，容差仍能发现）。工作台导出仍逐像素相同（AC-R1-3）。
- **AC-R2-6 输出分辨率**（Epic AC-9）：
  - **WHEN** 输出分辨率分别为 1K、2K、4K、8K 并导出（含 1:1 和竖幅摄像机），**THEN** 导出图长边为 1024、2048、4096、8192 px，短边符合画幅，作为画布图片节点正常显示；4K、8K 的 `clay`、`lit` 是 JPEG，`line` 和 1K、2K 是 PNG。
  - **WHEN** 设备的纹理上限小于所选档位，或 4K、8K 出图时设备丢失，**THEN** 导出明确失败并说明原因（手动导出提示，助手导出以 `host_error` 结束、不回队），不降分辨率。
  - 8K 实时 GI 在参考设备上能导出；如果实在做不到，按 Epic AC-9 的约定先只支持到 4K，由 owner 在验收时确认。
- **AC-R2-7 助手**（Epic AC-6）：**WHEN** 摄影师请助手「换成实时 GI，按 4K 导出」，**THEN** 助手用 `set_render` 写入 `engine` 和 `resolution`，用 `render_view` 回看实时 GI 的画面，再导出长边 4096 的图；工具说明的单测固定新字段的写法。
- **AC-R2-8 WebGL2 后端**（Epic AC-3 实时 GI 部分）：**WHEN** 在参考设备上用 `forceWebGL` 导出盒子间的实时 GI，**THEN** 与 WebGPU 后端的同一张图在 R1 的 AC-1 阈值内（`lit` 平均差 ≤ 1.5，差 > 8 的像素 ≤ 1%）；**WHEN** 只有 SwiftShader 的 WebGL2，**THEN** 实时 GI 照常出图，不报错。
- **AC-R2-9 性能**（Epic AC-7）：在参考设备上记录并判断：
  - 实时 GI 的视口在盒子间和 R0 性能场景里，交互时每帧 ≤ 33 ms（1× 像素比）；
  - 停止操作到停止重绘的时间；
  - 各档分辨率两种渲染器的导出耗时，2K 实时 GI 一张不超过 5 秒（K 标定后按实测修正，修正理由写进 evidence）；
  - 工作台的视口帧时间不高于 R1；
  - 连续导出 48 张实时 GI 图后 GPU 纹理数不增长。

  不满足时逐项列出差距交 owner 决定。
- **AC-R2-10 重绘对照**（Epic AC-8）：**WHEN** 同一机位分别用工作台和实时 GI 导出，交给 gpt-image-2.5 重绘，**THEN** 样例存进 evidence，owner 判断实时 GI 是否改善了重绘；结论回填 Epic 未决事项 2（新场景的默认渲染器）。
- **AC-R2-11 Safari**：**WHEN** 验收前在 Safari 26.5 人工打开编辑器，切到实时 GI 并导出一次，**THEN** 画面正确，结果记入 evidence。

## Impact

- 后端：
  - `backend/internal/creativescene/`：`model.go`（字段、缺省、校验）、`view.go`（完整读取补齐 `render`）、`type.go`（新建场景）、`ops.go`（「至少写一项」提示）、`fingerprint.go`（`engine` 计入 `clay`、`lit`）及对应测试；
  - `backend/internal/creativemedia/config.go`：总像素上限 67,108,864；
  - `backend/internal/creativeagent/scene_tools.go`、`instructions/base.md`：工具说明与系统提示；
  - `api/openapi.yaml` 与两份生成物（`httpapi/api.gen.go`、`frontend/src/api/schema.d.ts`）。
- 前端 `frontend/src/creative-scene/`：
  - `render/`：`sceneRenderer.ts` 加实时 GI 分支与累积，新增 `nodes/SSGINode.js` fork 及 GI 合成节点，`litLook.ts`（实时 GI 下的环境光补偿）；
  - `SceneEditor.tsx`、`panels/`：属性区选项卡、新的 `RenderPanel`，`EnvironmentPanel` 去掉曝光，`ExportDialog` 显示档位；`state/`：选项卡状态；
  - `Viewport.tsx`：收敛、实时 GI 的像素比、正交角标；
  - `host/render.ts`、`host/exportShots.ts`、`host/framing.ts`、`host/executor.ts`：按档位出图、累积 K 帧、逐帧让出主线程、JPEG、时限计算、高档位丢设备的处理。
- 测试：后端 `creativescene`、`creativeagent` 的单测；前端 `creative-scene-*.test.ts`；`creative-scene.e2e.mjs` 与 `creative-scene-agent*.e2e.mjs` 新增与修改的用例。
- 文档：`docs/dev/agent-skills-tools.md`（工具表与排空后升级记录）、`docs/dev/creative-scene-agent.md`（导出尺寸与格式）。
- 契约：交付 `data: 场景文档`（由 Epic 批准基线提取）。HTTP 接口契约不变，`.lti/contracts/api.md` 的场景接口沿用「字段以数据契约为准」。
- Living Spec：`creative/scene` 新增「渲染器与输出分辨率」，修改「导出分镜到画布」「导出图提示场景已更新」「编辑灯光与环境」。
- 依赖：不新增 npm 和 Go 依赖；`three` 仍锁定 0.186.1。

## Risk Notes

- **SSGI 降噪**：R0 试过的两种接法都错。本子项在设计阶段读源码确定接法（design「SSGI 降噪」），实施的第一个任务就在盒子间上验证；不成立时改用自写的联合双边滤波，仍不达标时交 owner。
- **性能**：实时 GI 的视口必须按 1× 像素比渲染（R0：2× 约 5 倍耗时），Retina 屏上会比工作台略软。几何多的场景 R0 没有测，本子项用性能场景补测；超出预算时先降采样档位，再交 owner。
- **长时间出图**：8K 实时 GI 一张约 75 秒、峰值约 9 GB（R0），8 GB 内存的 Mac 和显存 8 GB 以下的独显大概率失败，导出明确失败。8K 多机位的助手导出可能超过任务的 30 分钟期限，按超时失败处理，不为此改任务期限。
- **主线程占用**：K 帧累积加高分辨率后，同步出图会把续租挤过 30 秒租约。设计为逐帧等 GPU 完成并让出主线程；WebGL2 后端没有 `onSubmittedWorkDone`，用 fence 加 Worker 计时器轮询（不用 rAF，R0 证实 rAF 轮询在后台标签页里永远不返回）。
- **数据回滚**：上线后有场景写入新字段时，回滚到不认识它们的后端会读取失败，只做前向修复（Epic「数据与存储设计」）。
- **既有缺口**：完整读取现在直接返回存储的 JSON，老场景的 `render` 里连 `exposure_ev` 都没有（前端靠 `?? 0` 兜底），与数据契约「读取响应里带齐」不符；新字段是字符串，零值不是缺省值，老场景的任何写入都会因 `engine: ""` 被拒绝。本子项在解码处补缺省值、在完整读取里补齐 `render`，顺带修好 `exposure_ev`。
- **新增决定**（Epic 未规定、本子项设计里确定，详见 design「局部设计与偏差」）：
  - 属性区三个选项卡和自动切换规则；
  - SSGI 的降噪接法与合成方式（结清未决事项 10）；
  - 实时 GI 的场景 pass 布局与透明物体的处理；
  - N、K 的标定方法，单一固定 K（不按场景自适应）；
  - 时限公式与逐帧让出主线程；
  - 实时 GI 下的环境光补偿取值。
