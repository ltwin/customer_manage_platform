# Tasks

## 1. 修复

- [x] 1.1 `render/sunShadow.ts` `cascadeSunShadow` 的 undo 先逐级释放 `_shadowNodes` 及其阴影贴图的深度纹理，再释放级联节点；注明 three r186 核对过。完成判据：前端类型检查与 lint 通过。

## 2. 验证

- [x] 2.1 e2e「lights: the rendered view warms its lights in one compile…」转绿（红：第 1 轮后 RT 15 / 纹理 53，第 3 轮后 RT 23 / 纹理 85）。
- [x] 2.2 e2e「AC-5: … the sun at 5° lengthens rendered shadows …」通过：来回切换后太阳阴影照常。
- [x] 2.3 临时脚本核对：删除一盏投影的场景太阳灯后，视口 `renderTargets` 回到加灯前的数目。

## 结果

- 1.1：`tsc -b` 通过，`oxlint` 无告警。Prettier 对该文件的告警在修改前就有，与本次无关。
- 2.1：绿，在 worktree 的 Vite（5198）上运行该检查，`ok`（66.7 s）。修复前同一检查第 1→3 轮 RT 15→23、纹理 53→85。
- 2.2：绿，`ok`（17.2 s）。
- 2.3：临时脚本（已删除）只放一个箱子，切到「最终渲染」，外部加一盏投影平行光再删除：
  - `renderTargets` 10 → 11 → 10。只有落着投影物的那一级级联会画，所以只多 1 个。
  - 纹理 30 → 37 → 35。多出的 5 张都是加灯时替身预编译绑定的深度纹理，按设计保留到下次编译才释放（`SceneRenderer.kept`）。多轮不累积已由 2.1 证明。
