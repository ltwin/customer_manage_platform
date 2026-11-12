---
risk:
  factors: [trust-boundary, concurrency]
behavior: changed
reads: [creative/scene, creative/documents]
decisionsPending: false
---
# 降低场景助手的工具参数拒绝率

## Why

现有工具把完整物件结构、唯一序号和旋转计算交给助手生成。普通搭建或移动常因机械字段被整批拒绝。

## What Changes

- 新增物件可省略内部 ID、顺序、根级父级、可见性和锁定标记，服务端补齐并返回实际 ID。
- 支持局部变换，简化旋转输入，明确目标点和旋转的互斥关系。
- 明确模型代码、沿用模型修订和冲突重试的参数分支，修正场景基线的工具定义。

## Acceptance Criteria

- WHEN 批量新增省略机械字段 THEN 写入完整物件，按父级顺序追加，并返回可用于后续调用的 ID。
- WHEN 只移动物件或指定分镜的位置 THEN 保留其余变换和关键帧。WHEN 误放或重复指定朝向 THEN 返回能定位和修正的拒绝。
- WHEN 建模来源缺失或冲突 THEN 在建任务前说明需提供的参数。合法旧请求、冲突保护和回执重放继续通过。

## Impact

变更内置场景助手的工具输入与结果。场景 HTTP 命令、持久化格式和烘焙协议沿用既有契约。

## Risk Notes

助手输入在账号与授权范围内规整。顺序和局部变换依赖加锁场景，仍按助手读过的修订检查并发冲突。
