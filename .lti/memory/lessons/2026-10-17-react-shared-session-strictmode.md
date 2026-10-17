---
slug: react-shared-session-strictmode
status: observed
scope: 前端：被多个组件共用、按引用计数释放的会话 / 订阅 / 撤销栈，以及浏览器 e2e 夹具
createdAt: 2026-10-17T10:29:04.659Z
source: 32c90485-2ed7-497a-9411-b09fb0608521
---
开发模式的 StrictMode 会在挂载后多跑一轮 effect 清理。如果资源在 useMemo 或渲染里 attach、在清理里 detach，模拟卸载时引用数归零，资源被立刻销毁；之后的编辑只在本地显示、不再发送，生产构建却正常。做法：attach / detach 成对写在同一个 effect 里，引用归零后推迟一拍再判定无人使用。e2e 夹具要和 main.tsx 一样用 StrictMode 渲染，否则测不出这类问题（C1 owner 测试时「新建方块退出再进来就没了」即此根因）。
