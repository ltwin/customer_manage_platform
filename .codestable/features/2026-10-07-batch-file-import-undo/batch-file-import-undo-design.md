---
doc_type: feature-design
feature: 2026-10-07-batch-file-import-undo
status: approved
requirement: V6-008
tags:
  - creative-canvas
  - creative-media
  - frontend
  - backend
---

# V6-008 批量文件导入与整批撤销 · design

需求：[V6-008](../../requirements/creative-workspace-v6/items/V6-008.md)，PRD CV-90–94、CV-A2。owner 2026-10-07 已定：
- 撤销后，无论上传是否完成都取消，不做上传候选。
- 重做只恢复空节点，不重新上传。
- 节点只持有独立内容，不记录来源资产。

## 1. 现状（基线 cf412bd）

- `dropFiles` 先用一个 `add_node` batch 建空节点，按 +36/+36 斜排，然后对每个文件发起一次上传，目标是 `{kind:'node'}`。
- 不支持的文件直接过滤掉，只提示一句「不是支持的格式」，不逐项说明。
- 超过 `batch_limit` 的文件已经建了节点，但 `uploads.start` 会截掉它们，于是留下空节点。
- **一步撤销不成立**：每个上传完成后，worker 都会调 `BindNodeRevisionInTx` 另记一条 change。导入 3 个文件，就是 1 条 add 加 3 条 bind，⌘Z 要按 4 次。而且只撤 add 时，bind 已经改了节点的 data 字段，按字段核对会报 `ErrVersionConflict`。
- 撤销时不会取消进行中的上传。节点被撤掉后，上传完成会因为目标已变化而进入 `needs_review` 候选。
- 上传绑定时，节点标题保持原样，不会用文件名补默认标题。高度按比例更新：`fitMediaSize` 已经做到。

## 2. 方案

### 2.1 一批 = 一个 change group（后端，契约小改）

后端已有 `change_group_id`：batch 命令可以带上它，`undo` 也可以按 group 撤销，把组内所有 change 合并成一个反向 change。本条补上「上传绑定也进同一组」：

- `CreativeUploadNodeTarget` 新增可选字段 `change_group_id`（string，≤100）。upload 会话保存这个 target。worker 调 `BindNodeRevisionInTx` 时把 group 传给 `persistGraph`，bind change 就和建节点的 add change 进了同一组。
- 前端拖入时，生成 `group = crypto.randomUUID()`。add batch 带上 `change_group_id: group`，每个上传的 node target 也带上同一个 group。

撤销和重做的识别放在前端 `canvasHistory`。时间上，同组的 change 是连续的：add 在前，bind 跟在后面。
- **撤销**：如果撤销栈顶的 change 带 group，前端发 `{type:'undo', change_group_id}`。后端把组内 change 合并成一个反向 change，`inverse_of` 记为组内最后一条。前端看到这个反向 change 后，把同组的所有 change 一起移出撤销栈。这样一次 ⌘Z 就撤掉了节点和已经绑定的内容。
- **重做**：反向 change 本身没有 group，所以重做沿用现在的单 change redo，一次恢复整组。恢复的是撤销那一刻的状态：已经绑定的内容会随节点回来，因为 change 历史保留着这个 revision。没传完的已经取消了，对应节点回来时是空的。

这比 owner 定的「重做只恢复空节点」多保留了已经完成的内容。原因是 redo 是服务端按字段还原，要做到只恢复空节点，就得另写一种「部分重做」，不值得。请 owner 确认这个偏差是否可以接受（见 §5）。

撤销栈顶的组在撤销之后，如果还有迟到的 bind 到达，后端会因为目标已变化转成候选。这种情况由 §2.3 兜底。

### 2.2 撤销即取消上传（前端）

- `useUploads` 记下每个 item 的 `batch`（等于 group）和 `AbortController`，并新增 `cancelBatch(group)`：
  - 还没开始的 item 直接标为 `cancelled`。
  - 进行中的 item 先 abort 本机请求；如果已经拿到 upload id，再调 `/creative/uploads/{id}/cancel`。
  - 托盘里显示「已撤销」。
- 什么时候触发：
  - 撤销时 add 还没发出（被 `cancelTail` 取消），上传本来就没开始。
  - 发出了 group undo，就在发送时对这个 group 调 `cancelBatch`。
- 后端 cancel 只接受 `created/uploading/verifying` 状态。如果此时已经 `ready`，说明内容已经 bind 进组，会被 group undo 一起撤掉。前端吃掉这个 `ErrState`，不报错。

### 2.3 迟到成果不留候选（前端兜底）

上传完成时，`binding.status === 'needs_review'` 并且这个 item 属于已撤销的 batch，前端立刻调 `upload-candidates/{id}/discard`，不提示。其他来源的 `needs_review`（单节点上传时目标被别人改了）不在本条范围，维持现状。

### 2.4 布局、标题与格式说明（前端）

- **布局（AC-01）**：第 i 个节点放在 `x = 落点.x + i·330 − 150`，垂直方向让节点中心对齐落点。330 等于默认宽 300 加 30 间距，对齐原型 `canvas.mjs:298`。
- **标题**：去掉扩展名的文件名，最多 200 个字符。和现在一样，在 add 时写入。
- **整批选中**：不变。
- **格式说明**：
  - 不支持的文件和超出 `batch_limit` 的文件都不建节点，进入上传托盘，显示为 `rejected`，原因逐项写明：
    - 不支持的文件用 `classifyFile` 给出的原因。
    - 超出的文件显示「一次最多导入 N 个文件」。
  - 全部文件都不可用时，不建任何节点，只显示托盘。

### 2.5 拖到已有节点（AC-04）

现有规则保持不变：
- 只有一个文件、落点是同类空节点、节点有 `replace` 能力，才填进这个节点。视频节点接收视频文件。
- 其他情况一律新建节点。

本条补两件事：
- 补验收。
- 多文件落在空节点上时照常新建一批节点，不填充。

填充失败（上传失败）时，托盘提供重试，节点保持空，可以再次上传。

### 2.6 填充效果（AC-05）

- **只保留这一份版本，主版本为自动**：`BindNodeRevisionInTx` 和 `fill_from_asset` 都会清空 `selected_version_id`，并且不写版本行。这一点只补验收。
- **默认标题**：
  - 节点标题为空时，后端 bind 用上传文件名（去掉扩展名，最多 200 个字符）补上。规则和 `fill_from_asset` 用资产名补空标题一致。
  - 本条后端只改这一处。worker 已经知道 `upload.Name`，按 `strings.TrimSpace(n.Title) == ""` 判断后写入，不新增参数。
- **高度按比例**：`fitMediaSize` 已经做到，补验收。
- **可撤销**：
  - 填单个空节点的 bind 本身就是一条普通 change，撤销回到空节点，不需要 group（实现时简化，见验证记录）。
  - 从资产库填入本来就是普通 batch，可以撤销。
- **不记录来源资产**：按 owner 的决定执行。

## 3. 契约变更

- `CreativeUploadNodeTarget` 新增 `change_group_id`，可选，string，maxLength 100。完成后运行 `make generate`。
- 没有新表，也不需要迁移：upload 的 target 本来就以 JSON 形式保存。
- 后端还有两处行为变化，都不改接口：
  - bind 时，节点标题为空会用文件名补上。
  - 带 group 的 bind 写入 `change_group_id`。

## 4. 验证计划

- **Go**，带 DB，需要 Docker：
  - add batch 带 group，再完成两个带同一 group 的上传。按 group 撤销一次后，节点和内容都消失；redo 一次后整组恢复。
  - 不带 group 的旧路径行为不变。
  - 节点标题为空时，bind 写入文件名；标题不为空时保持原值。
  - 上传已 `ready` 后再 cancel，返回 `ErrState`。
- **前端单测**：`canvasHistory` 撤销整组后，同组的 change 一起出栈；节点横排布局函数。
- **mock e2e**：新建 `creative-file-import.e2e.mjs`。
  - AC-01：布局、标题、选中，混合格式逐项说明原因，超出数量的文件被拒绝。
  - AC-02：
    - 离线时撤销：节点被取消，没有发起上传。
    - 在线时：mock 对 group undo 删除节点，并检查对每个未完成的上传都调了 cancel。
  - AC-03：mock 返回 `needs_review` 时自动调用 discard。
  - AC-04、AC-05：拖到已有内容的节点上新建节点；拖到同类空节点上填充；视频节点接收视频；失败后可以重试。
- 跑全部纯 mock 的创意空间 e2e。真实后端的 e2e 照例说明没有运行。

## 5. owner 确认（2026-10-08）

重做时，已经传完的内容会随节点一起回来，只有没传完的是空节点。owner 同意这个偏差。
