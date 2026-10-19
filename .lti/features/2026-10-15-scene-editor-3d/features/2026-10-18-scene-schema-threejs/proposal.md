---
risk:
  factors:
    - data-migration
    - contract-break
    - trust-boundary
behavior: changed
reads:
  - creative/scene
decisionsPending: false
---
# 本次交付

## Why

Epic `scene-editor-3d` 修订 v7（owner 2026-10-18 批准）决定：场景格式里助手要写的字段与 three.js 同名同义。本子项 C3a 落地这次修订。

C3 试用中，助手按 three.js 的习惯给日式小巷加灯光和雾，全部被拒：

- 点光写成 `{type, color, intensity: 45, decay: 2, distance: 7}`，被要求改成 `range_m`、`angle_deg`，并补全 7 个字段；
- 雾写成 `{color, near, far}`，被要求改成 `near_m`、`far_m`。

格式大体像 three.js 时，助手会按像的部分去补其余字段；含义不同的字段（整锥角与半角、度与弧度）不会报错，只会悄悄出错。C4 摆摄像机、C5 渲染灯光之前，先把这些字段定好。

## What Changes

按修订 v7 的 `data: 场景文档`、`api: 提交场景命令`、`api: 读取运行的场景改动`（`epic bind --deliver`）实现：

- **字段改名**：雾和摄像机的 `near_m`/`far_m` 改为 `near`/`far`；灯光的 `range_m` 改为 `distance`（0 表示不截断），`angle_deg` 改为 `angle`（弧度，半角），新增 `decay`。
- **字段可省略**：灯光只有 `type` 必填，摄像机全部可省略；省略的字段取 three.js 默认值，存储时补齐。写了该类型灯光没有的字段返回 400 并指出字段。
- **局部更新**：`update_objects` 里的 `light`、`camera` 由整体替换改为按键合并；灯光类型改变时，去掉新类型没有的字段，缺少的补默认值。服务端和编辑器的本地应用保持一致。
- **迁移**：一次迁移改写已有的场景快照（开发库 13 个修订带摄像机或雾，没有灯光；生产库没有场景数据），提供反向迁移。
- **工具说明**：`edit_scene` 写明灯光、雾、摄像机的新字段与默认值，灯光和摄像机朝局部 −Z，太阳方位 0° 朝 +Z、90° 朝 +X，以及按键合并；继续注明灯光物件暂不渲染。
- **契约补记**：「读取运行的场景改动」按 C3 的实现补记 3 处（`hierarchy_not_restorable`、5000 条历史上限、同一物件多来源修订记为别人改过），代码不变。

不做的事：

- 不渲染灯光（C5）。
- 不加 `look_at`（C4）。
- 不改 `rotation`、天空和焦距相关的字段名。
- 不接受旧字段名作为别名。

## Acceptance Criteria

- **AC-1**：**WHEN** 助手写入点光 `{type: "point", color: "#ff963d", intensity: 45, distance: 7, decay: 2}`，**THEN** 写入成功，读回时另有 `cast_shadow: false`，没有 `angle`、`penumbra`。
- **AC-2**：**WHEN** 只写 `{type: "spot"}`，**THEN** 存为 `color #ffffff、intensity 1、distance 0、decay 2、angle π/3、penumbra 0、cast_shadow false`。**WHEN** 新增摄像机时省略字段，**THEN** 存为 `{35, [36,24], "3:2", near 0.1, far 1000, ""}`。
- **AC-3**：**WHEN** 写了旧字段名（如 `range_m`、`near_m`），或该类型没有的字段（如点光写了 `angle`），**THEN** 命令返回 400，助手工具结果指出字段，场景不变。
- **AC-4**：**WHEN** `update_objects` 只给灯光 `intensity`，**THEN** 其余字段不变。**WHEN** 把聚光改成点光，**THEN** `angle`、`penumbra` 被去掉，颜色、强度、`distance`、`decay` 保留。编辑器本地应用与服务端结果一致，摄像机同理。
- **AC-5**：**WHEN** 雾用 `{color, near, far}` 写入，**THEN** 编辑视图和截图里雾照常生效。**WHEN** 摄像机用 `near`/`far` 写入，**THEN** 按摄像机取景的截图使用它们。
- **AC-6**：**WHEN** 迁移执行后，**THEN** 原有带摄像机或雾的场景照常打开，历史版本能预览和恢复，摄像机与雾的数值不变。**WHEN** 反向迁移，**THEN** 字段名恢复为旧名。
- **AC-7**：**WHEN** 助手读工具说明，**THEN** 能看到灯光三种类型各自的字段和默认值、朝局部 −Z、天空方位约定，以及局部更新只改写到的字段。
- **AC-8**：**WHEN** 归档，**THEN** 项目契约「读取运行的场景改动」与 C3 的实现一致。

## Impact

- **后端**：
  - `backend/internal/creativescene`：模型结构、输入解码、默认值补齐、`update_objects` 合并、校验；
  - `backend/internal/creativeagent/scene_tools.go`：工具说明；
  - `backend/internal/platform/store/migrations`：新增 0076。
- **契约**：`api/openapi.yaml` 的 `CreativeSceneFog`、`CreativeSceneLight`、`CreativeSceneCamera` 和更新操作，以及两份生成物。
- **前端**：
  - `frontend/src/creative-scene`：雾、截图取景、本地应用 `update_objects`；
  - 测试夹具。
- **契约状态**：破坏性修改（字段改名）。调用方只有编辑器和助手工具，与本次同一版本更新。上线前，没有外部调用方。

## Risk Notes

- **数据迁移**：改写已有快照，破坏「内容修订不可变」一次，owner 2026-10-18 同意（修订 v7）。反向迁移会丢掉 `decay`；有非默认 `decay` 的灯光时拒绝回滚，前向修复。
- **信任边界**：服务端校验仍是唯一关口。新增的「按类型拒绝多余字段」和默认值补齐都在服务端完成，前端不替服务端补值。
- **契约破坏**：输入改名后，旧名返回 400。迁移和新代码同一次部署，迁移先于服务启动执行。
