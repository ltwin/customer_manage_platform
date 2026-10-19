# Tasks

## 1. 服务端格式

- [ ] 1.1 `scene_test.go` 先补 AC-1–AC-3 用例（按类型补齐、只写 `type`、摄像机缺省、旧名、多余字段、`null`），再改 `Fog`、`Light`、`Camera`、`lightPatch`，新增 `completeLight`、`completeCamera`，接入 `addObjectsOp.apply`，更新 `validateEnvironment`、`validateLight`、`validateCamera` 与旧夹具；`make check-go PKG=./internal/creativescene/...` 通过
- [ ] 1.2 `scene_test.go` 先补 AC-4 决策表 5 行与摄像机局部更新，再实现 `mergeLight`、`mergeCamera` 并接入 `updateObjectsOp.apply`，`objectUpdate` 注释改为按键合并；同一检查通过
- [ ] 1.3 `api/openapi.yaml` 改 `CreativeSceneFog`、`CreativeSceneLight`、`CreativeSceneCamera` 与更新条目，`make generate` 并提交两份生成物；`creative_documents_test.go` 夹具改名，并补一例旧名返回 400；`make check-go PKG=./internal/platform/httpapi/...` 通过

## 2. 迁移

- [ ] 2.1 新增 `0076_creative_scene_threejs_fields` 的 up/down 与 `creative_scene_threejs_migration_test.go`：旧格式快照（雾、摄像机、三种灯光）up 后字段和数值正确，down 后还原，`decay ≠ 2` 时 down 被拒；`rollbackSceneJobs` 先回滚 0076；`make check-go PKG=./internal/platform/store/...` 通过

## 3. 助手工具

- [ ] 3.1 改写 `edit_scene` 说明中灯光、摄像机、雾的部分（design 给出的文字），`scene_tools_test` 断言新字段在、旧名不在；`agent_tools_test.go` 夹具改名；`make check-go PKG=./internal/creativeagent/...` 与 `creativescene` 通过

## 4. 编辑器

- [ ] 4.1 `creative-scene.test.ts` 先补本地合并用例（同 AC-4 决策表），再改 `state/apply.ts:updateObject`；`Viewport.tsx` 雾、`host/framing.ts` 摄像机改用 `near`/`far`；其余单测与 e2e 夹具改名；`make check-frontend` 通过

## 5. 合并前

- [ ] 5.1 先合 develop，再跑一次 `make check`；结果和例外记进本文件
