---
doc_type: feature-design
feature: 2026-10-06-node-label-meta-defaults
status: approved
requirement: V6-003
tags:
  - creative-canvas
  - frontend
  - backend
  - openapi
---

# V6-003 节点标题元信息、空态与默认尺寸 · design

需求：[V6-003](../../requirements/creative-workspace-v6/items/V6-003.md)，PRD CV-10/12/14、CV-A0。

## 1. 现状（2026-10-06 复核，代码基线 8bc6e0c）

| 项 | 现状 | 目标 |
|---|---|---|
| 标题元信息 | 卡片外标签只有图标和标题；快照里没有版本数或模型信息 | 版本数 > 1 时显示「主版本 i/N」；只有 1 个版本时显示模型名；运行中和文本节点不显示 |
| 默认尺寸 | 后端 `add` 固定 280×180，图片/视频 280×280；前端有三处 280×180 常量 | 文本 300×220，图片/视频 300×300，链接 300×140（CV-B0），音频 300×120（CV-C0） |
| 空态 | 文本只有图标；图片/视频有「上传」「资产库」按钮，没有引导文案 | 文本「双击输入文字」；图片/视频加引导文案，两个按钮保持只填充当前节点 |
| 复制 | 前端发 `dx/dy: 36`；后端和本机预览都不给标题加「副本」 | +40/+40；只有根节点标题加「 副本」 |

文本节点 220 高、组默认尺寸取原型 `sizeFor`（PRD 只写了宽 300）。组的大小由成员决定，打组时另算，不在本条范围。

## 2. 方案

### 2.1 默认尺寸：后端为准，前端同一张表

- 后端 `graph_commands.go` 新增 `defaultNodeSize(typeKey)`，`add` 改用它。有内容、拖入资产时仍走 `fitMediaSize`，按 300 宽适配原始画幅。
- 前端 `outbox.ts` 导出 `defaultNodeSize(typeKey)`，与后端取值相同。`WorkspacePage`、`CanvasView` 的 280×180 常量改用它；本机预览新建的节点也用它，确认前后不跳动。
- 已有节点的尺寸不迁移。

### 2.2 复制：偏移 +40，副本标题由后端写

- 前端三处 `duplicate_selection` 改为 `dx/dy: 40`。偏移量仍由客户端传，契约不变。
- 后端 `duplicate` 给根节点标题加「 副本」，加完超过 200 字时截掉原标题的尾部，保证副本后缀完整。标题为空时只写「副本」。子节点标题不变。
- 本机预览 `structuralPreview` 用同一规则，避免确认后标题跳动。

### 2.3 标题元信息：快照加只读 `version_summary`

OpenAPI `CreativeCanvasNode` 新增可选字段：

```yaml
version_summary:
  type: object
  additionalProperties: false
  required: [count, main_ordinal, main_model_key]
  properties:
    count: { type: integer, minimum: 1 }
    main_ordinal: { type: integer, minimum: 1 }   # 主版本在未删除版本按 version_no 升序中的位置
    main_model_key: { type: string, nullable: true }
```

- 只在节点至少有 1 个未删除版本时出现。没有版本的节点（上传、填充的媒体）不带这个字段，标签不显示元信息。
- 主版本 = `selected_version_id` 指向的版本；为空时取 version_no 最大的版本，与版本条「主版本 · 自动」一致。
- `main_model_key` 从主版本的 `generation_provenance.prompt.model_key` 读；没有时为 null，标签不显示模型名。
- `fillNodeViews` 用一次 `canvas_id` 查询读取本画布的未删除版本，与已有的 `loadVersions` 一样不按节点逐个查询。
- 字段只读，不进 `nodeStateJSON`、撤销链和 read set。新增、删除、选定版本都会提高 data_revision，快照随之刷新。
- `make generate` 同步 Go、TS 生成物和 Agent 工具 schema。字段可选，旧客户端忽略即可。

前端标签逻辑（`nodeLabelMeta`，纯函数）：
- 文本、链接、组和 `generation_state === 'running'` 的节点不显示。
- `count > 1` 显示「主版本 {main_ordinal}/{count}」。
- `count === 1` 且有 `main_model_key` 时显示模型名：在生成目录里找到这个 key 就用对应的 `label`，找不到就直接显示 key。目录只在画布上有带模型 key 的单版本节点时读一次，用这类节点中的第一个；读取失败时直接显示 key。

### 2.4 空态

- 空文本：图标下加「双击输入文字」，保留 `aria-label`。空链接：「双击填写链接地址」。
- 空图片/视频：比例标记下加「选中后输入提示词生成，或拖入{图片/视频}」，文案来自原型。按钮和 `fillEmpty` 不变，仍只填充当前节点。

## 3. 失败与边界

- 版本超过 10000 时 `loadVersions` 已经会报 `ErrLimit`；新增的汇总查询沿用同一上限，超过时不出 summary，不让整张快照失败。
- 快照读不到 provenance 或解析失败时，按没有模型处理。
- 契约只加可选字段，不涉及幂等、权限或撤销的变化。

## 4. 验证计划

- Go：`defaultNodeSize` 表驱动测试；`duplicate` 的副本标题（根节点、子节点、空标题、200 字截断）；`version_summary`（0/1/N 个版本、固定主版本与自动主版本、带 model_key 与不带）。需要数据库的部分沿用 creativecanvas 现有的 storetest。
- 前端单测：`nodeLabelMeta`、`defaultNodeSize`、复制预览的标题规则。
- mock e2e（新增 `creative-node-label.e2e.mjs`）：每条 AC 对应一个场景，并做回退有效性验证。
- 门禁：`make check-go PKG=./internal/creativecanvas/...`、`make check-frontend`、`make generate-check`。
- 现有 `creative-media.e2e.mjs` 断言 280 宽，同步改为 300（需要真实后端，运行情况单独记录）。
