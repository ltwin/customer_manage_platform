---
doc_type: feature-verification
feature: 2026-10-06-node-label-meta-defaults
requirement: V6-003
status: passed
date: 2026-10-06
---

# V6-003 验证记录

设计：[node-label-meta-defaults-design.md](node-label-meta-defaults-design.md)。

## 实现偏差

- 元信息放在标题后面（间隔 4px），没有像原型那样整行靠右。原因：画布基线 CV-20 要求「短标题旁边的空白点到画布、不选中节点」，`creative-canvas-interaction` e2e 有对应断言。改成整行宽度会让这块空白变成选中区域，所以标签保持内容宽度。标签高度按 PRD 改为 26px。
- 空链接节点的提示「双击填写链接地址」是本条补的，PRD CV-12 只写了文本和图片/视频。

## 门禁

- `make check-go PKG=./internal/creativecanvas/...`：build、golangci-lint 0 issues、测试通过。
- 依赖包 `creativegeneration`、`creativeagent`、`creativemedia`、`creativelibrary`、`platform/httpapi` 测试通过。
- `make generate-check`：退出码 0（OpenAPI、Go/TS 生成物、Agent 工具 schema 已同步）。
- `npm run lint` 退出码 0；`npm run build` 通过；`make test-frontend` 399/399。
- mock e2e：新增 `creative-node-label` AC-01–04 全部通过。`creative-save-status`、`creative-canvas-interaction`、`creative-canvas-baseline`、`creative-composer-baseline`、`creative-library`、`creative-library-baseline`、`creative-generation` 也都通过。
- 回归有效性：5 个前端源文件换回 HEAD 后，`creative-node-label` 的 AC-01–04 全部失败；恢复后通过。

修改的既有断言：
- `creative-save-queue.test.ts`：默认宽改为 300，2:3 图片高 450，音频默认高 120。
- `creative-library.e2e.mjs`：批量放入的间距 320 → 340（宽 300 + 间距 40）。
- `creative-media.e2e.mjs`：默认宽 300、64×48 图片高 225、音频 300×120。这个测试需要真实后端，本次没有运行。

## 验收对应

| AC | 证据 |
|---|---|
| AC-01 主版本 i/N 或单版本模型；运行中与文本隐藏 | Go `TestNodeVersionSummary`（实库：无版本时不出 summary；1 版、2 版时最新为主；固定第 1 版后 `main_ordinal=1`；序列化形态）。`TestSummarizeVersions`（version_no 有空洞、固定、非生成版本没有模型、选定的版本已不存在时回落到最新、provenance 解析失败）。前端单测 `nodeLabelMeta`。e2e AC-01：「主版本 2/3」、目录 label「Seedream 4」、未知模型显示 key、无版本/运行中/文本/组不显示，标签高 26px、位于卡片外 |
| AC-02 默认宽 300、图/视频 1:1；空文本有提示 | Go `TestNodeDefaultSizeAndCopyTitle`（实库：文本 300×220、图片/视频 300×300、链接 300×140、音频 300×120）。e2e AC-02：从添加菜单新建的文字、图片、视频、音频的预览尺寸与此一致；空文本显示「双击输入文字」 |
| AC-03 前后端尺寸一致；复制 +40/+40，只有根标题加副本 | 前后端尺寸取值相同，分别有单测（Go `TestDefaultNodeSizeAndCopyTitle`、前端 `default sizes match the server table`）。Go 实库：根节点「晨光 副本」、空标题「副本」、组「参考组 副本」、组内子节点标题不变、200 字截断后保留完整后缀，偏移 +40。e2e AC-03：⌘D 预览偏移 +40/+40，标题为「multi 副本」；复制组后只有组标题加副本；恢复网络后发出的 `duplicate_selection` 是 `dx/dy=40` |
| AC-04 空图片/视频引导文案 + 上传/资产库，只填充当前节点 | e2e AC-04：显示「选中后输入提示词生成 / 或拖入图片」；「资产库」打开「选择图片」填充模式；「上传」打开文件选择器，不新建节点。只填充当前节点由既有 `fillEmpty` 保证，V6-057 AC-06 已经验证过填充模式只写目标节点 |

## 未验证

- `creative-media`、`creative-text-canvas`、`creative-canvas-commands` 需要真实后端和账号，本次没有运行。
- 真实生成链路下的模型名：当前没有可用的图片供应商（V6-023），只用 mock 目录和 Go 的 provenance 单测验证。
- 视觉走查：截图存在 `/tmp/v6-003-labels.png`，本次环境打不开图片，没有人看过，交给 owner。
