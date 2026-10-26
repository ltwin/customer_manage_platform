# 第 3 组：同一张阴影深度图上的比较采样与原始读取（U7）

> 状态：WebGPU 上 2D 和立方体贴图都有结论，WebGL2 待测。

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

待测（任务 3.2）。GLSL ES 3.0 不允许对 `sampler2DShadow` 使用 `texelFetch`，而 three 的 WebGL2 后端是否给 `textureLoad` 另外生成一个 `sampler2D` uniform，指向同一张纹理，还要实测。比较模式是纹理的状态，同一张纹理在一个着色器里既做比较又读原始值，结果没有定义（评审 B3）。如果不可行，就用 design 的备选：阴影 pass 额外输出一张线性深度。
