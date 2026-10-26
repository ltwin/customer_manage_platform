---
slug: scene-render-nan-block-diagnosis
status: observed
scope: 3D 场景编辑器：画面出现纯白或纯黑的成块区域，怀疑着色器算出 NaN 或溢出时的排查
createdAt: 2026-10-26T08:00:27.172Z
source: develop f3c4807f（owner 2026-10-26，体积雾纯白）
---
先量像素：全是 255（或 0）、几乎没有过渡像素，就是坏值，不是太亮；真被照亮会有渐变，边缘也会变暗。WebGPU 在 Metal 上 min(NaN, x) 返回 x，所以 NaN 会被 6e4 之类的上限截成纯白。再看边缘的台阶：每级是 2 个 CSS 像素，说明出在半分辨率的步进；1 像素的锯齿说明出在全分辨率的合成。复现要用 owner 的场景：场景快照在 creative_content_revisions.payload->'scene'，文档经 creative_nodes.document_id 找到，只读事务里取（连接串取自后端进程环境，要 owner 同意）。去掉模型实例后放进 e2e 的 SceneServer，用编辑器视角截图；场景相机的位置不一定能看到问题。然后逐项关掉灯、阴影、天空光二分，定位到一盏灯或一类阴影后，在可疑节点里加由页面全局变量切换的替换实验（常数、去掉条件、换掉比较深度）。最后用 addInitScript 包住 GPUDevice.prototype.createShaderModule，把含目标纹理类型的 WGSL 抓出来，看 TSL 实际生成了什么分支。实验在另开的 worktree 和端口上做：S1 工作区一改源码，owner 正开着的 Vite 页面会跟着热更新。
