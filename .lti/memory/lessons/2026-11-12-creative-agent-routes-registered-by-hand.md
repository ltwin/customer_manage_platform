---
slug: creative-agent-routes-registered-by-hand
status: observed
scope: backend/internal/platform/httpapi
createdAt: 2026-11-12T03:27:07.785Z
source: 81e65423-dd89-46de-b95d-43333b1e7b97
---
创作助手接口不走生成代码的 RegisterHandlers，而是在 creative_agent.go 的 registerCreativeAgent 里逐条手写注册。OpenAPI 新增 operation 后，除了实现 handler，还要在这里加一行路由，否则真实服务落到 NoRoute，返回 404 not_found「资源不存在」。冒烟测试不要只断言 404 not_found（未注册路由也是这个结果），要断言服务层自己的文案或成功状态。e2e 用模拟接口，覆盖不到这一层。
