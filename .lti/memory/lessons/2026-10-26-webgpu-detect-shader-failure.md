---
slug: webgpu-detect-shader-failure
status: observed
scope: 3D 场景编辑器：WebGPU 下检测某个材质的着色器或管线建失败，并据此降级
createdAt: 2026-10-26T08:12:44.709Z
source: 679ca314-5a9a-49ef-bb72-691f46cd716b
---
不要包住 createShaderModule 再调 getCompilationInfo 提前发现编译失败：S1 试过，页面卡死或崩溃（three 自己报诊断时也会调它，见 design A13 ④）。可行的做法：包住后端的 createRenderPipeline，记下要盯的材质（userData.watchPipeline）对应的管线，画完一帧查 three 的 pipelineData.error，失败就退回原有路径、不重试；three 的 backend.draw 遇到出错的管线会直接跳过，所以失败只坏一帧。WGSL 解析错误是未捕获的设备错误，Dawn 只部分压制后续的「Invalid X」，用设备错误监听按缓冲名前缀（S1 是 trace_）认领。验证：往着色器里临时注入一段编不过的 WGSL，确认只坏一帧、之后的帧没有报错，验证完用 grep 确认已撤掉。
