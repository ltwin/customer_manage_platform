---
slug: three-storage-buffer-release
status: observed
scope: 3D 场景编辑器：用 three r186 的存储缓冲（storage / instancedArray）做大块 GPU 数据，释放 CPU
  数组或 GPU 副本时
createdAt: 2026-10-26T08:12:44.441Z
source: 679ca314-5a9a-49ef-bb72-691f46cd716b
---
两个坑（S1 design A5、A13）。一是 CPU 侧数组不能换成空数组：three 在给后面的图建管线时，按数组长度判断存储缓冲是不是结构体数组（isCustomStruct），空数组被当成单个结构体，WGSL 编译失败。S1 的离屏底座释放数组时换成 2 个元素的数组，字节数在分配时记下，不再读数组长度。二是 BufferAttribute.dispose() 对存储缓冲不起作用，只派发事件；要真正释放 GPU 缓冲得经渲染器的属性表（renderer._attributes.delete(attr)，它调 backend.destroyAttribute），项目里封装在 SceneGPU.release。存储节点换上新缓冲之后才能释放旧的；整份数据被丢掉的那一帧可能还要画它，延到下一帧再释放。JS 堆的读数量不到 GPU 缓冲是否释放，验证要靠单测断言 release 的调用时机。
