---
slug: three-info-memory-leak-trace
status: observed
scope: frontend/src/creative-scene/render
createdAt: 2026-10-27T17:36:50.394Z
source: 8a117f55-e43d-4600-8cbf-536f1b1334b5
---
排查视口 info.memory 的 renderTargets/textures 增长：在 e2e 页面里包住视口渲染器的 info.createTexture / info.destroyTexture（__sceneGPU.live 里 role=viewport 的 info），按纹理对象记录创建调用栈和所在步骤（window.__phase），再逐步删减操作，就能看到留下的是谁建的。直接 patch GPUDevice.prototype.createTexture 在 e2e 浏览器里抓不到 three 的调用，结果为空，不能当作「GPU 上没有泄漏」的证据。要验证修法而不改源码，可以从 performance resource 条目里找到 Vite 预打包的 three 模块 URL，import 后 patch 原型。three r186 已知漏释放：CSMShadowNode.dispose 不释放每级 ShadowNode（已在 render/sunShadow.ts 处理）。
