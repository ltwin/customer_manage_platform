---
slug: epic-delivered-contract-modify
status: observed
scope: Epic 子项：修改前序子项已交付的 epic 契约
createdAt: 2026-10-17T14:01:37.467Z
source: 9f8bcaa9-5b8e-4314-b5b4-7576bbc0ab61
---
epic contracts.md 里的条目归 epic 所有。前序子项交付后，若要改它：不能在 epic 里改 ADD 正文（epic revise 报 epicDesignInvalid: ADD conflicts with current project truth），也不能写在子项局部 contracts.md（apply 报 epicDesignOverride）。做法：epic 里把该节标题改成 MODIFY，正文以当前项目契约（.lti/contracts/）为底改写，epic revise 后由本子项 epic bind --deliver 交付。改动 proposal/design/contracts 后原批准失效，需重新 approve。来源：C2 scene-procedural-geometry 修改 C1 已交付的 api: 读取场景、data: 场景文档。
