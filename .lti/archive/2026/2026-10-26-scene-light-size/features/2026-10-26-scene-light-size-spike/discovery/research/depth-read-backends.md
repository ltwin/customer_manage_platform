# 第 3 组：同一张阴影深度图上的比较采样与原始读取（U7）

> 状态：完成。WebGPU 和 WebGL2 上，2D 和立方体贴图都有结论。

PCSS 先搜索遮挡物，这一步要读原始深度；后面的滤波用比较采样。three r186 给阴影深度纹理设了 `compareFunction`（`ShadowNode.js:338`、`PointShadowNode.js:225`），问题是同一个着色器里能不能两者都做。

## WebGPU（参考设备，three r186）

结论：**有条件可行**。

1. **three 按纹理生成 sampler，而不是按用法**：在同一个着色器里，three 给一张纹理只生成一个 sampler 声明。它是普通 `sampler` 还是 `sampler_comparison`，取决于这张纹理第一次被采样时的用法。
   - 先原始采样、后比较采样：`textureSampleCompare` 拿到的是普通 sampler，WGSL 编译失败，报错为 `no matching call to 'textureSampleCompare(texture_depth_2d, sampler, …)'`，管线创建失败，画面没有这部分着色。
   - 遮挡物搜索改用 `textureLoad`（TSL 的 `textureLoad`，即 `texture().setSampler(false)`），不需要 sampler，结果仍然报同样的错：sampler 声明还是按第一个纹理节点生成的。
2. **可行的写法**：在用到这张深度图的所有地方之前，先做一次比较采样，并用 `toVar()` 固定它的位置，例如对接收点中心做一次比较。这样声明出来的是 `sampler_comparison`；遮挡物搜索用 `textureLoad` 按整数 texel 读原始深度，不需要 sampler，就能编译通过，滤波照常使用硬件比较和它的 2×2 双线性。
   - `textureLoad` 的坐标取 `floor(uv · mapSize)`，方向与 uv 相同，不需要翻转 v。按原始采样逐点核对过：翻转 v 的读法读到的是别的位置，用 `uv · (size − 1)` 取整，在深度变化处会差一个 texel。
3. **备选写法**：全部改用原始采样，滤波时手动比较深度，可以编译，但没有硬件双线性，边缘更粗糙。
4. **待办**：比较采样在先、`textureLoad` 在后，依赖 three 生成代码的顺序。S1 要把这个约束写进代码注释，并用 e2e 防止回退：着色器编译失败时，画面上这一处会直接缺失（lesson `webgpu-detect-shader-failure`）。

### 立方体贴图（点光）

结论：**只能全部用原始采样、手动比较**，没有硬件比较的双线性。

1. WGSL 没有针对 `texture_depth_cube` 的 `textureLoad`，2D 上「先比较采样，再用 `textureLoad`」的写法走不通。
2. three 在一个按像素分叉的 `If` 里调用点光的滤波函数（`PointShadowNode.js:111`），因此不能用依赖导数的 `textureSample`，只能用 `textureSampleLevel`。
3. **three r186 的缺陷**：对可过滤的深度立方体贴图调用 `.level()` 时，three 把 level 当 float 输出（`WGSLNodeBuilder.generateTextureLevel`），而 WGSL 要求整数，编译失败。只有不可过滤的贴图才走输出 `u32` 的那条路径（`generateTextureLod`）。
   - `setSampler(false)` 也不行：生成的是 `textureLoad(texture_depth_cube, vec3<i32>, u32)`，同样不存在。
   - 原型的绕法：`level(expression('0u', 'float'))`。表达式声明为 float，three 就不再包一层转换，实际输出整数。S1 要么沿用这个写法并加注释和 e2e，要么给 three 提补丁。
   - 编译失败时，页面上这盏灯的着色整个缺失，原型页会一直等不到完成（lesson `webgpu-detect-shader-failure`）。
4. 效果见 `soft-shadow-candidates.md`「点光」：在 occluder-wall 上，手动比较的效果与参考解吻合，看不到立方体面接缝。

## WebGL2

参考设备上 Chromium 的 WebGL2（ANGLE，Metal），原型页加 `?backend=webgl`。

1. **比较采样加 `textureLoad` 不可行**：three 给一张纹理只声明一个 uniform，深度图设了 `compareFunction` 就声明为 `sampler2DShadow`，GLSL ES 3.0 不允许对它用 `texelFetch`，着色器编译失败（评审 B3 的预判）。
   - 失败是静默的：页面照常报告完成，画面上这部分着色缺失。
2. **全部原始读取、手动比较可行**，但要同时满足两点：
   - 去掉深度图的 `compareFunction`，否则 GL 纹理停在比较模式，用普通采样器读是未定义的。three 的 WebGL 后端只在 `compareFunction` 非空时设置比较模式，设成 null 并不会改回 NONE，所以要在纹理建好之前设。原型在滤波函数构建时设，早于第一次绘制阴影，有效。
   - 改用最近点过滤。PCF 模式下 three 把阴影深度图设成线性过滤（`ShadowNode.js:368`）；没有比较模式的深度纹理在 GLES 3.0 里不能线性过滤，纹理变得不完整，每次读都返回 0，画面全黑。
3. **立方体贴图**：GLSL 允许在分支里用隐式 level 的 `texture()`。three 给深度纹理生成的 `textureLod` 漏掉了普通 `texture()` 会追加的 `.x`（`GLSLNodeBuilder.generateTexture` 对比 `generateTextureLevel`），返回的 vec4 被赋给 float，编译失败。所以 WebGL 上用 `texture()`，WebGPU 上用 `level(expression('0u', 'float'))`，两边各有一个 three 的缺陷要绕。
4. **结果**：三种灯在 WebGL2 上的成片与 WebGPU 逐像素对比，平均差 0.00–0.04 级，p99 不超过 1 级。

## 建议（U7）

**两个后端统一用一套写法**：深度图去掉比较、改为最近点过滤，所有读取都用原始值、手动比较。

- 画质：WebGPU 上与「比较采样加 `textureLoad`」相比，误差均值、半影宽度、剖面误差都在测量噪声以内。例如聚光半径 0.3 m 时，3 m 板剖面平均差都是 0.010，最大差 0.080 对 0.079。失去硬件比较的双线性，在 32 个滤波样本加逐帧旋转下看不出来。
- 好处：不再依赖「比较采样必须写在最前面」这种代码生成顺序；2D 和立方体贴图同一种写法；WebGL2 只要多绕一个 three 的缺陷。
- 代价：带尺寸的灯，阴影图不能再给硬件 PCF 用。尺寸为 0 的路径也要改成手动比较，无法逐像素复现原来的硬件双线性。这与 Epic「尺寸为 0 与基线差异不超过 1/255」的要求冲突，留到第 4 组（`integration.md`）给出处理方案。
- S1 的防回退：WebGL2 和 WebGPU 各有一个 e2e，检查带尺寸的灯所在之处有正确的着色，而不是缺失或全黑（lesson `webgpu-detect-shader-failure`）。
- 原型参数：WebGL 后端自动走这条路径；WebGPU 上用 `?compare=off` 开启。
