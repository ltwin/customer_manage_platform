# 组 1：接入

> 2026-10-23，S0 任务 2.2。结论：**有条件可行**。打包、求交、材质索引和确定性都达标。但项目代码要调用库的求交函数，必须写一个很小的 WGSL 包装函数，这与 `patterns: 场景着色写法` 现在的例外范围冲突，需要 owner 决定（见文末「需要决定」）。

## 做法

原型在 `prototype/spike/trace/integration.ts`（S0 期间在 `frontend/` 下，运行方法见 `prototype/README.md`），测量脚本是 `measure/integration.mjs`，数据在 `integration/`。

- **打包**：
  - 自己收集网格：可见、在第 0 层、不透明。
  - 用 `BVHComputeData` 的子类打包，属性为 `position`、`normal`（`vec4f`）。
  - 子类覆写 `writeTransformData`，把每个物件的材质索引写进变换槽的第一个对齐字（`TransformStruct` 第 33 个 u32，即 `_alignment0`）。
- **求交**：一个 TSL compute 内核，每像素发一条主光线。光线方向由相机的逆投影矩阵和世界矩阵（uniform）求出，通过 WGSL 包装函数 `spike_traceFirst` 调用库的 `bvh_RaycastFirstHit`。输出两样：每像素的（命中距离，世界空间几何法线），以及从变换槽读出的材质索引。
- **对照**：光栅用替身材质画两遍，读回后逐像素比较。
  - 两遍分别输出：几何法线，以及（物件 ID，到相机的距离）。几何法线用屏幕导数的叉积求得。
  - 判为不符的条件：命中与否不同、物件不同、距离相差超过 0.5%，或法线夹角超过 3°。
  - 排除边缘：相邻像素的物件、距离或面片法线发生变化的像素不参与比较。
- **确定性**：同一内核跑两次，读回结果逐字节比较。
- **分辨率**：1536 × 1024，共 157 万条光线。

## 结果

| 场景 | 三角形 | 打包 | GPU 时间（两次） | 参与比较的像素 | 不符 |
|---|---|---|---|---|---|
| `cornell` | 4 054 | 7–10 ms | 8.1 / 8.1 ms | 1 533 352 | 2 像素（距离 1，法线 1） |
| `materials` | 4 150 | 8 ms | 1.6–5.8 ms | 1 508 356 | 2 像素（法线） |
| `backlit-room` | 302 | 4–6 ms | 2.0–6.4 ms | 1 535 503 | 1 像素 |
| `thin-wall` | 122 | 6 ms | 1.4–6.1 ms | 1 554 570 | 0 |
| `mirror-board` | 4 222 | 9–12 ms | 1.2–5.0 ms | 1 530 298 | 0 |
| `noon` | 998 | 4–5 ms | 1.2–4.7 ms | 1 539 655 | 2 像素（法线） |

- **一致性**：所有场景的不符像素都不超过 0.0002%，远低于判定标准的 0.1%。
- **材质索引**：写进变换槽后，在着色器里按命中的 `objectIndex` 读回，与光栅的物件 ID 全部一致。上游预留的这个扩展点可以用。
- **确定性**：两次运行逐字节相同。
- **耗时**：
  - GPU 时间用时间戳查询测得，同一场景在不同次运行之间相差可达 3 倍。原因待查，可能是 GPU 的频率或功耗状态。组 2 的吞吐测量要多次重复并取中位数。
  - 首次调用的墙钟时间包含编译内核，为 0.03–2.9 s。
- **截图**：光线结果的法线图（`cornell-normals.png`、`mirror-board-normals.png`）与光栅画面一致；不符像素的分布见 `*-mask.png`，灰色是排除的边缘。

## 在 three r186 上从 TSL 调用库的求交函数

这是本组最重要的发现。按 `patterns: 场景着色写法` 的约定，项目代码只调用库导出的 TSL 函数节点，不自己写 WGSL。实测这在 r186 上做不到，依次遇到四个问题：

1. **库的签名丢了一个参数**：
   - `bvh_RaycastFirstHit` 的签名是 `fn …( shape: Ray, result: ptr<function, IntersectionResult> ) -> bool`。
   - `WGSLTagFnNode.getNodeFunction` 为 three 拼签名时，把除结构体以外的模板参数都换成了 `_argN`；第二个参数正好是一段内联代码，于是被换掉了。
   - three 因此只认出一个参数，TSL 调用时丢掉了结果指针（`WGSLTagFnNode.js:166-218`）。
   - 一个小补丁可以修好：拼签名时把内联代码也展开。原型里验证过这个补丁，后来用不上了，已删除。
2. **作用域冲突**：
   - three 默认把计算内核的变量声明在模块作用域（`var<private>`），只有用到 barrier 时才放进 `main`（`BarrierNode` 会关掉 `allowGlobalVariables`）。
   - 库的结果指针要求 `ptr<function>`，所以变量必须在函数里。
   - 但库自己有一个临时变量 `bvh_rayScalar_*`，依赖模块作用域。加了 barrier，它就被挪进 `main`，库的辅助函数找不到它。
   - 把调用包进带 `setLayout` 的 TSL 函数也不行：这个临时变量第一次在那个函数里被构建，就成了那个函数的局部变量。
   - 结论：在同一个内核里，纯 TSL 无法同时满足这两个要求。
3. **未使用的调用被剪掉**：在带 layout 的 TSL 函数里，`call(...).toVar()` 的结果如果之后没被用到，整个调用就被剪掉了。要用 `assign` 才能确保调用写进函数体。
4. **结构体必须给出全部成员**：构造 `StructNode` 时缺少的成员，three 会按常量生成，取到 null 就报错。

**可行的做法**：和上游示例 `example/webgpu_gpuPathTracingSimple.js` 一样，用一个 WGSL 包装函数调用库函数：

```wgsl
fn spike_traceFirst( origin: vec3f, direction: vec3f ) -> IntersectionResult {
  var ray: Ray; ray.origin = origin; ray.direction = direction; ray.maxDist = 0.0;
  var hit: IntersectionResult;
  bvh_RaycastFirstHit( ray, &hit );
  return hit;
}
```

- 包装函数里的 `hit` 是函数自己的局部变量；库的临时变量随内核声明在模块作用域。
- TSL 这边按值拿到结构体，可以直接读 `hit.get('didHit')` 这些字段。
- 内核其余部分（光线生成、写结果、读材质索引）仍然是 TSL。
- 用了包装函数之后，第 1 条的签名补丁就不再需要了：库函数是在 WGSL 文本里被调用的。

## 顺带发现（原型以外也适用）

- **读回时每行要按 256 字节对齐**：WebGPU 读回纹理时，每行字节数必须是 256 的倍数。`rgba32float` 时宽度要取 16 的倍数，否则 `readRenderTargetPixelsAsync` 读回的行会错位（1500 宽时，下半幅全错）。S1 的画质对照和 e2e 的读回要注意这一点。
- **项目的材质输出钳制会截掉负值**：`render/gpu.ts` 的 `clampMaterialOutput` 把所有节点材质的输出钳到 [0, 60000]。用材质输出法线这类有符号的数据时，要先偏移编码。

## 回答

- **接入可行性**：可行，条件是「需要决定」里的一项。打包、材质索引、确定性都满足 S1 的需要。
- **U9 的打包部分**：材质索引写进变换槽的对齐字段可行。增量打包（不整体重打包）属于组 2，在 `throughput.md` 里回答。

## 需要决定（交 owner，经 Epic 修订 v3 写入 `patterns: 场景着色写法`）

项目代码要调用 `three-mesh-bvh` 的求交函数，在 three r186 上至少需要一个 WGSL 包装函数。选项：

| 选项 | 做法 | 代价 | 推荐 |
|---|---|---|---|
| A | 允许 `render/trace/` 里有一个专门的包装模块：只包含对库函数的调用（构造参数、声明局部结果、按值返回），不写任何着色逻辑；静态检查对这个文件放行，其余文件仍禁止 WGSL | 例外范围从「依赖内部」扩大到一个包装文件，大约 20 行 | **推荐**，与 B 同时进行 |
| B | 向上游提交 PR：提供按值返回的 TSL 可调用版本（例如 `raycastFirstHitResult( ray ) -> IntersectionResult`），并修好签名拼接 | 等待上游合并和发版，时间不可控 | 与 A 一起做；上游发版后删掉包装模块 |
| C | 只用库的 CPU 端打包和缓冲布局，自己用 TSL 写遍历和三角形求交 | 约 1–2 天，还要自己维护遍历的性能，性能要另外比较 | 不推荐，除非 A 不被接受 |
