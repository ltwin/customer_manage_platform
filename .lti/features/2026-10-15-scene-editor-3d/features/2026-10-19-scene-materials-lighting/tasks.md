# Tasks

## 1. 编组（后端与契约）

- [x] 1.1 `reparent_objects` 扩展：`ops.go` 的解码（`moves` 仍为 1–2000，`create_groups`、`remove_groups` 可选）、涉及单元（带变换的移动加摆放单元，删除的分组加两个单元）、应用顺序（新建 → 改挂 → 删除）和错误（重复出现、新建的不是分组、删除时仍有子物件）；`api/openapi.yaml` 的 `CreativeSceneReparentObjects`、`CreativeSceneMove`，`make generate`。先写 Go 测试：编组、取消编组、删除空分组、各失败情形、回执的 `changed_units`/`removed_units`，旧请求不变；`make check-go PKG=./internal/creativescene/...` 与 `make generate-check` 通过
- [x] 1.2 助手改挂：`lookat.go` 增加 `mat4.invert`、`decompose`（先写与 three.js 数值对照的测试）；`reparent.go` 的 `resolveReparent`（补局部变换和顺序、新建分组的默认位置、拒绝同批移动祖先、越界拒绝）；`agent.go` 去掉禁用；`run_changes.go` 先按 `changed_units` 与前一修订的 `unit_revs` 筛出候选，只对候选比对前后快照，判断层级变化并阻止对应物件；`summarizeChanges` 报改挂；`agent_tools_test.go`、`run_changes_test.go` 相应用例翻转或新增，测试通过

## 2. 编组（编辑器）

- [x] 2.1 `state/world.ts` 与 `grouping.ts`：五个操作各生成正向与反向的 `reparent_objects`；`state/apply.ts`、`state/intents.ts` 镜像服务端（含 `withoutNoops` 按容差去掉无变化的移动、新建分组的单元；取消的分组都没有子物件时改发 `remove_objects`）。先写单测：保持世界位置（误差 < 1e-6，含旋转缩放过的父级）、反向操作回到原父级与顺序、同一用例本地应用与 Go 回执一致；`make check-frontend` 通过
- [ ] 2.2 `state/sync.ts` 的 `keepPresent` 与 `objects` 冲突重排、`state/undo.ts` 的 `ops` 步骤（有效性检查、顺序重排、撤销与重做）、`agentActivity.ts`。单测覆盖：冲突后重排重试、父级缺失时提示、别人改过时撤销跳过、撤销后再重做
- [ ] 2.3 按键（Ctrl + G、Ctrl + Alt + G、Ctrl + P、Alt + P）、物件菜单项、`Outliner.tsx` 拖动（之前、之后、放入，不能拖到自己或后代上）；e2e 覆盖 AC-7 的五个场景

## 3. 显示模式与光照

- [ ] 3.1 `display` 状态：`sceneStore.ts`、`viewState.ts`（本机记住，`render.mode` 兜底）、`Z` 菜单、`Shift + Z`、顶栏按钮；线框模式（不可见可拾取的网格 + 按几何缓存的轮廓边，高亮改线色，直接 `gl.render`）。单测：读写与兜底；e2e：切换、重开保持、撤销不受影响
- [ ] 3.2 `Lighting.tsx`（白模分支原样搬迁，e2e 对比搬迁前后截图一致）、`SceneRenderer` 改名与 `Frame.ao`、透明网格集合与 AO 钩子；材质预览的 `RoomEnvironment`；最终渲染的太阳（阴影沿用 `fitSunShadow`）、three 自带 `Sky` 的背景与不含太阳盘的环境贴图、`ColorEnvironment`、补光、`SceneRenderer` 的曝光；`SceneLights.tsx`。单测：灯光字段到 three.js 属性的映射与 target 方向
- [ ] 3.3 曝光补偿：`model.go` 的 `render.exposure_ev`（范围、补 0）、`ops.go` 的 `set_render` 按键合并、`fingerprint.go` 的 `lit` 投影（为 0 时不写入）、`api/openapi.yaml` 的 `CreativeSceneRender` 与 `make generate`、`state/apply.ts` 的合并。先写 Go 测试：合并、范围、补默认、`lit` 指纹随曝光变化且为 0 时与原指纹相同
- [ ] 3.4 标定：参考场景（室外正午、黄昏、室内两盏壁灯）下定出 `SUN_MAX`、环境贴图强度、`AMBIENT_FILL` 和常用灯光强度（只定一组），以及各类场景的建议曝光，写回 design 未决事项 1 与工具说明的数值

## 4. 材质

- [ ] 4.1 材质目录：`materials.json`（原理化 BSDF、14 种预设、提示映射）、`materials.go`（嵌入与严格解析、`MaterialCatalog()`）、`Makefile` 的复制与比对、`materials/catalog.ts`。先写测试：目录合法、默认值在范围内、每个预设的参数表锁定、前后端副本一致
- [ ] 4.2 `models/cache.ts` 保留槽位、提示和材质常量，部件矩阵烘焙进几何（行列式为负时翻转绕序）；`materials/resolve.ts` 的解析表五条与参数夹取；`materials/principled.ts`（`anisotropy_rotation` 为弧度）。先写单测（含 `creative-scene-model.test.ts` 的部件断言调整）
- [ ] 4.3 程序化着色：`materials/inject.ts`（锚点检测与失败退回）、`patterns.ts`（公共噪声、14 个预设、凹凸；硬边用 `fwidth` 驱动的 `smoothstep`，高频八度和凹凸按 `fwidth(vPos)` 淡出）、`instances.ts`（共享与引用计数）；`SceneObjects`、`ModelMesh` 按显示模式选材质。单测：锚点、缓存键；e2e：AC-2 的缩放不拉伸、金属反射且只有一个太阳高光、远处砖墙平移半个像素前后像素差低于阈值、Alpha 与 AO；逐个预设出样张存档
- [ ] 4.4 属性栏「材质」区（槽位列表、来源标记、参数、转为原理化、多选作用于同名槽位、合并键）与「复制材质到选中物件」、槽位复制粘贴；e2e 覆盖 AC-2 多选、AC-3

## 5. 灯光与环境编辑

- [ ] 5.1 添加菜单的三种灯光（`editing.ts:newLight`）、`LightFields.tsx`、`EnvironmentPanel.tsx`（含曝光补偿，经 `set_render`）、`set_environment` 的合并键；e2e 覆盖 AC-4、AC-5

## 6. `lit` 导出与助手

- [ ] 6.1 渲染端：`ShotKind` 加 `lit`、`openShooter` 的 `look` 与 `setLook`（`compileAsync`）、`renderShots` 按渲染方式分批、`render_view` 的模式；导出对话框的 `lit` 与默认勾选；后端 `jobs.go` 的 `mode` 与 `exportJobPasses`。单测与 Go 测试先行；e2e 覆盖 AC-6
- [ ] 6.2 助手：工具层的材质槽按键合并与目录校验、HDRI 拒绝；`set_render` 的曝光说明；`edit_scene`、`render_view`、`export_shots` 的参数与说明；`instructions/base.md`；工具说明的钉住测试与压缩窗口按需调整；助手 e2e 的黄昏布置样例。Go 测试覆盖错误信息与列出的可用项

## 7. 收尾

- [ ] 7.1 AC-9 性能实测（参考场景 fixture，四种模式的帧时间与加灯卡顿）写进 design；更新 `docs/dev/agent-skills-tools.md`、`docs/dev/creative-scene-agent.md`；建 `discovery/research/lit-repaint.md` 供 owner 记录 AC-10 重绘对照
