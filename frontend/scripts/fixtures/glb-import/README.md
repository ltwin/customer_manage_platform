GLB 导入（scene-glb-import）单测用的压缩几何样本，内容是合成的，不来自任何外部素材：

- 源模型：three 0.186 的 `GLTFExporter` 导出的两个网格。
  - `Body`：半径 0.5 的球，12 × 8 段，带 `COLOR_0`，材质 `Body`。
  - `Trim`：1 × 0.1 × 1 的盒子，材质 `Trim`。
- `draco.glb`：`npx @gltf-transform/cli@4.5.1 draco source.glb draco.glb`，使用 `KHR_draco_mesh_compression`。
- `meshopt.glb`：`npx @gltf-transform/cli@4.5.1 meshopt source.glb meshopt.glb`，使用 `EXT_meshopt_compression` 和 `KHR_mesh_quantization`，属性是整数。

两份都不带贴图。带贴图、外部 URI、量化等其他夹具由测试在运行时生成。
