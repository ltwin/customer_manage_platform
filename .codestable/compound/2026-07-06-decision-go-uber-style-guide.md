---
doc_type: decision
category: convention
date: 2026-07-06
slug: go-uber-style-guide
status: active
area: backend
tags: [go, style-guide, code-review, checklist, uber]
---

# Go 后端编码规范采用 Uber Go Style Guide

## 背景

后端为 Go + Gin（ADR-003），截至 2026-07-06 规划层已完成、业务代码尚未开工。单人开发 + AI 协作写码，若不在第一行业务代码之前统一编码规范，风格会靠临场发挥漂移，code review 也没有统一检查口径。

## 决定

- Go 后端统一采用 **Uber Go Style Guide** 作为项目编码规范。
- 中文版以 <https://github.com/xxjwxc/uber_go_guide_cn> 为日常阅读参考；条目语义有歧义时以英文原版 <https://github.com/uber-go/guide> 为准。
- 设立配套 **code review checklist**（`docs/go-style-checklist.md`），作为人工 review 的检查口径；工具可自动查的项（格式化、vet、lint）交给 gofmt / goimports / golangci-lint，不进人工清单。
- golangci-lint 的具体配置（`.golangci.yml`）在后端工程初始化 feature 时落地，本决策只锁定规范源与 checklist，不预支工具配置。

## 理由

- Uber Go Style Guide 是 Go 社区广泛采用的事实标准，覆盖错误处理、并发、性能、风格、模式全维度，且持续维护。
- 有维护良好的中文译本（owner 指定的参考仓库），降低阅读与执行成本。
- 采纳成熟规范而非自造一套，单人项目的规范制定成本趋近于零，符合本项目"不预支复杂度"的一贯口径。

## 考虑过的替代方案

未做系统对比评估（如 Google Go Style Guide、仅 Effective Go + Go Code Review Comments 的轻量组合）。拍板依据是 Uber 规范的社区采用度与中文译本可得性，由 owner 直接指定。

## 后果

- 所有 Go 后端代码按该规范编写；feature 实现后的 code review 阶段按 `docs/go-style-checklist.md` 逐项检查。
- 检查口径分层：CLAUDE.md 硬规则（账号隔离 / 薄 handler / 凭证红线）优先级最高，checklist 是风格与惯用法层，两层都查、冲突时硬规则赢。
- 后端工程初始化 feature 的范围需包含 golangci-lint 配置落地，把规范中机器可查项交给工具。
- 某条规范条目与实际场景冲突时，回本决策 update / supersede，不得在 feature 里私自绕开。

## 相关文档

- `docs/go-style-checklist.md` —— 配套 review checklist
- `.codestable/requirements/adrs/003-monolith-first-gin-openapi.md` —— Gin + 轻量 DDD 分层约束
- `CLAUDE.md` —— 硬规则与文档索引
- <https://github.com/xxjwxc/uber_go_guide_cn>（中文参考）/ <https://github.com/uber-go/guide>（英文原版）
