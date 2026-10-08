---
doc_type: feature-verification
feature: 2026-10-07-batch-file-import-undo
requirement: V6-008
status: passed
date: 2026-10-08
---

# V6-008 验证记录

设计：[batch-file-import-undo-design.md](batch-file-import-undo-design.md)。前后端都有改动，契约只多了一个可选字段。

## 实现取舍

- 填充单个空节点不使用撤销组。这一步的 bind 本身就是一条普通 change，撤销后节点回到空的状态。design 里原本写的「单独建一个组」没有必要，已经简化掉。代价是：单个节点的填充还在上传时按撤销，不会取消这个上传。上传完成时节点已经变了，结果会落成候选，界面上仍然显示原来的「目标已变化，待处理」提示。批量导入不受这个影响。
- 组被别的改动隔开时（比如上传还没完成就拖动了节点），撤销退回到逐条撤销。原因是服务器按组撤销会把组里的所有改动一起回退，跨过中间那条改动就不对了。
- 撤销时如果取消没赶上（会话已经进入发布），前端会等这个会话结束：已经绑定的内容随撤销组一起回退；落成候选的，直接调用 discard 放弃，不提示。
- 拖进来但没有建节点的文件（格式不支持、超出数量）会出现在托盘里，逐项写明原因，并且不提供重试，因为没有目标节点。

## 门禁

- Go：`make check-go PKG=./internal/creativemedia/...` 和 `PKG=./internal/creativecanvas/...` 都是 0 issues，测试全部通过（需要 Docker）。
- `make generate` 已重新生成，`make generate-check` 没有漂移。
- `npm run lint` 退出码 0；`npm run build` 通过；`make test-frontend` 411/411，其中新增 7 条：`creative-file-import.test.ts`。
- mock e2e：新增的 `creative-file-import` 6 个记录全部通过，连续跑 3 次都稳定。另外 12 个纯 mock 的创意空间 e2e 也全部通过：node-toolbar、node-create、canvas-viewport、node-resize、node-label、save-status、canvas-interaction、canvas-baseline、composer-baseline、library、library-baseline、generation。
- 回归有效性：把 7 个前端文件换回 HEAD 后，AC-01、AC-02、AC-03、AC-04 失败。另外两项在 HEAD 上也通过：
  - AC-02 offline：改动前，离线撤销就是在本机取消，上传本来就不会开始。
  - AC-05：标题和高度由 mock 服务端写入，前端只负责显示。这两项的真实逻辑在后端，由 Go 测试覆盖。

## 验收对应

| AC | 证据 |
|---|---|
| AC-01 横排间距 330，标题去掉扩展名，整批选中；混合格式逐项说明原因 | e2e AC-01：拖入 5 个文件，只建 3 个节点（image / video / image），标题为「晨光.v2」「街拍」「c」，x 方向间距 330，第一个节点中心对齐落点，3 个节点一起被选中。托盘里 notes.txt 显示「暂不支持此文件格式」，d.png 显示「一次最多导入 3 个文件」，两项都没有重试按钮。单测覆盖了 `importPlan`、`importSlot`、`fileTitle`。改动前超出数量的文件也会建节点，然后留成空节点；现在不会了 |
| AC-02 整批一步撤销，不留空节点、不重复上传 | Go `TestBatchImportUndoesAsOneGroup`：add 加 2 次 bind 共 3 条 change，属于同一组；按组撤销一次，节点和内容都消失；重做一次，2 个节点连同内容一起恢复。e2e AC-02：两个文件中一个已经绑定、一个还在上传，按一次 ⌘Z 发出按组撤销（读集是这一批的两个节点），两个节点都消失；正在上传的那个被取消，托盘显示「已撤销」；画布上没有多出节点，也没有重复上传。e2e AC-02 offline：离线时撤销直接在本机取消，不发起任何上传。单测：同组的改动合成一步撤销，反向改动把整组一起弹出；组被隔开或组里只有一条改动时，按 change_id 撤销 |
| AC-03 撤销后迟到的成果不覆盖节点，也不留候选 | e2e AC-03：撤销时取消请求返回 409（没赶上），之后上传落成候选，前端自动调用 discard，节点没有回来，也没有出现「目标已变化」提示。Go：上传 ready 之后再 cancel，返回 `ErrState` |
| AC-04 拖到有内容的节点上新建节点；只有同类空节点可以填充；视频节点接收视频；失败可以恢复 | e2e AC-04：拖到有内容的图片上，新建节点，原节点不变；mp4 拖到空视频节点上，直接填充，不建节点；mp4 拖到空图片节点上，新建节点；填充的上传失败时节点保持空，托盘出现重试，点重试后节点被填充 |
| AC-05 填充后只保留这一份版本、主版本为自动；默认标题换成文件名；高度按比例；可以撤销 | Go `TestBatchImportUndoesAsOneGroup`：标题为空的节点在 bind 后改为「窗边.晨光」（只去掉最后一个扩展名）；已有标题「保留」不变。`BindNodeRevisionInTx` 会清空 `selected_version_id`，不写版本行，高度由 `fitMediaSize` 按比例计算。e2e AC-05：空节点填充后标题显示为「窗边」，高度按 3:2 更新，一次撤销后回到空节点（按 change_id 撤销）。按 owner 的决定，不记录来源资产 |

## 未验证

- 真实后端下的整条拖入流程没有在浏览器里跑：上传 worker、真实的撤销组、取消与发布的竞态。后端组撤销和标题补全由 Go DB 测试覆盖；前端编排只在 mock 下验证过。
- `creative-media`、`creative-canvas-commands` 这两个真实后端 e2e 没有运行。
- 托盘里「已撤销」「一次最多导入 N 个文件」等文案的视觉效果，交给 owner 走查。
