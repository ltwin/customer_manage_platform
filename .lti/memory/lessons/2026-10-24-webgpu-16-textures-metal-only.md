---
slug: webgpu-16-textures-metal-only
status: observed
scope: 3D 场景编辑器：WebGPU 全屏合成 pass、体积步进等采样多张纹理的着色
createdAt: 2026-10-24T10:00:13.918Z
source: 47e54703-f35f-43e1-a4a1-d9d19ff7d7bc
---
WebGPU 每个着色阶段最多采样 16 张纹理（缺省限额，gpu.ts 不调高），超过时管线建不起来，整个画面是黑的；SwiftShader（WebGL2）的上限更高，在它上面跑的 e2e 全绿也抓不到。W5 的实时 GI 视口：GI 合成、雾、空气透视、体积和辅助对象叠加在一个 pass 里共 17 张，视口全黑，导出（没有辅助对象，15 张）正常。加纹理输入时按最多的那条路径（视口，含辅助对象）数一遍，并在本机 Metal 上（SCENE_E2E_W5_METAL 的 w5Metal()）跑一次对应视口。three 按纹理的 uuid 共享绑定（TextureNode.getUniformHash），多个节点指向同一张已生成的纹理只算一张，但占位纹理各自不同。
