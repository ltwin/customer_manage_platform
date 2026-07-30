---
doc_type: decision
category: convention
date: 2026-07-07
slug: frontend-toolchain-first-standard
status: active
area: frontend
tags: [react, typescript, eslint, prettier, style-guide, code-review, checklist]
---

# 前端编码规范采用「工具链优先 + 薄人工清单」

## 背景

前端为 React + TypeScript（技术栈 2026-07-05 拍板），契约经 openapi-typescript 生成 `schema.d.ts` 与后端对齐。后端已于 2026-07-06 拍板 Uber Go Style Guide + `docs/go-style-checklist.md` 的两层规范结构；前端若无对称口径，AI 协作写码时风格与模式会漂移，review 无检查依据。

与 Go 的关键差异：前端没有 Uber Guide 那样的社区权威全维度规范（Airbnb Style Guide 已停止演进），社区事实标准就是工具链自身的 recommended 规则集，且前端可机器检查的比例远高于 Go。因此规范重心从「规范源」移到「工具链」，人工 checklist 相应做薄。

## 决定

- **工具链为规范主体**（机器可查项一律交给工具，不进人工清单）：
  - TypeScript 编译器 `strict: true`；
  - ESLint（flat config）：`typescript-eslint` strict 预设 + `eslint-plugin-react-hooks`；
  - Prettier 负责格式化，与 lint 职责分离。
- **人工 review 口径**为配套薄清单 `docs/frontend-style-checklist.md`，只列工具查不到的项（类型对齐、状态设计、组件边界、错误封套、硬规则叠加）。
- 具体配置文件（`tsconfig.json` / `eslint.config.js` / Prettier 配置）在 **platform-skeleton feature 落地**（roadmap 条目 1 已含"build/test/lint 命令基线"），本决策只锁工具口径与 checklist，不预支配置。
- 数据获取库、组件库、样式方案属技术选型而非编码规范，**本决策不涉及**，留待 platform-skeleton 或首个前端 feature 的 design 阶段按场景拍板。

## 理由

- `strict: true` + typescript-eslint strict + react-hooks 规则是前端"性价比最高"的规范组合：hooks 依赖数组规则是 React 项目唯一"不开会出事故"的 lint，TS 严格模式相当于半部 style guide。
- ESLint + Prettier 是 AI 写码时模型最熟悉的口径，单人 + AI 协作场景下"无聊的默认"漂移风险最低（Biome 单工具方案作为已知替代，未采纳）。
- 与 Go 决策结构对称（工具兜底 + 人工薄清单 + 配置延后落地），review 时两端心智模型一致，符合本项目"不预支复杂度"的一贯口径。

## 考虑过的替代方案

- **Biome（单工具替代 ESLint + Prettier）**：性能好、配置少，但 AI 协作生态与插件成熟度不及 ESLint 事实标准，未采纳；若后续工具链维护成本成为真实痛点，可 update 本决策。
- **采纳某份社区 style guide 文档（如 Airbnb）**：已停止演进、与 flat config 时代脱节，不采纳。

## 后果

- 所有前端代码在工具链三件套通过后才进人工 review；review 按 `docs/frontend-style-checklist.md` 逐项检查。
- 检查口径分层与后端一致：CLAUDE.md 硬规则（术语 / 凭证红线）优先级最高，checklist 是模式与惯用法层，冲突时硬规则赢。
- platform-skeleton feature 的范围需包含前端工具链配置落地（tsconfig / eslint / prettier + lint 命令接入基线）。
- 某条口径与实际场景冲突时，回本决策 update / supersede，不得在 feature 里私自绕开。

## 相关文档

- `docs/frontend-style-checklist.md` —— 配套 review checklist
- `.codestable/compound/2026-07-06-decision-go-uber-style-guide.md` —— 后端对称决策
- `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` §5 条目 1 —— platform-skeleton（lint 基线归属）
- `CLAUDE.md` —— 硬规则与文档索引
