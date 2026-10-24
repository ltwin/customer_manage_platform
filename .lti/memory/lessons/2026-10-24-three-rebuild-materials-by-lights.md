---
slug: three-rebuild-materials-by-lights
status: observed
scope: 3D 场景编辑器：需要让 three 重新构建一个场景的受光材质（按场景状态切换着色代码）
createdAt: 2026-10-24T10:00:14.039Z
source: 47e54703-f35f-43e1-a4a1-d9d19ff7d7bc
---
three r186 的 WebGPU 渲染器只在渲染对象的缓存键变化时重建材质；material.needsUpdate 在缓存键不变时只更新版本号、不重建。动态缓存键里能按场景区分的是灯的组合（LightsNode.customCacheKey：灯的 id 与 castShadow）。W5 的做法：需要时往场景里挂一盏强度为 0、带 userData 标记的 AmbientLight，阴影节点在 setup 时看 builder.lightsNode.getLights() 里有没有它；它走 SceneLights 的预热，替身灯复制可见灯时也带上，预编译与真实帧生成同样的代码。renderer.contextNode 的版本也在缓存键里，但 RenderPipeline 渲染时会换成派生的上下文节点，不可靠。另：CSMShadowNode 把 uniform 固定命名为 cascades、shadowFar，两盏以上投影平行光进同一材质时 WGSL 重复声明（sunShadow.ts ownNames 已处理）。
