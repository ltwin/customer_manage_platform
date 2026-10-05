---
doc_type: issue-fix
issue: 2026-10-05-composer-baseline-regression
status: confirmed
path: fast-track
fix_date: 2026-10-05
requirement: V6-056
tags:
  - frontend
  - creative-canvas
  - node-generation
  - baseline-regression
---

# 节点生成输入框基线回归验收与修复记录

## 1. 范围

[V6-056](../../requirements/creative-workspace-v6/items/V6-056.md) 是验证项，覆盖 PRD GN-01/02/03/04/05/34。方法是先用浏览器回归逐条验收 6 条 AC：已经符合的直接记录证据；有缺口的，在本条范围内的就地修复，PRD 与基础需求冲突的交给 owner 拍板。

## 2. 首轮验收与处置

新增 `frontend/scripts/creative-composer-baseline.e2e.mjs`：全量 mock API，带一个可用模型，每条 AC 在新的 browser context 里运行。

| AC | 首轮 | 处置 |
|---|---|---|
| AC-01 出现条件与位置 | 失败 | 两处真实缺口已修复：多选时仍显示输入框；间距 12px，PRD 要求 14px。选中组时显示输入框与 GN-01 冲突，owner 拍板按基础需求 CAN-09 保留（PRD §11.3 #8） |
| AC-02 宽度、跟随、贴边 | 失败 | 左侧最少留 64px 避让工具轨，与 GN-02「左右各留 8px」不一致，owner 拍板保留避让（§11.3 #9）。跟随平移/缩放的 14px 间距随 AC-01 的修复一起修好。右侧留 8px、超出底部上移贴底本来就符合，首轮失败是测试平移方向写反了 |
| AC-03 结构与底栏 | 失败 | 底栏缺比例/数量 chip 和点数，这两项分别属于 V6-021、V6-022 的交付。owner 拍板 AC-03 只验已有部分，已在两条里登记交接 |
| AC-04 点击与按键隔离 | 通过 | — |
| AC-05 草稿保存 | 失败 | 未保存的本机输入按窗口隔离，是 CAN-07 多窗口约束下的现有设计，新开窗口看不到。owner 拍板「重开后仍在」指已保存的草稿（§11.3 #10），不改代码 |
| AC-06 生成键 | 通过 | — |

另外，D-04 由 owner 拍板：文本节点同样出现输入框，作为文字生成入口（§11.3 #7），已回写 PRD GN-01。

## 3. 代码修复

- `WorkspacePage.tsx`：生成输入框只在 `selection.length <= 1` 时渲染。原来只按焦点节点判断，多选时仍会显示焦点节点的输入框，容易让人误以为生成只针对其中一个。
- `NodeGenerationPanel.tsx`：输入框顶部偏移从 12px 改为 14px，与 PRD GN-01 及原型 `composer.mjs` 一致。

后端、OpenAPI、草稿日志机制都没有改。

## 4. 需求文档回写

- PRD `v6-workspace-prd.md`：更新 GN-01（加入文本节点、组显示范围任务描述）、GN-02（左边界避让工具轨与抽屉）、GN-05（重开的口径），§11.3 新增 #7–#10。
- README 待拍板表：D-04 标为已拍板。
- V6-024：文字生成入口按 D-04 确定，不再另设入口。
- V6-021、V6-022：登记底栏比例/数量 chip 与点数的交接。

## 5. 验证结果

- `npx tsc -b --noEmit`、`npm run build`：通过。改动文件没有新的 lint 告警。
- `make test-frontend`：391/391 通过。
- `node scripts/creative-composer-baseline.e2e.mjs`：连续两次 6/6 通过。
- 回归 `creative-generation`、`creative-canvas-baseline`、`creative-canvas-interaction`、`creative-library` 四个 e2e，全部通过。
- 回归有效性：把 `WorkspacePage.tsx`、`NodeGenerationPanel.tsx` 换回修复前的版本，AC-01 在多选处失败，AC-02 在 14px 间距处失败，其余 AC 通过。

未验证：

- 生成只用 mock 执行接口验证到「受理后按钮变为停止」，真实生成链路由 V6-023 负责。
- 拖动中隐藏输入框由已有的 `creative-generation` e2e 覆盖，本条没有重复写断言。
- 没有做视觉走查。

## 6. 验收对应

| AC | 证据 |
|---|---|
| AC-01 | composer e2e AC-01：单选图片、文本节点时出现，距节点 14px；多选、选中连线、编辑文字时不出现；选中组时显示组任务描述；切换选中后显示新节点的提示词 |
| AC-02 | composer e2e AC-02：宽 460；平移、缩小后仍在节点正下方居中、间距 14px；左侧不压住工具轨；右侧留 8px；接近底部时上移，保持在视口内 |
| AC-03 | composer e2e AC-03：参考行 → 提示词 → 底栏；底栏以「生成模型」开头、圆形「生成新版本」结尾。比例/数量 chip、点数由 V6-021/022 验收 |
| AC-04 | composer e2e AC-04：点击输入框不取消选中；在输入框里按 t、i、Delete、Backspace、⌘A、⌘D、⇧1 都不触发画布操作；点击底栏控件不取消选中 |
| AC-05 | composer e2e AC-05：两个节点的草稿互不干扰，切换后仍在；保存草稿后当前作品不变；刷新后两个节点的草稿都在；新窗口重开后已保存的草稿仍在 |
| AC-06 | composer e2e AC-06：提示词为空且没有上游时禁用；有上游参考时可用；Shift+Enter 只换行；输入法组字中按 Enter 不生成；Enter 生成一次，之后按钮变为「停止」 |
