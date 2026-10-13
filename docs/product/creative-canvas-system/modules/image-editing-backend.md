# 图片编辑一期：技术选型与后端设计

**档位 S1；推荐 Go 业务编排 + worker 内受控 libvips 子进程，异步只负责渲染，采用是同步画布命令。**

状态：技术设计第三稿，2026-10-12；已处理两轮评审意见，预览存储与回收按 owner 拍板改为「不计额度的预览 blob + 媒体 GC」。编解码穿刺（§11 第 1 步）已完成，结论见[穿刺记录](../../../../.codestable/features/2026-10-12-image-editing/image-editing-spike.md)，§8 已按结论回写；2026-10-13 owner 修订：压缩不做应用前预览（§5.1、§7、§10 已改）；其余部分未实现。对应[产品设计](../image-editing.md)。本文的「设计」是拟议契约，不是现有代码行为；「已验证」指本次直接读取源码或官方资料确认；容量数据为规划假设。

## 1. 目标、规模与边界

主链路：固定源图片 → 选质量 / 定裁切框 → 点击应用 → 真实输出候选 → 同步采用 → 新节点版本，可撤销。Agent 压缩直接进入相同处理及发布链路，无需模拟面板操作。

按小规模单体部署设计，假设同时 5 个编辑面板、每个面板约每 2 秒提交一次参数，即候选请求峰值约 2.5 次/秒；这不是实测流量。若每次编码需 1 秒，一个编码槽的处理能力约 1 次/秒，必须限制并发、合并参数和及时丢弃过时工作，不能无界排队。单张 60 MP 图片仅一份 RGBA 像素就约 240 MB，编码库、方向转换和现有校验还会增加内存。

优先级：账号隔离与不误覆盖 > 可恢复与结果真实 > 交互时延。S1 的原因是已有异步执行、重试、并发采用和对象存储，不是预设高流量。本次不设计分片、多地域、独立图片微服务或分布式缓存。

## 2. 已有能力与明确缺口

图索引代次为 2026-09-28，相关覆盖记录陈旧；以下依据当前工作树直接读取。源码符号定位优先于旧图中的行号。本次没有运行测试。

| 已验证事实 | 设计结论 |
|---|---|
| [generation_import.go](../../../../backend/internal/creativemedia/generation_import.go) `writeGeneratedMedia` 完成校验、建 blob、写对象、固定版本；`PrepareGeneratedMedia` / `CommitGeneratedMediaInTx` 在写对象前预留额度、提交时计入 stored 并建修订 | 抽出「校验并写入 blob」的公共部分供预览使用；预览不走导入的额度预留与修订提交 |
| schema 已允许 ready 但 `quota_booked_at` 为空的 blob；全仓没有扣减 `stored_bytes` 或物理删除对象的程序 | 预览写成不计额度的 blob；回收依赖本期补齐的[媒体 GC](media.md#6-gc恢复和上线约束) |
| [generation.go](../../../../backend/internal/creativecanvas/generation.go) `AdmitGenerationInTx`、`RetainGenerationOutputInTx` 与 [publish.go](../../../../backend/internal/creativecanvas/publish.go) 已支持图片生成结果 | 不再把文本 executor 的限制当作图片编辑的前置缺口；采用无需节点执行 |
| [versions.go](../../../../backend/internal/creativecanvas/versions.go) 已有 makeVersion / selectVersion；[media_ports.go](../../../../backend/internal/creativecanvas/media_ports.go) `fitMediaSize`（148 行起）保持宽度和坐标，仅超长媒体等比缩小 | 新命令复用版本、尺寸、图持久化及撤销，不单设裁切布局规则 |
| [agent.go](../../../../backend/internal/creativegeneration/agent.go) `GenerateNode`（125 行起）每 2 秒有界等待；只判断执行 state，不区分 apply_state | 压缩采用相同等待方式；生成工具的采用状态误报纳入实施修正项 |
| [canvas_tools.go](../../../../backend/internal/creativeagent/canvas_tools.go) 使用 run/tool-step 派生身份；现有 applyOnce 冲突会重新读取最新画布 | 复用稳定身份，不能复用这种刷新源版本后重试的语义；本次编辑源必须固定 |
| [creative_jobs.go](../../../../backend/internal/platform/store/creative_jobs.go)（81 行）当前 default=4、maintenance=1；Queue 白名单尚无 image_edit | 同一 worker 内新增 image_edit=1，并更新白名单；不是现有独立部署 |
| [verify.go](../../../../backend/internal/creativemedia/verify.go) 校验完整解码、提取主色，display 为 JPEG；[config.go](../../../../backend/internal/creativemedia/config.go) 默认 25 MiB、12000 单边、60 MP | 原文件编辑，透明预览读结果 original；资源预算包含 Go 校验 |
| [worker.go](../../../../backend/internal/creativemedia/worker.go) `SweepExpired` 只扫上传与上传候选 | 本功能不另写清理；预览 blob 由媒体 GC 按「无持有方」回收 |
| [canvas_tools.go](../../../../backend/internal/creativeagent/canvas_tools.go) `edit_canvas` / `generate_node` 写画布时不在事务内复核 run 资格 | 压缩工具与之对齐，不单独增加事务内 run 钩子 |

attached 持久续跑不作为本期可复用能力，后续单独设计。[Dockerfile](../../../../Dockerfile) 已有 `creative-worker` 目标（Alpine 3.22 + libvips 8.16.1 + choom，§11 第 1 步补齐）。来源授权沿用 [RetainDerivativeRights](../../../../backend/internal/creativecontent/derivation.go)。

## 3. 技术选型

| 方案 | 结论 | 原因 |
|---|---|---|
| 浏览器 Canvas 编码最终文件 | 不采用 | Agent 无浏览器环境，浏览器编码差异会形成两套结果语义；前端只负责选区和展示 |
| 现有 Go imaging 全包办 | 不采用 | 可裁切及编码 JPEG/PNG，但公开 Encode 格式没有 WebP，不能独立满足一期透明输出策略 |
| Go 内嵌 libvips / CGO binding | 一期不采用 | 会改变现有静态构建方式，并把原生解码的故障和内存带进 Go 进程 |
| **Go 调 libvips CLI 子进程** | **唯一推荐** | 保留 Go 构建方式；图片处理依赖放入 worker；可按任务终止进程；同容器的资源仍共享，见 §10 |
| 云图片处理或新增 Node/Python 服务 | 不采用 | 当前没有需要增加网络依赖或独立服务的证据 |

已验证的外部依据：imaging 的[官方说明](https://github.com/disintegration/imaging#frequently-asked-questions)列出有限编解码格式；libvips 提供[CLI 操作入口](https://www.libvips.org/API/current/using-the-cli.html)、[WebP 编码与质量参数](https://www.libvips.org/API/8.17/method.Image.webpsave.html)、[ICC 色彩转换](https://github.com/libvips/libvips/wiki/HOWTO----Image-shrinking)。本次通过 Context7 查询原项目资料，并补查 libvips 官方文档。具体发行版本及构建摘要在镜像穿刺后锁定，不使用浮动 latest。

CLI 的代价必须明确：官方 CLI 文档说明，多操作之间需要中间文件，无法直接复用库内流水线，因此比直接绑定慢。本方案为沿用现有构建和子进程取消边界接受这项代价，不能引用 libvips 的库内性能来承诺本链路的速度。

推翻条件：若一期明确移除 WebP 和色彩管理要求，可重评纯 Go；若实测 CLI 多阶段临时文件成本无法满足时延 / 空间预算，再比较单个原生 helper 与 Go binding。未经样张和资源实测，不先引入第二种编码后端。

前端继续使用现有画布坐标体系实现裁切 overlay，不新增画布编辑器；仅在坐标和可访问性穿刺证明自有 overlay 成本过高时评估裁切库。本文不冻结前端组件 API。

## 4. 模块归属

`creativeimageedit` 拥有候选、参数策略、渲染 job 和受控 CodecRunner；`creativemedia` 继续拥有原对象精确读取、pin、配额、读票据与 GC，并新增两个窄接口：

- `StorePreviewBlobs`：校验输出并写入 original / display blob，置 ready、不计额度，同时写入 blob 主色，返回 blob ID 与实际统计。与 `writeGeneratedMedia` 共用校验和写对象代码。
- `BindPreviewInTx`：锁 blob 并确认 ready → 计入额度（`stored_bytes` 增加并写 `quota_booked_at`，超额返回空间不足）→ `WriteMediaAndRetain` 建修订。

`creativecanvas` 提供同步的 `ApplyImageEditInTx`，内部沿用 makeVersion、fitMediaSize、persistGraph。外层图片服务拥有完整 operation 回执与候选状态事务，手工和 Agent 调用同一采用逻辑。canvas 不反向 import 图片包，候选的受信 blob 和参数由上层传入，禁止客户端自选 blob 或结果修订。

不扩展 NodeExecutor、RequestExecutionInput、ExecutionObservation，不增加 RequestImageEditExecution、action_payload、attached 续跑或节点执行记录，不更改 draft_revision / prompt_snapshot 的 NOT NULL 约束。

## 5. 三个入口，共用渲染和同步采用

### 5.1 手动压缩

与裁切相同（owner 2026-10-13 取消应用前预览）：固定源修订 → 点击「应用压缩」时创建候选 → image_edit job 生成真实文件 → 前端等 ready 后调用同步 apply → 返回回执并展示实际大小变化。调整质量不发起请求；候选不占 active_execution，采用前不创建历史版本，采用使用编码出的原文件，不再编码。下文「预览 blob」指候选编码出、尚未绑定修订的结果文件，不再供应用前展示。

### 5.2 手动裁切

拖动只在本地显示选区；点击「应用裁切」创建候选。前端在编辑页面仍有效且本次意图未取消时等待 ready，再调用相同的同步 apply。页面提前关闭或取消后不调用 apply，后台渲染不能代为修改画布。若 apply 已经发出，关闭页面不能撤销已提交命令，重新进入时通过原操作回执核实。

### 5.3 Agent 压缩

1. 用 `run_id + tool-step` 派生固定 create / apply / discard 操作 ID，创建固定源及参数的候选。请求哈希包含 `client_created_at`，因此重放先按操作 ID 查回执再决定是否提交；重放不重新读取节点当作新来源。
2. 工具调用内部每 2 秒查询一次，只等同一候选；不让模型反复调用查询。等待上限建议 90 秒，同时受父 run / 工具 deadline 限制，渲染本身仍最多 60 秒。排队时间计入工具等待预算。
3. ready 后复核 Agent 当前有效执行权，并调用同一同步 apply；只在收到已提交回执时报告「已更新」。不重新读取最新内容以绕过冲突。
4. 等待超时且尚未发出 apply：报告「未应用」，停止自动采用并丢弃候选；清理失败可由过期扫描接管。取消 / 进程崩溃后没有后台自动采用者。
5. apply 已发出而响应未知：查询原操作，显示「正在确认」，不能误报「未应用」或直接丢弃。已提交则回放；未提交且 run 资格已失效则拒绝新写。等待预算到期不等于提交失败。

Agent 写入与现有 `edit_canvas` / `generate_node` 对齐：工具只在存活的 run 内执行，取消通过 context 生效，不单独增加事务内 run 资格钩子。取消与已发出写入之间的窗口是全部 Agent 画布写入共有的，需要收紧时单独立项统一处理。重放先按操作 ID 查回执（同 `applyOnce`），不持久化 CreatedAt，也不沿用其冲突后读取最新画布重建请求的做法。

同一 render 被当前工具处理时只允许一个 apply 命令效果。重放先找回执，再看候选；超时丢弃的旧调用不能凭同一身份重新排队。真正重试是新的明确调用。

**现有生成工具的修正范围**：仍保留 GenerateNode 的有界等待；返回 execution_state 与 apply_state。succeeded/pending 继续等到预算耗尽，conflicted/discarded/expired 明确「未采用」，只有 applied 可以表达当时已更新。查询 / 展示还需区分后来人工切换版本；不能把历史已应用说成现在仍选中。此项目前仅列入实施，不声称已经修复。

## 6. 候选表与状态

新增 `creative_image_edit_renders`，只描述候选文件；不新增节点任务表。

| 字段组 | 内容 / 约束 |
|---|---|
| 身份 | id、account_id、operation_id、request_hash，账号内身份和操作唯一 |
| 源与目标 | canvas_id、node_id、source_revision_id、精确源对象引用；不可改变 |
| 参数 | action、schema_version、规范化 parameters、encoder_policy_version |
| 调用归属 | caller_kind；Agent 的 run / tool-call 身份由服务端填写 |
| 状态 | state、revision、deadline、expires_at、error_code；无 lease_epoch / lease_until |
| 产物 | original_blob_id、display_blob_id、实际 MIME / 宽高 / 大小 / 摘要；ready 才设置 |
| 已采用结果 | applied_operation_id、result_revision_id、applied_version_id、change_id；只有 applied 才设置 |

候选状态仅八个：**queued / running / ready / not_reduced / failed / discarded / expired / applied**。

- queued → running：job 开始；重复 job 允许恢复同一候选，终态立即返回。
- running → ready：预览 blob 已写入并置 ready 后，在短事务内确认候选仍为 running 且未被取消，记录 blob ID 与统计。
- running → not_reduced：压缩无收益，不保存可采用输出。
- queued/running → failed：确定永久失败或最终重试耗尽；瞬时错误由 River 重试同一候选，不来回重置状态。
- queued/running/ready → discarded 或 expired：取消、被替代、超时，旧工作不能复活候选。
- ready → applied：同步采用和版本登记同事务，applied 不回退。撤销改变图历史，不把候选重新置 ready。

render 不需要 epoch 或 lease。同一候选被重复投递时，每次 attempt 各自写新的 blob；只有第一个在条件更新（`state='running' AND original_blob_id IS NULL`）中成功的 attempt 被记录，其余 blob 没有持有方，由媒体 GC 按 15 分钟宽限回收。认领后最终 ready 事务失败或进程异常退出时，重投递在账号写屏障下把旧认领的 verifying blob 转为 deleting，并清空认领后使用新 blob 身份重试；迟到的旧写入必须同时满足 blob 状态与候选 blob 身份才能提交，不能覆盖新尝试。候选已 ready 或终结时 job 直接返回，包括此前 ready 提交成功但响应未知的情况。旧 job 的瞬时错误只返回队列，不得覆盖已完成的状态；最终失败须与候选当前状态复核。

持有关系：queued / running / ready 且未过期的候选持有其源修订（保证编码期间源不被回收）和预览 blob，登记进媒体 GC 的持有方清单。applied 后预览 blob 已绑定到新修订，由版本持有；discarded / expired / not_reduced / failed 不再持有任何东西，不需要专门的清理程序。origin_kind 使用现有 TEXT 的 `edit` 值；复用现有 Provenance JSON / generation_provenance 列，以 schema_version + action 区分编辑说明。接受 API 名称暂时别扭，不新增 edit_provenance 列，不为改名迁移历史数据；API、历史 UI 按 origin_kind 解释。

## 7. HTTP 与工具契约

路径相对既有创意 API 前缀；写入 OpenAPI 并生成类型。账号不由请求输入。

| 接口 | 语义 |
|---|---|
| GET /image-edit-capabilities | 环境开关和静态策略：格式、质量、像素上限、策略版本；不声称证明实时 worker 健康 |
| POST /image-edit-renders | 固定操作身份、canvas/node/source、action/parameters；202 返回候选 |
| GET /image-edit-renders/{id} | 返回候选状态、实际统计、过期时间及 applied 回执定位 |
| POST /image-edit-renders/{id}/discard | 幂等释放候选；applied 只能返回已采用事实，不能逆转为 discarded |
| POST /canvases/{canvas_id}/nodes/{node_id}/image-edits/apply | 只接收 render_id、operation_id、expected_data_revision、expected_source_revision_id；同步 200 返回 version_id、change_id、最新 revision 和应用结果 |

compress 参数为 quality 整数 1–100；crop 为方向校正后的整数像素 left/top/width/height。整数框先由前端映射、取整并回显，后端拒绝越界，不重新按比例推导另一选区。上述 apply 不接受压缩参数、不创建 execution、不返回 202。

工具 key 为拟议 `compress_image`。使用现有 Agent 工具注册和有界调用方式；如映射到统一目录，其 execution 分类只表示工具包含耗时渲染，不意味着必须写 creative_node_executions。实现同时校验 media_write、画布可写、任务范围及 Agent 执行资格。MayCharge=false / MayEgress=false 只描述本地处理，不描述 Agent 对话模型。

错误：非法参数 400；不可披露对象 404；权限不足 403；source_changed / node_busy / idempotency_conflict / render_not_ready 409；过期 410；大小 413；格式 415；无收益或颜色错误 422；账号并发候选已满或超出创建频率时 429；服务不可用 503。沿用项目统一错误封装。存储额度只在 apply 时检查，超额用已有配额错误映射，候选保持 ready，有效期内释放空间后可再次应用。GET 对业务失败仍正常返回 state/error_code。

## 8. 编码策略与安全边界

采用版本化策略 `image-edit-v1`：显式 JPEG/PNG/WebP loader → 检查头部、字节、像素及动画 → 按 EXIF 校正方向 → 按内嵌 ICC 转换到 sRGB → 可选整数框裁切 → 编码 → 再校验实际文件。方向修正包含镜像方向，不仅处理 90° 旋转。

- 压缩：JPEG → JPEG；无透明像素 PNG → JPEG；透明 PNG / 静态 WebP → WebP。Q 使用传入 1–100；WebP alpha_q 固定 100。质量数值在不同格式间不代表相同视觉损失。
- 裁切：PNG → PNG；JPEG → JPEG Q95；WebP → lossless WebP，防止基础裁切再损失其已解码像素。PNG/WebP 更大仍允许裁切，但须满足输出大小及配额上限。固定策略的改变需递增版本。
- 无 ICC 的普通 RGB 图按 sRGB 处理；有有效 ICC 则转换。未标定 CMYK、损坏 ICC 返回明确错误，不能猜测颜色。libvips 加载时会静默丢弃无法解析的 ICC，因此先在 Go 侧扫描容器结构：文件声明了 profile 而加载后没有，即判为颜色不可知。16 位输入统一输出 8 位（含 PNG 裁切）。支持范围以穿刺样张为准写入 capabilities。
- 输出只保留 sRGB ICC，移除 EXIF/GPS/XMP；源文件不变。应用前明确告知格式与元数据变化，不能以编码默认值代替该策略。

CLI 通过 `exec.CommandContext` 的独立参数调用，不经过 shell，不接受自定义命令、文件路径、URL、loader 名或编码参数字符串。文件名由服务端生成；libvips 方括号格式选项完全由白名单常量及已验证数值构造。运行目录私有、禁止网络输入；源文件按已校验类型显式调用 jpegload / pngload / webpload，从不自动识别，SVG/PDF 等 loader 不可达。不设 `VIPS_BLOCK_UNTRUSTED`：libvips 8.16 把 vipsload 也标为 untrusted，设置后连自己的 `.v` 中间文件都读不了。APNG 与动画 WebP 同样由 Go 侧扫描拒绝。

多阶段使用无损内部临时格式，禁止 JPEG 作为 autorotate / 色彩转换 / 裁切的中间文件；只在末尾做有损编码。中间文件可能数百 MB，必须有磁盘预算和启动清扫；终止要覆盖进程组和临时文件，不只取消 Go context。库及依赖版本随 worker 镜像锁定。

已有 verifier 会再次完整解码，内存预算必须包含它；libvips 的低内存模型不能证明整个链路低内存。其[缓存内存设置](https://www.libvips.org/API/current/func.cache_set_max_mem.html)也不是进程硬上限。本期 image_edit 队列与其他任务共用 worker 容器，队列只限并发，不构成硬隔离。按 §10 做资源预算及子进程 OOM 倾向设置；不承诺其他任务绝不受影响。

## 9. 事务、幂等与回收

只有三个业务事务边界：

1. **受理候选**：权限、固定请求、创建频率、节点与账号并发上限和 jobs 入队原子提交；不占节点执行槽。
2. **记录预览**：编码、校验、写对象都在事务外，由 `StorePreviewBlobs` 完成，blob 置 ready 但不计额度；随后短事务锁候选，条件更新为 ready 并记录 blob ID 与统计。没有修订、没有额度变动。
3. **同步采用**：操作回执幂等串行边界 → 锁画布 → 锁候选 → 校验 ready、未过期、当前源、expected_data_revision、无 active_execution → `BindPreviewInTx`（锁 blob、计额度、建修订）→ RetainDerivativeRights → makeVersion(edit) → 设 selected / content → fitMediaSize → persistGraph → 候选 applied 与回执。全部一个事务，不碰外部存储，不复制对象，也不重新校验。

压缩不得洗掉源素材限制，派生授权在第 3 步写入。版本登记与尺寸改变归入同一撤销记录；超长图沿用 fitMediaSize 上限。采用冲突不改节点、不建版本、不计额度；候选在有效期内保留，但不能自动把 expected_source 改成新内容重试。

锁序：账号写屏障 → 画布 → 候选 → blob。Discard / 过期 / supersede 不得先锁候选再反向锁画布。编码和网络不持事务锁；正式实现用具体调用链验证锁序，不能仅凭本表宣称无死锁。

候选重复 job 可以浪费少量本地计算和临时 blob，但不能产生重复版本或重复额度；正常运行仅一个 image_edit 槽。瞬时故障退避重试，至多现有队列允许的 5 次，受候选总期限约束。永久错误、无收益、重复 OOM 不重试。

**回收**：本功能不写专门的清理程序。被取消、丢弃、过期、失败的候选不再持有预览 blob，由[媒体 GC](media.md#6-gc恢复和上线约束)按「从未绑定」类别 15 分钟宽限回收；写到一半的 verifying blob 按 GC 的 lease 规则回收。预览 blob 从未计额度，回收也不涉及额度账。所有异步 worker 都没有画布自动采用权限；采用请求响应未知只能查原回执。

## 10. 队列、并发与资源

同一 creative-worker 进程新增 River `image_edit` 队列，并发 1；已有 default=4、maintenance=1 保留。增加队列白名单和可信 handler 映射，不新增独立 worker 部署。压缩与裁切都只在点击应用时创建候选，不再有调参试算与替换（owner 2026-10-13）：

- 在画布锁下受理：同一节点同时最多一个未终结候选（queued / running / ready）；同一操作重放返回原候选，不同操作返回 409 node_busy。
- 每账号最多一个运行和一个排队候选，超出返回 429，提示稍后重试；实际存储配额不足仍明确报错。
- 被取消的运行中候选标为 discarded，提交出口不会发布 ready；尽力停止其进程，停止不了也不能采用其结果。它占据的编码槽在工作退出前不提前释放。

候选使用 2 分钟处理 deadline、ready 后 30 分钟 TTL；工具等待上限见 §5.3。关闭页面尽力 discard，断网时由 TTL 兜底。

Linux 下在受控子进程 exec vips 之前设置其 `oom_score_adj=1000`，不得改 Go worker 自身评分；启动自检验证设置成功。可用固定 launcher 完成设置和 exec，不拼接 shell，也不在进程已大量分配后再补设置。[Linux 手册](https://man7.org/linux/man-pages/man5/proc_pid_oom_score_adj.5.html)说明它调整 OOM 选择倾向；[cgroup 文档](https://docs.kernel.org/6.18/admin-guide/cgroup-v2.html)说明 memory.oom.group 可能导致整组终止，因此这不是硬隔离。

容器预算按 worker 基线（包含并行 Agent / 生成任务）+ 一次 vips 峰值 + 一次 Go 校验峰值 + 临时页缓存与安全余量实测。vips 退出后 Go verifier 仍可能 OOM，此时已经没有 vips 可先杀。若目标环境容量无法覆盖，降低 image_edit 的像素上限并由 capabilities 如实显示；不要承诺 60 MP 在任何部署都能编辑。独立 worker 部署只在实测资源争用要求时另评。

额度：候选结果在采用前不计额度，只有采用的那一份（original + display）在 apply 事务内计入；源图不重复计费。正常情况下每个候选都会被采用，未采用的只来自取消、失败、超时与冲突，由 GC 15 分钟宽限后回收；另加每账号创建频率上限（建议 10 分钟 60 次），防止脚本反复提交。每槽临时目录至少 1 GiB 仅为起始预算，须实测并清理。

## 11. 迁移、发布与实施切片

前置：[媒体 GC](media.md#6-gc恢复和上线约束) 先上线并开启删除，候选登记为源修订与预览 blob 的持有方。GC 未开启时不开放本功能，否则预览 blob 只增不减。

迁移只新增候选表及索引 / 约束；不改 execution 表，不新增 provenance 列，不回填历史图片。新增 edit 的 API 枚举 / 展示分支，但现有 JSON 结构和版本哈希机制复用，仍需兼容回归。渲染器读取已有 image-edit 来源时不依赖生成 prompt。

单一部署使用明确环境开关和相同策略配置；功能开启时 worker 启动检查 vips / codec / ICC / OOM 设置，失败就启动失败；关闭时不注册工具。capabilities 返回静态策略及开关，不引入 worker 心跳、能力租约或动态版本协商。它不是运行健康证明，worker 意外退出由进程监控发现，已受理候选受 deadline 限制，不永久转圈。

发布顺序：新增 schema 与根支持 → 兼容 edit 读取的 API / worker / 清理代码和依赖镜像 → 编解码自检及样张 → 开功能。回滚先停受理、明确取消候选并清理；有 edit 数据时保留兼容的读取 / GC 版本，不直接 down migration 或让旧 GC 清未知根。

1. 编解码穿刺（2026-10-12 已完成，见[穿刺记录](../../../../.codestable/features/2026-10-12-image-editing/image-editing-spike.md)）：补带 libvips 的 worker 镜像构建目标（固定发行版本），在该镜像内验证 JPEG/PNG/WebP、透明、全部方向、ICC、动画拒绝，测 12/24/60 MP 全链路 RSS / 时延 / 磁盘；生产资源实测随创意空间发布补做。
2. 候选闭环：结果 blob 写入（不计额度）、节点与账号并发上限、重复 job、取消与过期、未采用候选的 blob 由 GC 回收、队列并发。
3. 同步 apply：绑定与计额度、版本 / fitMediaSize / persistGraph / 回执 / 撤销；测试来源冲突、超额、active execution、并发取消，以及 apply 与 GC 标记竞争。
4. UI 和 Agent 有界等待：页面关闭不后台采用；工具超时与 apply 提交未知区别；补现有 GenerateNode 的 apply_state 返回和展示判断。

重点测试：跨账号和来源撤销、重复 job/回执、重复点击应用与节点忙、预览 blob 回收、GC 与采用竞争（候选过期瞬间 apply 必须得到 410 或成功二选一）、Agent 取消与 apply 竞争、候选过期后的操作回放、生成 state/apply_state 组合、历史版本切换尺寸一致。生产资源穿刺不能由数据库单测代替。

观测 render/operation、格式、像素、处理/排队时延、失败码、not_reduced、未采用候选的数量与原因、未绑定预览 blob 的数量与字节、apply 冲突与超额；不记录签名 URL、凭证或原图字节。

## 12. 本轮评审处理与决定

本轮采纳：输出先校正方向、转换 sRGB，再去掉 EXIF/GPS/XMP，只保留 sRGB ICC；透明 PNG 压缩转 WebP；采用改同步命令、Agent 有界等待；裁切及历史版本切换统一沿用 fitMediaSize。源图始终保留。上述已作为修订方案写入产品稿，不将其冒称为已实现或已通过验收。

删除上一版的 RequestImageEditExecution、action_payload、媒体 executor 分支、transferred、独立交接事务、edit_provenance 列、worker 心跳。保留 libvips CLI、白名单、无损中间文件、真实大小及 not_reduced 语义。

第三稿（owner 2026-10-12 拍板）：媒体 GC 本期实现，同时解决资产库永久删除不释放额度；预览改为不计额度的 blob，采用时绑定并计额度，不复用 PrepareGeneratedMedia、不写专门回收程序；GC 宽限为已计额度 24 小时、预览 15 分钟，开启删除前生产只读扫描 3 天；资产库永久删除确认文案与「待释放」本期一起做；Agent 写入的运行资格校验与现有工具对齐。

工程必验点：GC 持有方登记完整（含候选）；OOM 优先级只是降低风险，资源预算与 cgroup 行为必须在部署环境验证。attached 持久续跑独立推进，不是本期依赖。

本次只修订设计并核对源码 / 官方资料。尚未修改功能代码、修复现有生成工具、安装依赖、执行测试或开展新一轮独立评审。

2026-10-13 owner 修订：压缩不做应用前预览，点击应用后编码一次并直接采用，流程与裁切相同。删除预览票据接口、编辑会话号 / 请求序号与试算替换策略；每个节点同时最多一个未终结候选。原先为大量试算预览准备的「未计额度 blob 直接永久删除」不再需要，GC 删除方式不变。
