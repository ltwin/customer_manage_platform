# Tasks

## 1. 数据

- [ ] 1.1 后端：先改 `model_test.go`（部分字段补齐、越界与类型不符返回 400 并带 `volume.<field>`、体积带 `material_slots` 返回 400、第 9 个体积返回 413、`update_objects` 只改一个字段时其余不变）与 `fingerprint_internal_test.go`（改体积 `lit` 指纹变、`clay`/`line` 不变）；再改 `model.go`、`ops.go`、`fingerprint.go`；`api/openapi.yaml` 加 `CreativeSceneVolume`，运行生成器。完成判据：测试先红后绿，`go test ./internal/creativescene/` 通过，两份生成物已更新
- [ ] 1.2 前端：`state/apply.ts` 的 `contentKeys`、`updateKeys`、`mergeVolume` 与缺省值；`render/volumes/medium.ts` 的 JS 侧系数换算；单测 `creative-scene-volumes.test.ts`（系数换算、`mergeVolume`）。完成判据：单测通过，`tsc -b` 通过

## 2. 渲染

- [ ] 2.1 `render/volumes/state.ts`：体积登记表、`prepareVolumes` / `finishVolumes`、选灯排序与上限、结构签名、投影列表；`LitLighting` 登记「体积可画」。单测补选灯、签名、投影列表。完成判据：单测通过，`tsc -b` 通过
- [ ] 2.2 `medium.ts` 的 TSL 求交、密度、弦长；`lights.ts`；`march.ts` 的 `VolumeNode`（区间合并、步数分配、积分、两次模糊、纹理换图）；`composite.ts` 的覆盖掩码、上采样与分段合成；`aerial.ts:aerialAt`。接入 `sceneRenderer.ts` 工作台路径与 `realtimeGI.ts`；`objects/Volume.tsx` 的线框与登记，`SceneObjects.tsx` 渲染它。完成判据：e2e「W5 window」（视口与单帧导出）、「W5 spot」、「W5 range」、「W5 clay」、「W5 ortho」、「W5 aerial」通过；W3「day unchanged」、W4 far 与 GI 仍绿
- [ ] 2.3 首次编译与重建：步进材质在第一个体积出现时才建，视口异步编译、编好前不画，导出等编好；签名变化只重建步进材质。完成判据：e2e「W5 many lights」通过；没有体积时不建步进材质
- [ ] 2.4 表面阴影：`shadow.ts` 的 `volumeShadow` 与两个阴影节点子类；`SunCascades.setup` 乘系数；`lights.ts:buildLight` 装节点、`disposeLight` 释放；`standIn` 按原灯装同类阴影节点。完成判据：e2e「W5 cast shadow」「W5 shadow off」「W5 no recompile」通过；「sun shadows」仍绿

## 3. 编辑界面与助手

- [ ] 3.1 `editing.ts:newVolume`（落点单测）、`SceneEditor.tsx` 添加菜单两项与属性栏、`panels/VolumeFields.tsx`、`Outliner.tsx` 图标、`Volume.tsx` 的像素阈值拾取、`Viewport.tsx:centrePoint` 跳过体积。完成判据：e2e 走一遍添加、改密度、Cmd+Z 撤回；「W5 params」通过
- [ ] 3.2 `creativeagent/scene_tools.go` 的 `edit_scene` 说明与测试关键句；`docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md`。完成判据：`go test ./internal/creativeagent/` 通过

## 4. 验收

- [ ] 4.1 e2e「W5 twice」「W5 backends」；AC-W5-1 亮度比阈值按样张实测写进 e2e；owner 样张四张（窗口光束、聚光光锥、浓烟投影、介质里的点光）存进 `discovery/research/evidence/`。完成判据：`SCENE_E2E_ONLY=W5` 全绿，样张交 owner
- [ ] 4.2 性能（AC-W5-11）：1 个、8 个、8 个都投影三组，以及没有体积时帧时间和加灯编译时间与 develop 对照，记入 evidence；助手走一次（AC-W5-10）存截图。完成判据：数据齐全，未达标项写明原因
