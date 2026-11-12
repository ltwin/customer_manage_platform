# 场景助手入参设计

写工具继续验证账号和准入，在加锁场景上补齐新增物件的字段。显式 ID 和顺序继续可用，省略 ID 用现有生成器，省略顺序按父级追加并避开同批显式顺序。新增 ID 按提交顺序返回。姿态沿用对象层目标点，允许局部 Y 轴角度。普通局部更新保留未给出的字段，分镜只将实际指定的字段转成关键帧。最终通过原有完整场景命令校验，沿用按单元冲突检查和幂等回执。

建模工具公开的说明和 schema 明确三种来源。校验依旧在创建任务前进行，重试复用原有烘焙产物。场景修订仍由助手依据读取或冲突结果传回。接口是兼容扩展，见 contracts.md。

## 挂载点

- `backend/internal/creativeagent/scene_tools.go` 与 `backend/internal/creativeagent/scene_tool_schema.go` 更新工具定义与拒绝提示。
- `backend/internal/creativescene/agent.go`、`backend/internal/creativescene/agent_objects.go`、`backend/internal/creativescene/lookat.go`、`backend/internal/creativescene/jobs.go` 实现补齐和校验。
- `backend/internal/creativeagent/scene_tools_test.go` 与 `backend/internal/creativescene/agent_tools_test.go` 验证真实工具和分镜语义。

## 验证与交付

用工具集成测试回归报错，并验证新增 ID、并发和幂等。用场景集成测试锁定局部更新的分镜语义，跑受影响 Go 包门禁。
