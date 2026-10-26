<!-- 已批准的目标契约，不代表项目当前已实现。两条都是新增的数据章节，扩展 data: 场景文档 的「灯光」，做法与 data: 摄像机曝光、data: 体积物件 相同，不改写 场景文档 的正文。尺寸上限是暂定值，S0 可能经 Epic 修订调整（design U5）。 -->

## data: ADD 光源尺寸

### 用途与存储角色
- **用途**：扩展 `data: 场景文档` 的 `objects[].light`，照 Blender 的光源模型给点光、聚光和平行光加上尺寸：
  - 点光和聚光是有半径的球；
  - 平行光是有角径的圆盘。
  尺寸决定阴影半影的宽度和高光的大小，不改变光源强度。
- **存储**：与场景文档相同。字段位于灯光对象的内容单元 `object:<id>:content` 内。
- **角色**：权威主数据。半影、代表点和滤波半径都是渲染端的派生量，不落库。
- **所有权**：
  - 由 `creativescene` 经 `add_objects`、`update_objects` 写入，写入方是灯光属性面板和助手；
  - 读取方是渲染端和渲染指纹。

### 结构与身份
新字段与现有灯光字段平级，`update_objects` 里按键合并，可省略，存储时补齐缺省值。

| 路径 | 适用 `type` | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|---|
| `light.radius_m` | `point`、`spot` | number 0…10（米） | 0.1 | 发光球的半径，即 Blender Point / Spot 的 Radius。0 表示理想点光源，阴影是硬的，与加入本字段前一致 |
| `light.angular_diameter_deg` | `directional` | number 0…30（度） | 0.545 | 光源的角直径，即 Blender Sun 的 Angle。缺省值与 `sky.sun_size_deg` 的缺省值相同。0 表示理想平行光 |

- 写了该类型没有的字段返回 400 并指出字段，与现有规则相同。
- **强度与衰减**：
  - `intensity` 的含义不变，点光和聚光仍为发光强度（cd）。
  - 距离衰减把有效距离 `max(d, radius_m)` 代入现有公式，`decay` 和 0.01 的钳位照旧，`distance` 截断仍按真实距离 d。
  - 缺省 `decay = 2`、`distance = 0` 时，法线朝光源、球面完全在受光面地平线以上的照度为 `intensity / max(d², radius_m², 0.01)`。在光源以外按平方反比衰减，进入光源以内照度不再增大；`radius_m = 0` 时与现状相同。
  - `decay ≠ 2` 或 `distance > 0` 照样代入，但不是物理上精确的解。
- **缩放**：点光和聚光的半径不受物件缩放影响。是否与 Blender 一致，S0 核对后由 Epic 修订确认（design U8）。
- **聚光**：`penumbra` 只管光锥边缘，不控制阴影软硬，与 Blender 的 Blend 一样。
- **换类型**：保留新类型也有的字段，去掉其余，缺少的补缺省值。强度的换算见 `data: 面光源`「换类型」；点光、聚光和平行光之间的强度数值原样保留。
- **天空的太阳和月亮**：它们不是灯光对象，没有这两个字段，渲染端这样取角径：
  - 物理天空的太阳取 `sky.sun_size_deg`，与画出的太阳盘一致；
  - 月亮取 0.52°；
  - 程序化天空的太阳取 0.545°。

### 访问模式与保证
- **写**：按现有灯光字段的规则校验和合并，取值范围由应用保证，非有限值返回 400。
- **读**：旧快照里的灯光没有这两个字段，服务端解码时补缺省值，读取场景的响应里也带齐。这与修订 v8 的景深字段、`data: 摄像机曝光` 的做法相同。
- **渲染指纹**：
  - 两个新字段随灯光对象计入 `lit` 指纹，改尺寸会让 `lit` 导出过时。
  - 光源模型版本号：`lit` 的场景里有灯光对象，或天空为 `procedural` 或 `physical`（太阳会投影）时，`lit` 指纹计入版本号。本条交付时为 `lights-v2`。所以这些场景已有的 `lit` 导出各提示一次「场景已更新」；HDRI 或纯色天空、又没有灯光对象的场景不提示。
  - 版本号是服务端指纹的常量，反映的是渲染端的灯光语义。之后哪个改动改变了已有文档的灯光画面，就由它在同一次提交里加一。
  - `clay` 和 `line` 不读取灯光，不计入。
- 不进导出图来源的快照，`data: 导出图来源` 不变。

### 生命周期与演进
- 不迁移，读取时补齐。补齐后已有场景的灯会带上缺省尺寸，阴影变软；需要硬阴影的，把尺寸写成 0。
- 回滚到不认识这两个字段的后端时，读取失败，只做前向修复，同 `render.engine` 的先例。
- 上限是暂定值，S0 后可能经 Epic 修订调整。放宽属于兼容扩展；收紧时，已存的超限值在读取时按新上限截断。
- 强度改用 Blender 的 W 不在本条范围内。

### 表达与样例自查
```json
{"op": "add_objects", "objects": [
  {"id": "sobj_V1StGXR8_Z5jdHi6B-myT", "kind": "light", "name": "主灯", "parent_id": null, "order": 3,
   "transform": {"position": [-2, 3, 1], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}, "visible": true, "locked": false,
   "light": {"type": "spot", "intensity": 400, "cast_shadow": true, "radius_m": 0.25}}]}
```
存储后为 `{"type":"spot","color":"#ffffff","intensity":400,"cast_shadow":true,"distance":0,"decay":2,"angle":1.0471975511965976,"penumbra":0,"radius_m":0.25}`。

```json
{"op": "update_objects", "updates": [{"object_id": "sobj_V1StGXR8_Z5jdHi6B-myT", "light": {"type": "directional"}}]}
```
存储后为 `{"type":"directional","color":"#ffffff","intensity":400,"cast_shadow":true,"angular_diameter_deg":0.545}`。同一次如果写了 `"radius_m": 0.3`，返回 400，`field` 指向 `light.radius_m`。

## data: ADD 面光源

### 用途与存储角色
- **用途**：扩展 `data: 场景文档` 的 `objects[].light`，新增 `type = area`：矩形或椭圆的单面面光源，对应 Blender 的 Area 灯和 three.js 的 `RectAreaLight`，用来做柔光箱、灯板和窗户这类大面积光源。
- **存储**：与场景文档相同，仍是 `kind = light` 的对象，字段位于 `object:<id>:content` 内，不新增对象种类。
- **角色**：权威主数据。LTC 系数、阴影贴图和半影都是渲染端的派生量。
- **所有权**：
  - 由 `creativescene` 经 `add_objects`、`update_objects` 写入，写入方是新建灯光菜单、属性面板和助手；
  - 读取方是渲染端和渲染指纹。

### 结构与身份

| 字段 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `type` | `"area"` | — | 必填 |
| `color` | `#rrggbb` | `#ffffff` | 同其他灯光 |
| `intensity` | number 0…100000（cd/m²） | 10000 | 发光面的亮度，同 three.js `RectAreaLight.intensity`。缺省值约为常见 LED 灯板的亮度量级，0.25 m 方形时法线方向约 625 cd。three.js 的缺省值是 1，在缺省尺寸下只有 0.0625 cd，看不见，所以这里不用它 |
| `cast_shadow` | boolean | false | 同其他灯光 |
| `shape` | `"rectangle"` \| `"ellipse"` | `"rectangle"` | Blender 的 Square 和 Disk 分别是宽高相等的矩形和椭圆 |
| `width` | number 0.01…20（米） | 0.25 | 沿物件局部 X 轴的尺寸，同 three.js `RectAreaLight.width`；椭圆时为 X 向直径 |
| `height` | number 0.01…20（米） | 0.25 | 沿物件局部 Y 轴的尺寸，同 three.js `RectAreaLight.height` |

- **方向**：沿物件局部 −Z 单面发光，背面不发光，与聚光、平行光和 Blender 的 Area 灯一致。
- **缩放**：发光面随物件缩放，与 Blender 一致：实际宽为 `width × |scale.x|`，实际高为 `height × |scale.y|`，`scale.z` 不起作用。
- **等效发光强度**：法线方向上的等效发光强度 = `intensity` × 实际发光面积。矩形的面积是宽 × 高，椭圆是 π/4 × 宽 × 高。
- **没有的字段**：面光源没有 `distance`、`decay`、`angle`、`penumbra`、`radius_m` 和 `angular_diameter_deg`，写了返回 400 并指出字段。衰减由面积分自然得到，距离远大于尺寸时按平方反比下降。
- **换类型**（`update_objects` 里 `light.type` 改变时，由服务端在合并时完成）：
  - 保留新旧类型都有的字段（`color`、`intensity`、`cast_shadow`），去掉其余字段，缺少的补缺省值。
  - 本次写入**没有**显式写 `intensity` 时，按等效发光强度换算，再截断到目标类型的范围：
    - 点光或聚光 → 面光源：`intensity = min(原强度 / 新面积, 100000)`。新面积用本次写入或补齐后的 `width`、`height`、`shape` 和当前缩放计算；
    - 面光源 → 点光或聚光：`intensity = min(原亮度 × 原面积, 1000)`；
    - 与平行光之间：数值原样保留，截断到目标范围。
  - 本次写入显式写了 `intensity` 时，以写入的值为准，照常校验范围。
  - 编辑器切换类型时，显式写入换算后的值，结果与服务端相同。
- **体积**：体积物件的 `cast_shadow` 对面光源同样生效。在体积散射里，面光源按中心处朝 −Z 的单面余弦光近似，距离衰减钳在 `max(d², 面积/π)`。

### 访问模式与保证
- **写**：按现有灯光字段的规则校验和合并，取值范围和换类型时的换算由应用保证。
- **读**：不涉及存量，旧快照里没有面光源。
- **渲染指纹**：面光源随灯光对象计入 `lit` 指纹，物件缩放本来就在指纹里；`clay` 和 `line` 不计入。本条只新增类型，不改变已有文档的画面，不加光源模型版本号。
- **API**：`api: 提交场景命令` 的外形不变。OpenAPI 的 `CreativeSceneLight`、`CreativeSceneLightPatch` 的 `type` 枚举加 `area`，并加入上面的字段。

### 生命周期与演进
- 新增类型，不迁移。
- 回滚到不认识 `area` 的后端或前端时，含面光源的快照会读取失败，只做前向修复。
- 已知限制：点光和聚光的强度上限 1000 cd，而面光源 20 × 20 m 时等效可达 4×10⁷ cd，两者不对称。属性面板显示等效 cd 后，这个落差会很明显，留待以后修订灯光取值范围时一并处理。
- 以后再加的字段，例如 `spread` 和光源贴图，属于兼容扩展。

### 表达与样例自查
```json
{"op": "add_objects", "objects": [
  {"id": "sobj_2xHkQ9pL7mNcR4tYwB8sE", "kind": "light", "name": "柔光箱", "parent_id": null, "order": 4,
   "transform": {"position": [1.5, 1.8, 2], "rotation": [0, 0.3826834, 0, 0.9238795], "scale": [1, 1, 1]}, "visible": true, "locked": false,
   "light": {"type": "area", "intensity": 8000, "cast_shadow": true, "shape": "ellipse", "width": 0.9, "height": 0.9}}]}
```
存储后为 `{"type":"area","color":"#ffffff","intensity":8000,"cast_shadow":true,"shape":"ellipse","width":0.9,"height":0.9}`。法线方向的等效发光强度 ≈ 8000 × π/4 × 0.9 × 0.9 ≈ 5089 cd。

```json
{"op": "update_objects", "updates": [{"object_id": "sobj_2xHkQ9pL7mNcR4tYwB8sE", "light": {"type": "point"}}]}
```
存储后为 `{"type":"point","color":"#ffffff","intensity":1000,"cast_shadow":true,"distance":0,"decay":2,"radius_m":0.1}`：5089 cd 截断到上限 1000。

`{"type": "area", "decay": 2}` 返回 400，`field` 指向 `light.decay`。
