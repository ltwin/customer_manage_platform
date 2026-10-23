---
risk:
  factors:
    - perf-sensitive
    - broad-impact
behavior: changed
reads:
  - creative/scene
decisionsPending: false
---
# 本次交付

## Why

Epic `scene-world-environment`（批准版本 v9）目标 6「体积物件」：新增有范围的体积物件，参照 Blender 中带体积散射和体积吸收的物体，太阳、聚光和点光都照亮它，形成光束和阴影。本次是 roadmap 的 W5，也是 Epic 的最后一个子项。

穿刺已完成（`discovery/research/volume-lighting.md`），做法、后端、合成位置、上限和两处观感都已写进修订 v8；方案评审后，合成顺序、步数分配、灯数上限和无体积时的开销由修订 v9 补正。本次按它实施，完整交付 `data: ADD 体积物件` 与 `api: MODIFY 提交场景命令`。

## What Changes

1. **数据与服务端**（`data: 体积物件`，Epic 已定义，不改）：
   - 新对象类型 `kind = "volume"`，内容字段 `volume`：
     - `shape`、`scatter_color`、`scatter_density`、`anisotropy`；
     - `absorption_color`、`absorption_density`、`edge_falloff`；
     - `cast_shadow`。
   - 校验、补齐缺省值和按键合并，与 `light`、`camera` 的写法相同。
   - 每个场景最多 8 个体积物件，超出返回 413。
   - 渲染指纹：`lit` 计入体积物件，`clay`、`line` 清除。
   - OpenAPI 加 `CreativeSceneVolume`，并重新生成两份生成物。
2. **渲染**（Epic design「体积物件（W5）」）：
   - **步进与合成：**
     - 逐像素光线步进，按 CSS 像素的一半；步数按各体积区间的长度分配；加深度加权模糊和上采样，全分辨率的覆盖掩码保证范围外不变；
     - 接在工作台和实时 GI 两条路径里空气透视之后，体积前后两段空气按体积深度分开计入（Epic 修订 v9）；
     - 只在最终渲染、`lit` 导出和 `render_view` 的 `lit` 里绘制；没有体积的场景不步进，开销为 0。
   - **光源：** 太阳、月光、文档里的平行光、聚光和点光（按各自的阴影贴图），最多 6 盏，按对体积的贡献取；加天空光照；每个体积算自身的自遮挡。
   - **投射阴影：** 打开 `cast_shadow` 的体积，在平行光、聚光、点光的阴影上乘以透射率。
   - **预编译：** 替身灯装与原灯同类的阴影节点，新灯不再让材质在下一帧重新编译（顺带修好太阳级联阴影现有的错配）。
3. **编辑界面**：
   - 添加菜单在「其他」一节加「体积（立方体）」「体积（球体）」，新体积落在视口中心对着的地面上，大小 2 m；
   - 体积的线框按屏幕像素阈值拾取，不抢走它范围里其他物件的点击；
   - 物件列表有体积的图标；
   - 属性栏加「体积」一组：
     - 形状；
     - 散射颜色与密度、各向异性；
     - 吸收颜色与密度；
     - 边缘柔化；
     - 投射阴影。
   - 视口在任何显示模式下都显示范围线框，最终渲染下照常绘制介质；白模导出、线稿导出和 `render_view` 的 `clay` 里看不到。
4. **助手**：`edit_scene` 的工具说明加 `kind: volume` 与 `volume` 的字段、范围、上限，并给一个「窗前的雾」样例和「密度 0.05–0.3 是薄雾，2 以上是浓烟」的提示。
5. **文档**：`docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md`。

不在范围内（Epic 已定的局限）：
- 体积之间互相遮挡；
- 透明物件后面的体积按不透明表面合成；
- 天空光照照亮体积时不计遮挡；
- 体积云。

## Acceptance Criteria

- **AC-W5-1 窗口光束**：**WHEN** 屋里放一个盖住整间屋子的体积物件（散射密度 0.1，各向异性 0.6），太阳从窗口照进来，**THEN** 看到穿过窗口的光束，窗棂在光束里留下暗带；光束里的亮度与光束外之比不低于阈值（阈值由穿刺样张实测后定，写进 e2e）。视口和单帧工作台导出都成立。
- **AC-W5-2 聚光光锥**：**WHEN** 聚光照进体积，**THEN** 看到光锥，锥外没有这盏灯的散射。
- **AC-W5-3 范围**：**WHEN** 移动或缩放体积物件，**THEN** 只有它覆盖的范围变化，范围外的像素（包括轮廓附近）与没有体积时一致。
- **AC-W5-4 参数**：**WHEN** 分别调大散射密度、改吸收颜色、改各向异性（逆光看），**THEN** 画面变化方向与 Blender 的体积散射和体积吸收一致：更亮更浓、透过的颜色偏向吸收颜色、正向散射时逆光更亮。
- **AC-W5-5 投射阴影**：**WHEN** 给一团浓的体积打开投射阴影，**THEN** 它后面被太阳照着的地面变暗；关闭后地面与没有体积时一致。
- **AC-W5-6 白模与线稿**：**WHEN** 切到白模、线框或材质预览，**THEN** 体积只显示范围线框；白模导出、线稿导出和 `render_view` 的 `clay` 与没有体积时逐像素一致。
- **AC-W5-7 两个后端**：**WHEN** 在 WebGPU 和 WebGL2（SwiftShader）下渲染 AC-W5-1 的场景，**THEN** 两张图的差异不超过 Epic 定的 `lit` 阈值。
- **AC-W5-8 数据与过时**：
  - **WHEN** 添加体积物件只写了部分字段，**THEN** 其余补缺省值；越界、带了 `material_slots` 等别的内容字段、或第 9 个体积，分别返回 400、400、413，并写明字段或上限。
  - **WHEN** `update_objects` 只写 `volume` 的一个字段，**THEN** 其余字段不变。
  - **WHEN** 改了体积物件，**THEN** `lit` 导出提示「场景已更新」，`clay`、`line` 不提示。
- **AC-W5-9 确定性**：**WHEN** 在隐藏的标签页里连续两次导出含体积物件的场景，**THEN** 两次结果逐像素相同。
- **AC-W5-10 助手**：**WHEN** 请助手「在窗前放一团能看到光束的雾」，**THEN** 助手添加一个体积物件，写对字段，并用 `render_view` 的 `lit` 回看。
- **AC-W5-11 性能**（Epic AC-11）：
  - **WHEN** 在参考设备（M4 Pro，WebGPU）的视口（1280 × 720 CSS 像素、DPR 2）里放 1 个体积物件、三盏投影的灯，**THEN** 体积带来的帧时间不超过 3 ms；8 个时不超过 4.5 ms；8 个都打开投射阴影时再多不超过 4 ms。
  - **WHEN** 场景里没有体积物件，**THEN** 视口帧时间与 develop 相差在测量误差以内；加一盏投影的灯时，材质的编译时间不比 develop 多。
- **AC-W5-12 远处与空气透视**：**WHEN** 物理天空下打开空气透视并调大距离倍率，一团体积跨在地平线上，**THEN** 地平线上下两侧没有接缝，体积后面的霾不叠在浓烟上。

## Impact

- **前端：**
  - 新增 `render/volumes/`（介质、状态与选灯、受光、步进、合成、表面阴影）、`objects/Volume.tsx`（线框、拾取与登记）、`panels/VolumeFields.tsx`；
  - 修改 `render/sceneRenderer.ts`、`render/realtimeGI.ts`、`render/sunShadow.ts`、`world/atmosphere/aerial.ts`、`objects/lights.ts`、`objects/Lighting.tsx`、`objects/SceneObjects.tsx`、`SceneEditor.tsx`、`Viewport.tsx`、`editing.ts`、`panels/Outliner.tsx`、`state/apply.ts`。
- **后端：** `creativescene` 的 `model.go`、`ops.go`、`fingerprint.go` 及测试；`creativeagent/scene_tools.go`。
- **契约：** `api/openapi.yaml` 及两份生成物；交付 `data: ADD 体积物件`、`api: MODIFY 提交场景命令`。
- **已有场景：** 没有体积物件的场景画面不变，指纹不变。

## Risk Notes

- **性能**（perf-sensitive）：有体积时每帧多一个低分辨率的步进 pass 和两次模糊；没有体积时不步进。
  - 穿刺实测（WebGPU）：1 个 2.0 ms、8 个 3.0 ms、8 个都投影 +2.5 ms，对目标约 1.5 倍余量；WebGL2 下 8 个都投影 +3.6 ms，只有约 1.1 倍。AC-W5-11 按 WebGPU 验收。
  - 每盏参与的投影灯约多 0.4 ms，最多 6 盏。
  - 外接大屏（CSS 尺寸更大）时开销随像素数增长。
- **影响面**（broad-impact）：所有投影的灯都换成带体积系数的阴影节点。没有打开投射阴影的体积时，循环次数为 0。回归靠 W3 的「day unchanged」基线和 `fix/scene-sun-cascaded-shadows` 的「sun shadows」e2e：没有体积时画面不变。材质的编译时间和新灯的预编译另由 AC-W5-11 和「no recompile」e2e 检验；`standIn` 的改动影响所有新灯的预热。
- **重新编译**：灯光组合改变时只重新编译步进材质（穿刺：热缓存下 150–450 ms），视口里异步编译，编好前不画体积。
- 没有新的共享设计问题，局部决定写在 design「局部设计与偏差」。
