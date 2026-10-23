# Tasks

## 1. 数据

- [ ] 1.1 后端：先改 `scene_test.go`（部分字段补齐、W3 时期快照补齐为开启与倍率 1、非物理天空写它返回 400、倍率越界与未知字段返回 400，删去「aerial_perspective 返回 400」的旧用例）与 `fingerprint_internal_test.go`（改空气透视 `lit` 指纹变、`clay`/`line` 不变）；再改 `model.go`、`sky.go`；`api/openapi.yaml` 加 `CreativeSceneAerialPerspective`，直接运行生成器。完成判据：测试先红后绿，`go test ./internal/creativescene/` 通过，生成物已更新
- [ ] 1.2 前端：`world/physicalSky.ts` 的缺省值、`completeSky`、`isPhysical`；更新期望整份物理天空的单测（`creative-scene-astro.test.ts`、`creative-scene.test.ts`）。完成判据：单测通过，`tsc -b` 通过

## 2. 渲染

- [ ] 2.1 `world/atmosphere/aerial.ts`：froxel 生成 pass（1024 × 32 图集，太阳与月亮）、`applyAerial`、全局 uniform；CPU 上同一积分的单条视线参考（单测：正午远处偏蓝、透射率随距离单调下降、倍率放大等价于距离放大、太阳在地平线下且无月时散射为 0）。完成判据：单测通过，`tsc -b` 通过
- [ ] 2.2 接入：`SceneRenderer` 出图前生成 froxel，工作台路径接 `applyAerial`；`realtimeGI.ts` 接 GI 路径；`PhysicalLighting` 写开关、倍率与光源，卸载时关闭。完成判据：e2e「W4 far boxes」（AC-W4-1、AC-W4-2、AC-W4-4）与「W4 off」（AC-W4-3，关闭时与 W3 基线逐像素一致）通过；W2、W3 已有用例仍绿

## 3. 编辑界面与助手

- [ ] 3.1 `PhysicalSkyFields.tsx`「空气透视」一组（开关、对数倍率滑杆、关闭时置灰），`editing.ts` 的提交。完成判据：e2e 走一遍开关、拖倍率、Cmd+Z 撤回
- [ ] 3.2 `creativeagent/scene_tools.go` 的 `edit_scene` 说明与 `scene_tools_test.go` 关键句；`docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md`。完成判据：`go test ./internal/creativeagent/` 通过

## 4. 验收

- [ ] 4.1 e2e「W4」：确定性（AC-W4-8）、`SCENE_E2E_GPU` 下两个后端对照（AC-W4-5）、性能（AC-W4-9，复用「W3 performance」做开关对照，结果存进 `discovery/research/evidence/`）。完成判据：`SCENE_E2E_ONLY=W4` 全绿，性能数据齐全，未达标项写明原因
