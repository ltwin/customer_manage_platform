# Tasks

<!-- 17 项，超过 15 的提示线：C1 的范围已在 epic 交付计划中定档（C1–C6），不再拆分。组 1–3 是后端，组 4–6 是前端，4.1 穿刺需要 owner 在真机上操作，只阻塞 6.2。 -->

## 1. 底座放宽与内容类型

- [x] 1.1 底座 `restore_units` 上限 200 → 4096：`creativedocument/document_test.go` 先加边界用例（4096 个单元恢复成功，4097 返回 400 `validation_failed`）确认失败，再改 `command.go:maxRestoreUnits`；`make check-go PKG=./internal/creativedocument/...` 通过
- [x] 1.2 内容类型 `scene`：`creativecontent.Payload` 加 `Scene json.RawMessage` 和 `Validate` 的 `scene` 分支；迁移 `0073_creative_scene_document` up/down（内容类型 CHECK 加 `scene`，存在 `scene` 内容时拒绝回滚）；`creative_scene_document_migration_test.go` 新增回滚链头 `rollbackSceneDocument`，`rollbackDocumentFoundation` 先调用它，各迁移测试的回滚标签列表最前面加 `creative-scene-document`；迁移与内容测试通过

## 2. 场景文档类型（后端）

- [x] 2.1 模型与内置清单：`creativescene/model.go`（`Scene` / `Object` 类型、拒绝未知字段的解码与编码、`Validate` 校验 S1–S8）、`builtins.json` + `builtins.go`（7 个基础体、参数类型与范围校验、缺省补默认值）；表驱动测试先写，覆盖 S1–S8 每条及每个基础体的参数越界、未知键、未知 builtin
- [x] 2.2 场景操作：`ops.go` 实现 `add_objects` / `update_objects` / `remove_objects` / `reparent_objects`（各 1–2000 项）、`set_environment`、`set_render` 的解码、`Touched`（每单元一条）和 `Apply`；字段语义按 R5（`primitive.params` 按键合并，`light` / `camera` / `material_slots` 整体替换），物件不存在返回 400（R6），同一物件在一个操作里出现两次返回 400；测试覆盖每种操作的合法与非法输入（含灯光、摄像机、分组、材质槽），非法时不产生修订，一个 `update_objects` 更新 2000 个物件产生一个修订
- [x] 2.3 恢复、读取与注册：`restore.go`（`RestoreTouched` / `RestoreUnits`：最终集合、按源快照祖先链找父、顺序规则、删除分组时后代上移、结果与 `units` 顺序无关、不得单独列 `objects`）、`view.go`（`View` 按单元裁剪、`Summary`）、`type.go`（`sceneType`，`init()` 注册），`cmd/server` 与 `cmd/creative-worker` 空导入；测试覆盖 AC-1（`add_node core.scene` 同事务产生文档、修订 1、只有地面）和 AC-4 后端各项（删除后按原 ID 与位置加回；150 个物件的嵌套分组一次撤销成功，制造一个单元冲突时整体失败且快照不变；G ⊃ H ⊃ A 复原、打乱 `units` 顺序结果相同；只恢复 A 时挂到最近祖先或根）

## 3. 契约、生成物与集成测试（后端）

- [x] 3.1 OpenAPI 与生成：`api/openapi.yaml` 加 `CreativeSceneDocument`（并入 `CreativeDocument` 的 `oneOf`）、`CreativeScene`、`CreativeSceneObject` 等 schema 和按 `op` 判别的 `CreativeSceneOperation`，画布 `add_node` 的 `type_key` 枚举加 `core.scene`；`Makefile` 的 `generate` / `generate-check` 复制并比对 `frontend/src/creative-scene/builtins.gen.json`；运行 `make generate`，`make generate-check` 通过
- [x] 3.2 HTTP、并发与基线：`httpapi/creative_documents_test.go` 加场景读取、命令、409 冲突明细、跨账号 404、回收站 409（AC-9）；屏障并发测试每组 20 轮（摆放与内容并发两边都成功；同一单元并发恰好一方成功、明细指向 `object:<id>`）（AC-5）；基线测试 `-run Baseline`（不进默认测试集，300 个物件，记录 p50/p95 和每修订新增字节）；`make check-go PKG=./internal/creativescene/... ./internal/creativecontent/... ./internal/platform/store/... ./internal/platform/httpapi/...` 通过

## 4. 前端准备

- [ ] 4.1 浏览器输入穿刺（未决事项 2）：做一个录制页，记录 wheel（`deltaMode`、`deltaX/Y`、`ctrlKey` 等修饰键、时间间隔）、Safari `gesture*`、按键的 `code` / `key` / `metaKey` / `isComposing`，以及 `preventDefault` 能否拦下 Cmd/Ctrl + S、Cmd/Ctrl + D、捏合缩放页面；owner 在 macOS Chrome / Safari / Edge（触控板与鼠标）和 Windows Chrome（鼠标）上录制；事件序列存为 `frontend/scripts/fixtures/scene-input/*.json`，结论（`classifyWheel` 规则、各浏览器可拦截的组合键）写入 verification 草稿。录制页本身不入库
- [x] 4.2 依赖与懒加载入口：`frontend/package.json` 加 `three@0.186.1`、`@types/three@0.186.0`、`@react-three/fiber@9.8.1`、`@react-three/drei@10.7.9` 和新测试脚本；`SceneEditor.tsx` 空壳（`React.lazy`、WebGL2 检测、读取文档、只读占位）；`WorkspacePage.tsx` 按类型分派最大化；`CanvasView.tsx` 创建菜单「3D 场景」、`KindIcon`、`known`、未命名标签「未命名场景」、卡片物件数、最大化按钮；`outbox.ts` 的 `defaultActions` / `defaultNodeSize` 认识 `core.scene`；`make check-frontend` 通过，构建产物中画布入口块不含 three（AC-10）

## 5. 前端状态与同步

- [ ] 5.1 投影与几何：`state/apply.ts`（镜像服务端操作语义）、意图合并（队尾合并保留最早的预期修订号，复制后移动并入同一个 `add_objects`）、`builtins/*` 7 个几何函数；`creative-scene.test.ts` 先写，覆盖投影与服务端语义一致、合并规则、清单与几何函数一一对应
- [ ] 5.2 发送循环与意图终态：`state/sceneStore.ts`（zustand：权威快照、队列、投影、选择）、`state/sync.ts`（单请求在途；`pending` / `sent` / `unknown` / `blocked`；`{after}` 预期修订号的替换；只在 `objects` 冲突的新增自动变基、删除转确认、父对象已不在的新增不变基；依赖被拒意图的后续意图转 `blocked`；`pagehide` 时 `keepalive`；`beforeunload` 拦截；SSE 失效节流 500ms 后重读、拖动中推迟、`trashed_at` 切换只读）；状态机单测覆盖设计「意图终态」「离开提示」两行与 AC-5 前端、AC-7 前端
- [ ] 5.3 撤销与历史：`state/undo.ts`（撤销和重做都发 `restore_units`；按单元的 `foreignSeq` 过期规则只看物件单元，存在性变化时另查 `objects`；「重新移动后也不复活」；整份恢复清空两栈）、`history.ts`（按写入者、运行和时间窗分组与描述）、历史版本面板（只读预览、确认后整份恢复）；单测覆盖 AC-4 前端与 AC-6

## 6. 前端视口与交互

- [ ] 6.1 键位表与分发：`input/keymap.ts`（键位表是状态栏、菜单、速查的唯一来源；按 `event.code` 分发；macOS 认 `metaKey`、`Backspace` 视作 Delete；输入框内与组字中不分发；命中后 `preventDefault` + `stopPropagation`；Esc 的处理顺序且永不关闭视图；Ctrl/Cmd + S 拦下并提示已自动保存）；`creative-scene-interaction.test.ts` 先写，覆盖 AC-2c
- [ ] 6.2 视口与导航（依赖 4.1）：`Viewport.tsx`（R3F Canvas、`Grid`、天空、clay 光照、`GizmoHelper`）、`objects/*`（基础体、灯光图标、摄像机视锥、分组）、`input/navigation.ts`（`CameraControls` 配置；中键与 Alt + 左键并存；鼠标 / 触控板两套滚轮映射，`ctrlKey` 一律推拉；`classifyWheel` 推测加「视图」菜单手动选择并持久化；数字行视图、`.` 聚焦选中、Home 全部显示、反引号视图菜单）；单测覆盖 AC-2a，`classifyWheel` 用 4.1 的夹具
- [ ] 6.3 选择与变换：`input/selection.ts`（单击、Shift + 单击三态、`SelectionBox` 框选及 Shift 加选 / 修饰键减选、A / Alt + A、锁定与隐藏排除、只变换最上层选中物件）、`transform/modal.ts` + `numeric.ts`（G / R / S；X/Y/Z 全局 → 自身 → 取消循环、Shift + 轴锁平面；Ctrl 吸附 1 m / 5° / 0.1，Ctrl + Shift 精细吸附，Shift 精细从按下时起算；数字输入含小数、负号、Backspace、Tab 换轴；多物件以中点为枢轴；父对象下换算回局部变换；一次确认一个 `update_objects`）、gizmo 工具（`TransformControls` 挂在枢轴代理上）、W 切换工具、Shift + D、X / Delete、H / Shift + H / Alt + H、Alt + G/R/S、F2；单测覆盖 AC-2 模态变换与 AC-2b
- [ ] 6.4 面板与样式：工具栏、菜单栏（含 Shift + A 添加菜单，新物件落在鼠标所指的地面点）、物件列表、属性栏（左侧）、状态栏（当前工具或变换的轴向与数值、保存状态）、快捷键速查、右键菜单、首次提示、冲突提示（重新应用 / 放弃 / 仍然删除）、关闭视图时 `blocked` 意图的确认；`scene.css`；在助手面板展开时让出空间；菜单与状态栏的按键标注全部取自键位表；`make check-frontend` 通过

## 7. 端到端与基线

- [ ] 7.1 收尾验证：`creative-scene.e2e.mjs`（mock 接口：键盘流程 `Shift+A` → `G Z 2 Enter` → `R` 右键取消 → `A` `G` 确认并断言请求次数与最终快照；重开一致；两个页面同步与进出回收站；打开画布不请求 three 代码块）交 owner 本机运行，未运行时如实记录；跑基线测试并把 p50/p95、每修订字节、画布快照耗时写入 verification（AC-11）；`make generate-check`、`make check-frontend` 与组 3 的 `check-go` 全部通过
