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

S2 是 Epic `scene-light-size` 的最后一个子项，依据批准基线 v5（v4 加上面光源缩放的口径）。Epic 的目标、光源模型、面光源的着色和阴影做法、AC 阈值都在 Epic 的 proposal 和 design 里，这里不重复。

S1 已经打通点光、聚光、平行光的尺寸链路。S2 在这条链路上加一种灯：矩形和椭圆的面光源，用来做柔光箱、灯板和窗户。S2 完整交付 Epic 的目标契约 `data: 面光源`（`epic bind --deliver`），交付后 Epic 进入整体验收。

## What Changes

按 Epic roadmap 的 S2 行，逐项对应 Epic design 的章节：

1. **数据与服务端**：
   - `light.type` 加 `area`，字段 `shape`、`width`、`height`，按 `data: 面光源` 校验和补缺省值；
   - 强度上限按类型区分：面光源 100000 cd/m²，其他类型仍为 1000；
   - 换类型时按等效发光强度换算强度（Epic I6）；
   - OpenAPI 加 `area` 和这三个字段，提交 Go 和 TS 两份生成物；
   - 助手的场景工具说明补上面光源。
   - 光源模型版本号不加：只新增类型，已有画面不变（Epic D4）。
2. **光照**（Epic design「直接光的入口」）：
   - 新灯光子类 `SceneAreaLight`，尺寸取物件世界缩放后的宽高（含父级）；
   - 矩形用 three 的 LTC 和 `directRectArea`，补上 sheen；
   - 椭圆自写 LTC 圆盘（Heitz & Hill 2017），clearcoat 和 sheen 照矩形的写法；
   - 只有正面发光。
3. **软阴影**（Epic design「半影宽度」U3、U5）：
   - 面光源用中心的立方体阴影图，PCSS 的滤波核与面光源两条轴对齐；
   - 出图用 32 + 64 次采样、上限 128 texel，视口用 16 + 32 次、上限 48 texel；
   - 面光源总带尺寸，投影时总挂软阴影，`workbench` 出图照常做时域累积；
   - standIn 和投影体积的阴影节点按立方体处理。
4. **命中点**：在 S1 定下的灯光表格式里填入面光源（不改格式），命中点用同一套 LTC 着色，阴影光线在发光面上均匀取点。
5. **体积**：面光源在体积里按中心处、朝 −Z 的单面余弦光计算，强度为亮度 × 面积，衰减钳在 `max(d², A/π)`。
6. **编辑器**：
   - 新建菜单（Shift+A）加「面光源」，缺省 0.25 m 方形、10000 cd/m²；
   - 属性面板加「形状 / 宽 / 高」，强度旁显示等效发光强度（cd）；切换类型时写入换算后的强度；
   - 图标画矩形或椭圆轮廓，随物件缩放，加一条沿 −Z 的朝向短线；
   - `lightDefaults` 拆成按类型分开的表。
7. **验证**：补提交面光源棚拍参考场景（S0 当时是运行时合成的，没有提交），e2e、性能复测、owner 看图，见「验收标准」。

**不在本次**：面光源的 `spread`、IES、光源贴图，体积里的软阴影，都是 Epic 的非目标。

## Acceptance Criteria

阈值都取自 Epic proposal。数据在参考设备上测得，对照用光追参考解（`scripts/light-check/`）和新提交的棚拍参考场景 `area-studio`。下文「Epic AC-n」指 Epic proposal 的同名条目，S2 只验收其中面光源的部分。

- **AC-S2-1 面光源着色与阴影**（Epic AC-5）：
  - WHEN `area-studio` 里分别放一个矩形面光源和一个椭圆面光源，都沿局部 −Z 照射；
  - THEN 背面不照亮任何东西；与参考解相比：
    - 不带阴影时，墙面漫反射和高光板的能量相差不超过 5%；
    - 带阴影时，全图阴影系数误差均值 ≤ 0.005；
    - 每块板剖面的误差均值：1×1 m 光源 ≤ 0.02，2×0.5 m 和 2×2 m 光源 ≤ 0.05；
  - 并且缩放物件时，发光面和视口里的轮廓一起变化，两者一致。
- **AC-S2-2 平方反比**（Epic AC-3 的面光源部分）：WHEN 在面光源法线方向上 d ≥ 10 倍光源边长处测照度，THEN 与 `L × A / d²` 的相对误差不超过 2%；d 小于光源边长时，照度不超过 π·L（面光源能给的上限），不出亮斑。
- **AC-S2-3 命中点一致**（Epic AC-7）：WHEN 实时 GI 的屏幕外反射映出被矩形或椭圆面光源照亮的表面，THEN 命中点着色与光栅的差异不超过 GI-S1 AC-S1-2 的同类阈值。
- **AC-S2-4 性能**（Epic AC-8 的面光源部分）：
  - 场景：参考设备，1200 × 800，S1 的性能场景（聚光、点光、平行光、程序化天空的太阳）把平行光换成一个 1×1 m 投影的面光源，仍是 4 盏投影的有尺寸光源；
  - 对照：S1 的对照场景，即聚光、点光、平行光、太阳尺寸全为 0（面光源没有尺寸为 0 的状态，所以对照里没有它）；
  - WHEN 冷启动测量，THEN 视口每帧增量 ≤ 8 ms，`realtime_gi` 导出时长增量 ≤ 50%；第一次构建和 `workbench` 导出的增量按实测记录进 verify。超出时交 owner 决定，不悄悄降低画质。
- **AC-S2-5 编辑器与助手**（Epic AC-9 的面光源部分）：
  - 新建菜单能加面光源；属性面板能改形状、宽、高，显示等效发光强度，撤销可用；
  - 助手按工具说明写入面光源；
  - 面光源写了 `radius_m`、`distance` 等别的类型才有的字段，或点光写了 `width`，返回 400 并指出字段；
  - 点光或聚光与面光源互换类型、且没有显式写强度时，强度按等效发光强度换算并截断，不会越界报错；显式写了强度时以写入的为准；属性面板切换类型的结果与服务端相同。
- **AC-S2-6 防回退**：
  - WebGPU 和 WebGL2 各有一个 e2e，检查面光源所在之处着色正确，不缺失、不全黑；
  - 投影的面光源照着投影体积时，体积着色不缺失、不全黑；
  - `workbench` 最终导出连续两次逐像素相同；
  - 已有画面不变：S1 的 AC-S1-2 尺寸为 0 基线和 AC-S1-10 的几条 e2e 仍然通过，`lit` 指纹的光源模型版本号不变。
- **AC-S2-7 owner 看图**：`area-studio` 场景的矩形、椭圆面光源出图，`workbench` 和 `realtime_gi` 各一组，由 owner 判断观感是否接受。

## Impact

- **服务端**：`backend/internal/creativescene`（灯光字段、校验、补齐、换类型换算）、`backend/internal/creativeagent`（工具说明）、`api/openapi.yaml` 和两份生成物。
- **前端渲染**：`render/lightModel/`（灯光子类、光源节点、椭圆 LTC、立方体 PCSS 的矩形核与出图档）、`objects/lights.ts`、`render/volumes/`、`render/trace/`（`collect.ts`、`tables.ts`、`hit.ts`）、`render/sceneRenderer.ts`（standIn）。
- **编辑器**：`panels/LightFields.tsx`、`objects/SceneObjects.tsx`（`LightIcon`）、`editing.ts`、`SceneEditor.tsx`（新建菜单）、`state/apply.ts`。
- **e2e**：`frontend/scripts/creative-scene.e2e.mjs`、`frontend/scripts/light-check/`，新增参考场景 `frontend/scripts/fixtures/scene-reference/area-studio`。
- **契约**：交付 `data: 面光源`；Living Spec 加「面光源」（见 specs.md）。
- **画面**：已有场景不变。

## Risk Notes

- **缩放取世界缩放**（owner 2026-10-27，Epic 修订 v5）：面光源的尺寸包括父级物件的缩放，与网格物体和 Blender 一致。服务端已有按父级链计算的世界矩阵（`lookat.go`），换类型时同样按世界缩放换算，面板、助手和服务端结果一致。
- **性能**：S0 估计一个视口档面光源每帧约 +5 ms，S1 的 4 盏灯是 +4.3 ms，同时出现时可能接近 8 ms。AC-S2-4 用面光源替换平行光，仍是 4 盏；超出时交 owner 决定。出图档每帧约 22 ms（S0 2×2 m），只在导出时用。
- **椭圆 LTC 是新写的算法**：three 和 S0 原型都没有。按论文实现，单测与解析解、数值积分对照，再用参考解核对 AC-S2-1 的能量；对不上时先查实现，再交 owner。
- **S0 的矩形偏亮 4% 原因不明**：会让 AC-S2-2 的 2% 过不了。实施的第一个任务就是查清是光栅还是参考解的归一化问题，修掉那一边（design 未决事项 N2）。
- **three r186 的绕法**：`RectAreaLight` 不投影、`RectAreaLightNode` 不读缩放，都要靠子类绕开，加注释并纳入 `render/lightModel/nodes.ts` 的升级核对清单。
- **合并顺序**：以当前 develop 为底开工，合回前按 `make check-merge` 处理。
