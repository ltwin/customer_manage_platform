# Tasks

## 1. 服务端格式

- [x] 1.1 `scene_test.go` 先补 AC-1–AC-3 用例（按类型补齐、只写 `type`、摄像机缺省、旧名、多余字段、`null`），再改 `Fog`、`Light`、`Camera`、`lightPatch`，新增 `completeLight`、`completeCamera`，接入 `addObjectsOp.apply`，更新 `validateEnvironment`、`validateLight`、`validateCamera` 与旧夹具；`make check-go PKG=./internal/creativescene/...` 通过
- [x] 1.2 `scene_test.go` 先补 AC-4 决策表 5 行与摄像机局部更新，再实现 `mergeLight`、`mergeCamera` 并接入 `updateObjectsOp.apply`，`objectUpdate` 注释改为按键合并；同一检查通过
  - 挂载点外：`model_test.go` 的 `validScene` 夹具与 `TestValidate` 用例随类型改动（新增 L1 两例、摄像机缺字段一例）。
- [x] 1.3 `api/openapi.yaml` 改 `CreativeSceneFog`、`CreativeSceneLight`、`CreativeSceneCamera` 与更新条目，`make generate` 并提交两份生成物；`creative_documents_test.go` 夹具改名，并补一例旧名返回 400；`make check-go PKG=./internal/platform/httpapi/...` 通过
  - 验证：新增 `CreativeSceneLightPatch` 供更新条目使用（OpenAPI 3.0 不能取消 required）；`httpapi` 补旧名 400 与 three.js 点光 200 各一例，`check-go PKG=./internal/platform/httpapi/...` 通过。

## 2. 迁移

- [x] 2.1 新增 `0076_creative_scene_threejs_fields` 的 up/down 与 `creative_scene_threejs_migration_test.go`：旧格式快照（雾、摄像机、三种灯光）up 后字段和数值正确，down 后还原，`decay ≠ 2` 时 down 被拒；`rollbackSceneJobs` 先回滚 0076；`make check-go PKG=./internal/platform/store/...` 通过
  - 验证：迁移测试覆盖雾、摄像机、三种灯光、空场景和非场景修订（不被改写），up/down 往返还原，`decay = 3` 时 down 被拒；固定步数的回滚清单各加 0076（同 0075 的做法）。`check-go PKG=./internal/platform/store/...` 通过。
  - 真实数据（只读）：把 up 的改写在开发库上以只读事务的 SELECT 执行，70 个场景修订换算后全部通过新代码的严格解码与 `Validate`（13 个带雾、13 台摄像机、没有灯光），没有残留旧字段名。

## 3. 助手工具

- [x] 3.1 改写 `edit_scene` 说明中灯光、摄像机、雾的部分（design 给出的文字），`scene_tools_test` 断言新字段在、旧名不在；`agent_tools_test.go` 夹具改名；`make check-go PKG=./internal/creativeagent/...` 与 `creativescene` 通过
  - 验证：新增 `TestEditSceneDescribesThreejsFields`（新字段、默认值、局部 −Z、方位约定、按键合并在，旧名与「整体替换」不在）；`TestABadSceneCallSaysWhatToFix` 改用旧名 `range_m`，工具结果指出字段（AC-3）。`agent_tools_test.go` 的夹具在 `creativescene` 包，已随 1.1 改名。`check-go` 两个包均通过。

## 4. 编辑器

- [x] 4.1 `creative-scene.test.ts` 先补本地合并用例（同 AC-4 决策表），再改 `state/apply.ts:updateObject`；`Viewport.tsx` 雾、`host/framing.ts` 摄像机改用 `near`/`far`；其余单测与 e2e 夹具改名；`make check-frontend` 通过
  - 验证：`check-frontend` 通过；本地合并用例覆盖决策表与摄像机；`creative-scene.e2e.mjs` 35 项通过，其中新增雾一项（把字段改回 `near_m`/`far_m` 时画面差异为 0，被抓到）；`creative-scene-agent.e2e.mjs` 9/9（含按摄像机取景用 `near`/`far`）。
  - 挂载点外：`objects/SceneObjects.tsx` 的 `CameraFrustum` 调用处按字段兜底，原因同 `framing.ts`（摄像机字段在生成类型里变为可选）。

## 5. 合并前

- [ ] 5.1 先合 develop，再跑一次 `make check`；结果和例外记进本文件
