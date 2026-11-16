# Diagnosis

来源：`egress-consent-account-level` 验收时发现的已知边界（owner Samson Lee 2026-11-14 选择另开 change 处理，不阻塞该 feature 结案）。本文件只做诊断，尚未授权修复。

## Symptom

前置条件：账号已同意发送前说明且开启素材库检索；有一份只在素材库里（不在当前画布、本对话此前也没引用过）的文字素材。

1. 摄影师发消息，助手第 1 轮调用 `read_asset` 读这份素材，结果是正文。
2. 第 2 轮请求发出后，摄影师在「外发说明」里关闭素材库检索。
3. 助手第 3 轮请求照常组装、派发。

实际：第 3 轮请求里仍带着 `read_asset` 读到的正文，运行正常结束；之后同一对话的新运行也会带上它。
期望：与素材库工具（`search_assets` 等）的结果一致——素材库关闭后，只因素材库才可读的内容不再随之后的请求发出，换成占位说明。

## Root Cause

置信度：高（已用测试复现，见下）。

- `context.go:91` `directlyReadableInTx` 的第二个分支：本对话里「序号在触发消息之前或属于本运行（`run_id=$4`）的完整消息」引用过的修订一律可读，不区分引用来自摄影师还是来自工具结果。
- `read_asset` 的结果消息在 `creative_message_content_refs` 里登记了这份修订（`role='result'`），消息属于本运行，于是这份修订在素材库关闭后仍按会话引用可读。
- `execute.go:703`/`execute.go:724` 派发复核对本运行工具读过的修订调用 `readableInTx`，命中上面的分支，复核通过。
- `context.go:348` 素材库关闭时的占位替换只认 `library_tools.go:49` `libraryResultTool` 列出的素材库工具，`read_asset` 不在其中（它也读画布和会话内容，不能整体替换）。

因果链：工具读入 → 登记为本运行消息引用 → 会话引用分支放行 → 关闭素材库后既不被复核拦下，也不被占位替换。

## Evidence(红)

验收期间写过的测试（未保留，按下面重写即可复现红）：在 `egress_scope_test.go` 里 `f.agree(alice, true)`，`f.revision` 建一份素材库文字素材；脚本第 1 轮调用 `read_asset{content_revision_id}`，第 2 轮内 `f.agree(alice, false)` 并再发一个工具调用，断言第 3 轮请求不含该正文。当前结果：运行 `succeeded`，第 3 轮请求包含正文。

## Blast Radius

`directlyReadableInTx` / `readableInTx` / `readableRevisionInTx` 的调用方都依赖「本运行消息引用即可读」：

- `context.go:bindMessages`（历史图片是否附图）
- `context.go:resolveReference`（引用正文）
- `execute.go:admitDispatchConsentInTx`（派发复核）
- `context.go:textureReadableInTx`（`edit_scene` 贴图槽位）
- `runtime.go` 工具读取（`read_asset`、`read_run_result` 等）、`edit_canvas` 按修订放入画布

图片同理：`read_asset` 读到的素材库图片在素材库关闭后仍会作为历史图片附带。`read_run_result`、`render_view` 结果登记方式相同，需要逐一判断是否同样受影响。

## Fix Direction

- 区分引用来源：摄影师在消息里亲手附带或 @ 的引用保持「已有会话引用按原许可读取」；工具结果登记的引用，若该修订此刻既不在画布、也不被摄影师消息引用、只因素材库可读，则在素材库关闭时视为不可读。
- 不可读时 `bindMessages` 把对应工具结果换成占位（沿用 `libraryWithheld` 的做法），历史图片不附带；派发复核据此保持一致。
- 需要 owner 拍板：工具结果是否应当始终跟随素材库开关，以及 `edit_canvas` 已把素材放上画布的情况（上画布后按画布可读，属正常）。
