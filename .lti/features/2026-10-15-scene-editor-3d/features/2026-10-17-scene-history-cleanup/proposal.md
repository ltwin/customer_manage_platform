---
risk:
  factors: [irreversible, data-migration, perf-sensitive]
behavior: changed
reads: []
decisionsPending: true
---
# 本次交付（登记稿，方案待探讨）

> owner 2026-10-17 同意把历史清理记为 epic 后续子项，具体怎么做到做的时候再探讨。本稿只记录背景和待定问题，还不是待批准的方案。

## Why

epic design「写入频率」把历史清理列为后续工作。C1 `scene-document-shell` 实测（见其 verification「修订增长的检查与优化」）：

- 每次保存都存一份完整快照：一行内容修订，加上文档修订、引用和单元版本，只增不减。
- 本地 PG17 压缩后每个物件每个修订约 90 B：100 个物件的场景每次保存约 9.6 KB，1000 次操作约 10 MB；300 个物件每次约 27 KB。
- C1 已在前端丢弃没有变化的修改、按住快捷键只执行一次，减少了无效修订，但存储仍随操作次数线性增长。

## What Changes（待探讨）

- 清理哪些修订、保留多久，由 owner 决定。
- 讨论时可参考的思路：近期修订全部保留；更早的按历史面板的 2 分钟分组只留每组最后一版；恢复点和新建保留。
- 先理清和以下机制的关系：撤销栈、历史版本列表与预览、助手的本轮改动清单与「恢复这一处」、单元版本引用。仍会被它们用到的修订不能删。
- 清理是场景专属，还是放在通用文档底座（`creative-document-foundation`）里？策划文档也用同一套修订表。如果放进底座，这一项可能要从本 epic 移出，改成独立 change。

## Acceptance Criteria

方案确定后补充。

## Impact

可能涉及 `backend/internal/creativedocument`、`backend/internal/creativecontent`、数据库迁移或后台定时清理，以及前端历史版本面板。

## Risk Notes

- 删除修订不可逆，需要先定义「仍被引用」的判定，并考虑删除前是否保留备份或延迟删除。
- 大批量删除可能长时间锁表，需要分批执行。
- epic roadmap 的子项表在下次修订 epic 时补上本项。
