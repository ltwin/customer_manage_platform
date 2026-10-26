# 上游报告草稿（S1 任务 9.1）

> 2026-10-25。五份草稿，**未提交**：owner 过目同意后再发。发之前先在对应仓库搜一遍有没有已知的 issue，有就改为在原 issue 下补充。
>
> | # | 仓库 | 问题 | 项目里的绕法 | 上游修好后 |
> |---|---|---|---|---|
> | 1 | gkjohnson/three-mesh-bvh | 在 three r186 上，库的求交函数不能从 TSL 直接调用 | `render/trace/wgsl.ts` 的 WGSL 包装函数（`patterns` 的例外，owner 2026-10-23 批准） | 删掉包装模块，改用库的 TSL 函数 |
> | 2 | mrdoob/three.js | `ggxReflectionSample` 在法线正对摄像机时返回 NaN | `SSRTraceNode.js` 的 fork 里，法线与 z 轴几乎平行时先推开 1 毫弧度（design A8） | 去掉这一处 fork 块 |
> | 3 | mrdoob/three.js | TSL 的 `Switch` 各分支共享的表达式，在第一个分支里才被声明和赋值 | `hit.ts` 在 `Switch` 前把输入都存成变量 | 不必改，保留也无害 |
> | 4 | gkjohnson/three-mesh-bvh | 没有「找到任意交点就停」的遍历，阴影光线只能求最近交点 | 无。大厅里补追每帧多 7.5 ms，阴影光线约占 4.7 ms，owner 2026-10-25 接受（design S1-U4）。自己写要用 `wgslTagFn` 拼遍历，与 AC-S1-10 冲突 | 阴影光线改用库的任意交点查询，重测 AC-S1-7 |
> | 5 | mrdoob/three.js | `RecurrentDenoiseNode` 的 specular 模式对镜面也做空间降噪：半径有 √粗糙度 ≥ 0.01 的下限，预通道粗糙度又被 `getRoughness` 抬到 0.0525；光线长时，8 个取样点从几个像素外取到别的物体的颜色，旋转随 `frameId` 变 | `realtimeGI.ts` 在画这一层时，粗糙度 ≤ 0.06 的反射不用降噪结果（design A11） | 上游让镜面的半径趋于 0 后，去掉这段混合，重新对照 `hall-mirror-fix.png` |
>
> 版本：three 0.186.1，three-mesh-bvh 0.9.15，Playwright 自带的 Chromium（macOS，Metal）。第 1、3 条的现象来自 S0（`scene-trace-spike` 的 `integration.md`、`reflections.md`），第 2 条的数字来自 S0 的 `cornell`，第 4 条的数字来自 S1 任务 8.2（`evidence/after/trace-frames.json`）。

---

## 1. three-mesh-bvh：`bvh_RaycastFirstHit` cannot be called from TSL on three r186

**Title:** WebGPU: the WGSL raycast functions can't be called from a TSL compute/fragment node on three r186

**Body:**

Versions: three-mesh-bvh 0.9.15, three 0.186.1, WebGPURenderer (WebGPU backend).

We build a `BVHComputeData` and want to call its `bvh_RaycastFirstHit` from TSL (a fragment node and a compute kernel) without writing WGSL of our own. On r186 we could not; we hit these in turn:

1. **The signature three reads loses the result pointer.** `bvh_RaycastFirstHit` is `fn ( shape: Ray, result: ptr<function, IntersectionResult> ) -> bool`. When three builds the function's signature (`WGSLTagFnNode.getNodeFunction`, `WGSLTagFnNode.js` around lines 166–218), template parameters other than structs become `_argN`; the second parameter is an inline code piece and is replaced, so three sees one parameter and a TSL call drops the pointer. (A small patch that expands inline code when building the signature fixes it; this part may belong to three rather than here.)
2. **The result needs function scope, the scratch variable needs module scope.** The result must be a `ptr<function>`, so it has to be declared inside a function. three declares a compute kernel's variables at module scope (`var<private>`) unless the kernel uses a barrier (`BarrierNode` turns `allowGlobalVariables` off). But the library's scratch variable `bvh_rayScalar_*` relies on module scope: with a barrier it moves into `main` and the helper functions no longer see it. Wrapping the call in a TSL `Fn` with `setLayout` does not help either: the scratch variable is first built inside that function and becomes its local.
3. In a TSL function with a layout, `call( ... ).toVar()` is pruned when the variable is not read later; only an `assign` keeps the call.
4. Building the `Ray` as a `StructNode` needs every member given; a missing one is generated as a constant and fails on null.

What works is what `example/webgpu_gpuPathTracingSimple.js` does, a small WGSL function that holds the result:

```wgsl
fn traceFirst( origin: vec3f, direction: vec3f ) -> IntersectionResult {
  var ray: Ray;
  ray.origin = origin;
  ray.direction = direction;
  ray.maxDist = 0.0;
  var hit: IntersectionResult;
  bvh_RaycastFirstHit( ray, &hit );
  return hit;
}
```

called from TSL with `wgslFn( code, [ bvhData.fns.raycastFirstHit ] )`, its result read with `.toVar().get( 'didHit' )` and so on.

**Suggestion:** a by-value variant TSL can call directly, for example `raycastFirstHitResult( ray: Ray ) -> IntersectionResult` (or taking origin, direction and maxDist), generated next to the pointer version. Projects that keep shading in TSL could then drop their own WGSL. Happy to send a PR if the shape sounds right.

---

## 2. three.js：`ggxReflectionSample` returns NaN when the normal faces the camera

**Title:** SpecularHelpers: ggxReflectionSample returns NaN for a view-space normal of (0, 0, 1)

**Body:**

Version: three 0.186.1 (`examples/jsm/tsl/utils/SpecularHelpers.js`, used by `SSRNode`).

```js
const up = vec3( 0, 0, 1 );
let T = cross( up, N ).toVar();
T = T.normalize().toVar();
If( T.length().lessThan( 1e-3 ), () => {
	T.assign( cross( vec3( 0, 1, 0 ), N ).normalize() );
} );
```

The tangent is normalised before its length is checked. When the view-space normal is exactly `(0, 0, 1)`, a surface facing the camera, `cross( up, N )` is zero, `normalize` gives NaN, `NaN < 1e-3` is false, and the fallback never runs. The sampled direction is NaN.

How often: with 8-bit normals from a prepass, many pixels quantise to exactly `(0, 0, 1)`. In a Cornell box seen head on, 2.5% of the SSR pixels (the back wall) had a NaN direction; their SSR fails and falls back to the environment. Anything that traces such a direction further (we trace missed SSR rays through a BVH) walks the whole tree: 115 ms a frame against 9.6 ms with the fix.

**Fix:** check the length before normalising:

```js
const T = cross( up, N ).toVar();
If( T.length().lessThan( 1e-3 ), () => {
	T.assign( cross( vec3( 0, 1, 0 ), N ) );
} );
T.assign( T.normalize() );
```

(Our fork works around it by nudging such a normal by a milliradian before sampling; an isotropic GGX lobe does not depend on the tangent frame's turn about the normal, so either way gives the same distribution.)

---

## 3. three.js：an expression shared by `Switch` cases is declared inside the first case

**Title:** TSL: a node shared by several Switch cases is declared and assigned in the first case only; the other cases read it unassigned

**Body:**

Version: three 0.186.1, WebGPU backend.

```js
const n = normalize( someInput ); // used by more than one case
const out = vec3( 0 ).toVar();
Switch( selector )
	.Case( 0, () => { out.assign( n.mul( 2 ) ); } )
	.Case( 1, () => { out.assign( n.add( 1 ) ); } );
```

When `n` is used more than once, the builder makes it a variable where it is first built, which is inside case 0's block. Case 1 reads the variable but nothing assigns it on that path, so it reads zero (in our case one of 14 material cases got a NaN normal and drew black). The same probably holds for `If` / `Else` branches.

Workaround: `.toVar()` every shared input before the `Switch`.

Expected: either the shared expression is hoisted to the enclosing scope, or each branch builds its own copy. If this is by design, a note in the TSL docs on `Switch` / `If` would help.

---

## 4. three-mesh-bvh：an any-hit query for shadow rays

**Title:** WebGPU: an any-hit (occlusion) raycast that returns at the first triangle within maxDist

**Body:**

Versions: three-mesh-bvh 0.9.15, three 0.186.1, WebGPURenderer (WebGPU backend).

We trace shadow rays through a `BVHComputeData` (a top level over many instances, each with its own tree) to shade reflection hits: one ray from the hit towards each light, `maxDist` the distance to the light. A shadow ray only needs to know whether anything is in the way, but the only query is `raycastFirstHit`, which keeps traversing after a hit to find the nearest one (`bvhIntersectFirstHit` prunes by `bestHit.dist`, it does not stop). `getShapecastFn` does not stop early either: each leaf's result is OR-ed into `didHit` and the loop goes on.

In a stress scene (20 M triangles in about 2000 instances, one directional light) the shadow rays are about 60 % of what our reflection retrace adds to a frame (4.7 ms of 7.5 ms on an Apple M4 Pro, 1536 × 1024).

**Suggestion:** an any-hit variant generated next to `raycastFirstHit`, for example `raycastAnyHit( ray: Ray ) -> bool`, that returns `true` from both levels of the traversal as soon as a triangle is hit within `ray.maxDist`, skipping the barycentrics and the nearest-hit bookkeeping. Alternatively an option on `getShapecastFn` to stop as soon as `intersectRangeFn` returns `true`. Either can be called through the same small WGSL wrapper as the first-hit query (issue 1), so projects that avoid `wgslTagFn` of their own could use it. Happy to measure a prototype or send a PR.

---

## 5. three.js：the specular RecurrentDenoiseNode blurs mirror reflections

**Title:** RecurrentDenoiseNode (specular): mirror reflections are filtered in space, picking up neighbouring objects' colours

**Body:**

Version: three 0.186.1 (`examples/jsm/tsl/display/RecurrentDenoiseNode.js`), with `SSRNode` (stochastic) feeding it through `TemporalReprojectNode`, as the SSR example chain does.

In `'specular'` mode the kernel's world radius is

```js
worldRadius = radius * WORLD_RADIUS_SCALE;           // 5 * 0.1
worldRadius *= rl * abs( viewPosition.z );           // ray-length factor
worldRadius *= roughness.sqrt().max( 0.01 );         // floor for mirrors
worldRadius *= mix( 1, AGGRESSIVITY_RADIUS_MIN, aggressivity );
```

and the roughness it reads from the prepass has already been through `getRoughness`, which floors it at 0.0525. So a perfect mirror (material roughness 0) is filtered with `sqrt( 0.0525 ) ≈ 0.23` of the radius. Its edge stopping is by depth and normal, which a flat mirror shares across its whole face, so the 8 Vogel taps take colours from other reflected objects several pixels away. With long rays (we trace rays the screen missed through a BVH, so `rl` is near 1 for most of a mirror) this shows as grainy speckles of neighbouring colours over high-contrast reflected detail. Because the taps' rotation comes from `frame.frameId`, two renders of the same scene get different speckles.

A mirror's lobe has no stochastic noise to remove, so filtering it only loses detail. Workaround: we blend back to the unfiltered reflection below a roughness of 0.06 (0.12 fully filtered).

**Suggestion:** let the specular radius go to zero for mirror-like roughness (e.g. drop the 0.01 floor and map the prepass roughness back past `getRoughness`'s floor), or expose a roughness below which the spatial pass is skipped. We can share before/after crops.
