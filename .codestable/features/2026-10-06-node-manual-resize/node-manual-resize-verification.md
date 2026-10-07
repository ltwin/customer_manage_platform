---
doc_type: feature-verification
feature: 2026-10-06-node-manual-resize
requirement: V6-004
status: partial
date: 2026-10-06
---

# V6-004 验证记录

设计：[node-manual-resize-design.md](node-manual-resize-design.md)。尺寸约束和越界处理由 owner 在 2026-10-06 拍板。

## 门禁

- `make check-go PKG=./internal/creativecanvas/...`：build、golangci-lint、测试全部通过。依赖包 `creativegeneration`、`creativeagent`、`creativemedia`、`platform/httpapi` 也通过。
- `npm run lint` 退出码 0，`npm run build` 通过，`make test-frontend` 400/400。
- mock e2e：新增的 `creative-node-resize` AC-01–05 全部通过；另外 8 个纯 mock 的创意空间 e2e 也全部通过（node-label、save-status、canvas-interaction、canvas-baseline、composer-baseline、library、library-baseline、generation）。
- 回归有效性：把 4 个前端源文件换回 HEAD 后，AC-01、02、04、05 失败。AC-03 仍然通过，因为「一次拖动一步撤销」在改动前就已成立，本条只是补上验收。

修改的既有断言：`creative-canvas-commands.e2e.mjs` 原来验证组可以从 8 个方向缩放，现在改为断言组没有缩放手柄。这个测试需要真实后端，本次没有运行。

## 验收对应

| AC | 证据 |
|---|---|
| AC-01 只有单选普通节点显示缩放入口，组不能手动缩放 | e2e AC-01：单选时有手柄，多选时没有，组没有。Go `TestResizeNodeLimits`：组的 `resize_node` 被拒，快照里组的能力不含 `resize`；`TestValidResize` |
| AC-02 图片/视频等比，文本/链接/音频自由，拖动时实时显示尺寸 | e2e AC-02：16:9 图片拉宽后比例不变；文本 +60/-40、音频 -40/+40 都能自由缩放；拖动中显示「W × H」，松手后消失 |
| AC-03 一次调整一次撤销，尺寸随画布保存，刷新后保持 | e2e AC-03：一次拖动一步撤销；恢复网络后发出一个请求，内容是 `move_node + resize_node`。尺寸持久化走既有 outbox 和快照，`creative-canvas-commands` 的「快照回读」覆盖这一点（需要真实后端，本次没跑） |
| AC-04 前后端尺寸约束一致，越界有明确处理 | 前端拖动时卡在边界：e2e AC-04 中文本最小 160×100、最大宽 1200。后端 `TestResizeNodeLimits` 用实库验证：159、1201、高 99 都返回 `ErrValidation`，160 和 1200 通过。两端常量都有单测 |
| AC-05 媒体保持宽度、高度跟内容比例 | **部分完成**，见下文 |

### AC-05 拆分

已满足：
- 读不到原图宽高时保持原高度：e2e AC-05 的 ghost 节点保持 260 高；前端单测覆盖了 `fitMediaHeight` 的 fallback 参数。
- 有内容的媒体按当前宽度和内容比例算高度：e2e AC-05 的 wide 节点 400 宽、225 高；`creative-canvas-interaction` e2e 里切换预览版本后高宽比变成 1.5。
- 生成、上传、填充、选定版本、手动缩放后，服务端都按当前宽度重算高度：这几条路径分别是 `publish.go`、`media_ports.go BindNodeRevisionInTx`、`graph_commands.go` 的 fill / select_version / resize_node，都调用 `fitMediaSize`，由 `TestFitMediaSizeTracksContentAtCurrentWidth` 覆盖。

未满足：
- 空媒体节点改比例 chip 时按当前宽度重算高度。比例 chip 还没实现，归 V6-021 交付，V6-021 AC-03 的要求相同。本条暂不勾选 AC-05，等 V6-021 交付比例 chip 时一起验收。

## 未验证

- `creative-canvas-commands`、`creative-media` 需要真实后端，本次没有运行。
- 视觉走查（尺寸提示的位置和样式）交给 owner。
