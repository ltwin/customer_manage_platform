# 创意空间 FND-01：开发底座

本页说明已落地的底座及验证范围。正式页面与生成入口尚未接入；文字/链接应用接口已实现，见[文字与链接应用接口](creative-text-canvas.md)。`GET /api/v1/creative/capabilities`在认证后如实返回`foundation_only`、空节点/工具列表。账号能力新表默认全部关闭；不通过开发页绕过认证或写业务数据。

## 本次代码归属

- `platform/creativeops`：UUID操作身份、字符串BIGINT版本、严格命令解码、规范化请求hash、同账号同operation串行回执、90天恢复窗口、Eino具名工具适配。未来HTTP/Agent/Worker都复用该执行入口；完整Harness的epoch/step/预算仍在FND-07/08实现。
- `platform/store`：沿用AccountScope与账号writer屏障，0037只新增能力与回执两张显式key表，无外键。事务内入队使用封闭Jobs能力，不导出裸pgx.Tx。
- `platform/jobs`与`cmd/creative-worker`：版本化任务、注册处理器、独立进程的启动/停止及River迁移检查。任务账号来自受信外层，实际效果仍重查账号/能力并使用稳定operation幂等。
- `frontend/src/creative-canvas/lab`：React Flow受控view-model与Zustand验证页；不承诺业务保存，不作为生产API DTO。正式内容/节点保存属于FND-02起的后续交付。

## 锁定依赖

| 依赖 | 当前版本 / 边界 |
|---|---|
| Go | 1.25.5，保持项目工具链 |
| River / riverpgxv5 | v0.40.0；新版v0.47.0要求Go1.26，不在本切片升级Go |
| Eino | v0.9.19；正式go.mod内编译ADK及工具接口，原隔离探针继续保留 |
| React Flow | @xyflow/react 12.11.6 |
| Zustand | 5.0.15 |
| React / Vite | 沿用项目lockfile；当前安装React19.2.7、Vite8.1.3 |

参考：[River事务插入](https://riverqueue.com/docs/transactional-enqueueing)、[React Flow子流程](https://reactflow.dev/examples/grouping/sub-flows)。Context7曾返回旧`react-flow-renderer`示例；实际实现使用已安装`@xyflow/react`类型和源码核对，未照搬旧包名。

## 前后端分开运行

沿用项目现有API启动方式与DATABASE_URL，Vite的`/api`继续代理`localhost:8080`。本次没有连接或迁移本地真实业务数据库；0037会在明确运行项目迁移时应用。

```sh
# 工作树根目录，前端
cd frontend
npm ci
npm run dev -- --host 127.0.0.1 --port 5174 --strictPort
```

打开 `http://127.0.0.1:5174/creative-lab.html`。5174用于独立验证，避免占用原5173/8080/8770服务。只有Vite开发入口提供此HTML，正式构建仅包含原index入口。生成、素材库、Prompt与版本业务还未接入，不能把本地样例当正式创意项目。

```sh
# backend目录；DATABASE_URL由环境配置，示例不含凭证
# 先用项目已有迁移命令建业务schema，再显式维护River自己的schema：
go run ./cmd/creative-worker -migrate
go run ./cmd/creative-worker -check
```

River表在`creative_jobs` schema，由库自己管理版本；不混入应用0037，不把River表改成业务外键范式。迁移用独立会话锁串行，由River逐步提交：跨多个迁移的大事务会导致PostgreSQL拒绝读取刚添加的枚举值。

当前生产处理器目录为空，直接启动worker会明确报`no production creative workers are registered`；不注册假生成/假成功任务。FND-05/07/13加入真实处理器后，默认启动路径使用同一runtime并响应SIGINT/SIGTERM。测试里已运行真实River Worker和受信测试处理器，证明至少一次投递不会重复领域效果。

## 验证命令与边界

```sh
make check-go
make check-frontend
make generate-check
# 需要上面的Vite验证服务
node frontend/scripts/creative-canvas-foundation.e2e.mjs
# 原Eino隔离验证，不调用真实供应商
python3 docs/product/creative-canvas-system/probes/eino/run.py
```

回执测试使用标准storetest隔离PostgreSQL：同key并发、异hash、跨账号、禁用后重放/拒绝新写、回滚/过期、Eino与直接调用共享结果；网络代理在真正COMMIT后丢确认包，验证返回unknown而非假成功，再查原回执不重复效果。队列测试核对业务行与job行的xmin相同、整体回滚，以及两次投递一次效果。

浏览器验收覆盖框选/平移/父子移动/工具条/固定Handle、200节点300边、窄屏与无业务请求。页面可采样10分钟并下载机器/浏览器/视口/帧间隔记录；自动化只做3秒短样本，不能宣称已完成FND-12性能门槛。默认结果放`/tmp/creative-foundation-qa`，可由CREATIVE_QA_OUTPUT_DIR指定输出目录。

旧迁移测试的down清单随0037增加一步，目标迁移的行为断言不变；认证readiness检查当前迁移顶点，不再硬编码36。npm audit现有7项high的包版本与本次修改前一致，不由新React Flow/Zustand引入；未自动执行跨范围升级。

代码审查补强：所有对象递归拒绝重复字段及大小写折叠别名（含Unicode等价），限制64层嵌套，hash/Validate/Apply共享规范化输入；回执HTTP投影保留json.Number。Worker检查使用River同schema的完整迁移Validate，未迁移/仅部分迁移均拒绝就绪。这三项都有先失败后通过的回归测试。

画布滚轮规则：默认上下平移，按住Ctrl（Windows）或Command（Mac）再滚动才缩放；松开后恢复平移。左键框选、中键/空格拖动平移保持原行为，浏览器验证覆盖两种修饰键。

FND-02 的正式文字/链接创作入口与持久化保存流程见 [文字创作闭环](creative-text-canvas.md)。该页面对正常账号直接开放；本基础验证页仍用于交互探针，不代表后续媒体或 Agent 已可用。
