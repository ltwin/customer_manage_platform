# Tasks

## 1. 修复

- [ ] 1.1 `render/sunShadow.ts` `cascadeSunShadow` 的 undo 先逐级释放 `_shadowNodes` 及其阴影贴图的深度纹理，再释放级联节点；注明 three r186 核对过。完成判据：前端类型检查与 lint 通过。

## 2. 验证

- [ ] 2.1 e2e「lights: the rendered view warms its lights in one compile…」转绿（红：第 1 轮后 RT 15 / 纹理 53，第 3 轮后 RT 23 / 纹理 85）。
- [ ] 2.2 e2e「AC-5: … the sun at 5° lengthens rendered shadows …」通过：来回切换后太阳阴影照常。
- [ ] 2.3 临时脚本核对：删除一盏投影的场景太阳灯后，视口 `renderTargets` 回到加灯前的数目。
