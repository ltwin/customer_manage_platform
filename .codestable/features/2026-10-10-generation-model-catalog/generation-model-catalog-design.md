---
doc_type: feature-design
feature: 2026-10-10-generation-model-catalog
requirement: 无 V6 清单条目（体验优化：打开模型下拉要等待）；影响 V6-021/023 的生成面板，为 V6-022 价格投影铺路
status: approved
date: 2026-10-10
---

# 生成模型目录改为账号级：进画布预取，选节点不再等待

## 0. 结论

- **问题**：现在每选中一个节点，提示词面板都会重新请求一次这个节点的模型目录，还要等作品版本列表一起返回才显示模型。在这之前点开模型下拉，只能看到「正在读取模型目录…」。后端为了确认节点类型，每次还要把整张画布读出来。
- **做法**：
  - 新增账号级接口 `GET /creative/generation/catalog`，一次返回本账号可用的全部生成模型，和已有的 `/creative/agent/catalog` 同级。
  - 前端在整个工作区共享一份目录：进入画布时预取，选中节点时按节点类型在本地筛选。切回页面，或者生成提交被服务端明确拒绝时（由队列记下错误码来触发），在后台刷新。
  - 节点层面的不可用原因（节点版本、画布已归档、这种节点没有模型）都改由前端根据手上已有的数据判断。提交时服务端的检查一条不变。
- **效果**：除了刚进画布的那一次请求，之后选节点、点开模型下拉都不再发请求，也不用等。画布卡片显示模型名也改用这份目录，顺带修掉一个潜在的显示错误（§1.2）。
- **思路来源**：同类项目 buzzy（`super-buzzy/apps/agent-app`）的模型列表就是全局一份、进画布预取、前端按输出类型分组，这次借鉴了这一点。它有两个缺点没有照搬：读过一次后再也不刷新，以及模型列表要等另一个请求一起返回（§5）。

本期不做：

- 模型价格投影，属于 V6-022。
- 参考素材上限和连线时的检查（§5.2）。
- 模型配置改成运行时可调。
- 作品版本列表的读取优化。

## 1. 现状

### 1.1 读取路径

| 关注点 | 现在的做法 | 代码位置 |
|---|---|---|
| 接口 | `GET /creative/canvases/{id}/nodes/{node_id}/generation-catalog`，按节点返回模型和 `disabled_reason` | `api/openapi.yaml` 的 `getCreativeNodeGenerationCatalog` |
| 后端 | `Product.Catalog` 先用 `GetCanvas` 读出整张画布找到这个节点，再依次判断节点版本、是否归档、账号能力，最后按 `"core."+kind == node.TypeKey` 筛出模型 | `creativegeneration/product.go:82` |
| 面板挂载 | 提示词面板的 key 是 `${canvasID}:${node.id}`，每换一个节点就重新挂载，目录状态清空 | `WorkspacePage.tsx:1647` |
| 面板读取 | 用 `Promise.all` 同时请求目录和 `versions`，两个都返回才 `setCatalog`；依赖里有 `canvas.revision`，画布一改动就重读 | `NodeGenerationPanel.tsx:193` |
| 卡片模型名 | 找到第一个「单版本、有模型 key」的节点，读它的目录拿模型名 | `CanvasView.tsx:1074` |
| 助手工具 | 直接用 `Product.Models()`，不经过 HTTP 目录 | `creativeagent/canvas_tools.go:62` |

目录的内容其实只取决于三件事：账号能不能用、节点类型、画布是否归档。同一张画布上同类型的节点拿到的结果完全一样，按节点逐个请求是多余的。

### 1.2 卡片模型名的潜在错误

后端按节点类型筛模型，所以 `CanvasView` 借某一个节点读到的目录只含这一种类型的模型。如果第一个满足条件的是图片节点，视频节点卡片上的模型就找不到名称，只能显示原始 key。现在只接了 GPT Image 三个图片模型，所以还没暴露出来；接入视频或音频模型后就会出现。

### 1.3 契约出处

模块文档 `docs/product/creative-canvas-system/modules/tool-generation-runtime.md` §25.1 写着「新增节点专属 `generation-catalog` 查询」。这次改成账号级，要在同一个提交里改这一句（D3）。

## 2. 契约

### 2.1 新接口

```yaml
/creative/generation/catalog:
  get:
    tags: [creative-foundation]
    operationId: getCreativeGenerationCatalog
    summary: 本账号可用的全部生成模型；节点可用与否由客户端按节点类型、版本和画布状态判断，提交时服务端仍会复核
    responses:
      '200': {$ref: CreativeGenerationCatalog}   # 结构不变
      '401': ...
```

- **结构沿用 `CreativeGenerationCatalog`**，即 `models` 和 `disabled_reason`。`models` 包含所有类型的模型，前端按 `kind` 筛选；每个模型已经带有 `kind`、`parameters`、`configuration_revision` 和 `description`。
- **`disabled_reason` 只表达账号级原因**：
  - 账号不可用时为「账号当前不可用」，`models` 为空。
  - 部署里一个生成模型都没注册时为「尚未配置生成模型」。
  - 正常时为空字符串。
- **仍然 `no-store`**，和其他账号数据一致。缓存放在前端内存里，不靠 HTTP 缓存。理由是模型配置随发版变化，HTTP 缓存会让发版后的第一次读取拿到旧配置，而内存缓存刷新页面就清掉了。
- **不加参数**：不按节点或类型过滤。目录很小（现在 3 个模型，可预见的规模也在几十个以内），一次返回全部更简单。

### 2.2 旧接口

删除 `GET /creative/canvases/{id}/nodes/{node_id}/generation-catalog`（D1）。它的消费方只有前端两处和 10 个 e2e mock，都在这次改动范围内。

### 2.3 节点级不可用原因：搬到前端

| 条件 | 文案（与后端现有文案一致） | 前端的数据来源 |
|---|---|---|
| `node.metadata.type_version !== 1` | 此节点版本暂不支持生成 | 节点元数据 |
| `canvas.archived` | 项目已归档，恢复后可生成 | 画布视图 |
| 目录里没有 `kind` 与节点类型匹配的模型 | 此节点类型尚未配置生成模型，仍可保存提示词和查看作品版本 | 账号级目录 |

判断顺序和现在的 `Product.Catalog` 一样：先看账号级 `disabled_reason`，再按上表从上到下。

**这只是展示，不是安全边界**。提交生成时服务端的检查都还在：

- `Product.Request` 会核对草稿里的 `model_key` 是否已注册、`configuration_revision` 是否一致。
- 账号能力由 `creativeops.Executor` 按 `node_generate` 检查。
- 画布是否可写、节点版本和节点类型由提交链路检查（`creativecanvas/generation.go:36-51` 的 `lockCanvas` 和 `TypeVersion`/`TypeKey` 判断）。

所以前端即使判断错了，最坏结果也只是提交被拒，然后弹出现有的错误提示。

## 3. 后端

- `Product.Catalog(ctx, scope)` 去掉 `canvasID` 和 `nodeID` 两个参数，不再读画布，只做三件事：
  1. 调 `scope.CreativeCapabilities` 判断账号能否使用。
  2. 判断生成运行时是否就绪：沿用现有的 `p.runtime != nil && p.runtime.queue != nil` 检查（`product.go:114`）。没就绪就返回空目录和「尚未配置生成模型」。可以直接调用已经带这个检查的 `p.Models()`（`agent.go:53`）。
  3. 返回模型的深拷贝。现在用 JSON 往返来拷贝，保留这个做法。
- 账号隔离不受影响：返回的是部署级配置，本来就和账号无关；账号范围只体现在能力判断上。
- handler 改为 `GetCreativeGenerationCatalog(c)`，仍然 `setNoStore`。
- **测试**：
  - `product_test.go` 和 `image_test.go` 里的 `Catalog` 调用改用新签名。
  - `TestProductCatalogIsolationAndDisabled` 原来的「跨账号读别人的画布会失败」不再适用（不读画布了），改为验证「空目录时有原因」和「账号不可用时 `models` 为空」。
  - `httpapi/creative_generation_test.go` 改请求新路径，断言 `no-store` 和返回体里没有 `account_id`、价格等字段。

## 4. 前端

### 4.1 共享目录 `generationCatalog.ts`（新文件）

```ts
// 账号级生成模型目录：整个工作区共享一份。进画布时预取，
// 回到前台或生成提交被拒时在后台刷新；刷新失败时保留上一份。
type State = { catalog: GenerationCatalog | null; error: string }

export function preloadGenerationCatalog(): Promise<void>
export function refreshGenerationCatalog(): void
export function useGenerationCatalog(): State // useSyncExternalStore
export function modelsForNode(catalog, node): GenerationModel[]
export function generationUnavailable(catalog, canvas, node): string // §2.3
```

- **模块级单例和合并请求**：同时有多处要目录时只发一个请求。这是照着 buzzy 的 `useModels.ts` 做的。
- **会话边界（不变量）**：缓存和正在进行的请求都属于某一次登录会话，用 `getAuthSnapshot().sessionEpoch` 标识。
  - 发请求前先记下当时的会话编号。请求返回后（包括成功、`catch`、`finally`），会话编号已经变了就什么都不写。
  - 用 `subscribeAuth` 监听会话变化：会话编号一变（退出登录、换账号、退出后重新登录同一账号），立即清空缓存，丢掉正在进行的请求。
  - 只刷新 access token 不会改变会话编号（`session.ts` 的 `refreshAccessToken` 不调用 `advanceSessionEpoch`），所以不会因为 token 续期而重读。
  - `api.read` 本身已经会拒绝跨会话的成功响应（`api.ts:24`），所以现在并不存在跨账号泄露。这里是在共享单例这一层把边界写成明确规则，避免旧请求的失败或收尾处理改动新会话的状态。
- **刷新时机**，只有两个：
  1. `visibilitychange` 回到可见，并且距离上次读取超过 5 分钟。5 分钟是内部策略，可以随时调整。
  2. 生成提交被服务端明确拒绝，接线见 §4.6。
- **不再随画布改动重读**：画布改动不影响目录内容，归档状态由前端直接从 `canvas.archived` 读。
- **刷新是后台进行的**：刷新期间界面继续用旧目录，不会退回「正在读取」。只有第一次读取失败、手上又没有旧目录时才显示错误，并沿用面板现有的「重新读取」按钮。
- 用 `useSyncExternalStore` 订阅，不引入新的状态库（React 19 自带）。

### 4.2 预取位置

在 `WorkspacePage` 拿到 `account` 后调用 `preloadGenerationCatalog()`，和画布读取同时进行。之所以不放在 `CanvasView` 挂载时，是因为切换画布不需要重读，目录跟画布无关。

### 4.3 提示词面板 `NodeGenerationPanel`

- 目录改成从 `useGenerationCatalog()` 读，用 `modelsForNode` 筛出这个节点能用的模型，不可用原因用 `generationUnavailable` 算。
- `versions` 单独请求，不再和目录绑在一起；依赖仍包括 `canvas.revision`，作品版本还是跟着画布刷新。
- 「重新读取」按钮同时重读版本和目录（调用 `refreshGenerationCatalog`）。
- **生成键的条件要显式包含节点级不可用**：`canGenerate`（`NodeGenerationPanel.tsx:418`）新增 `!generationUnavailable(...)`。原因如下：
  - 以前节点不可用时，后端返回的模型列表是空的，`!!model` 这一条就把生成键挡住了。
  - 现在目录不再因为节点不可用而清空，同一个模型仍然能找到。只显示一句禁用文案并不能挡住生成键。
  - 现有的 `unsupported` 只检查 `type_key`，检查不到 `type_version`。
- **模型下线后的节点**：保持现状（D2）。草稿里的 `model_key` 不在目录里时，仍然显示「xxx（不可用）」的禁用项，生成键不可点，等摄影师自己选新模型。§4.6 的刷新让这个状态在页面停在前台时也能及时出现。

### 4.4 卡片模型名 `CanvasView`

删掉 `labelSource` 的那段读取，改为从共享目录生成 `key → label` 映射。这样一个请求都不用发，§1.2 的问题也随之消失。目录还没读到时照旧直接显示 key。

### 4.5 e2e mock

10 个 mock 了 `/generation-catalog` 的脚本，改为 mock `/creative/generation/catalog`，返回体不变。原来按节点返回的 mock 在适用时补上 `kind` 字段，因为前端现在要按 `kind` 筛选。

### 4.6 生成被拒时让目录失效

**现状**：生成提交要经过 `SaveQueue` 发出。`flush()` 会捕获服务端的拒绝，把任务记为 `rejected`，然后正常返回（`queue.ts:446-503`）。持久化的任务里只留下提示文字，没有错误码；只有 `creative_revision_conflict` 另外记了一个 `conflict` 标记。所以面板的 `await queue.enqueue(...)` 根本收不到 409，在面板里加 `catch` 也没用。

**接线**：

1. `Job` 增加可选字段 `code?: string`（`journal.ts`）。`flush()` 判定为 `rejected` 时，把服务端返回的错误码一起存进去。本机草稿格式的校验允许这个字段缺省，旧草稿照样能读。
2. 首次提交和恢复原操作（`flush(true)` 先查回执再重发）最后都走同一个 `catch` 分支，所以这一处同时覆盖两条路径。
3. 工作区原本就订阅队列变化，在那里判断：当前任务是 `rejected`、`path` 是 `/canvases/{id}/executions`、`code` 属于下面任意一个时，调用 `refreshGenerationCatalog()`。每个 `operation` 只触发一次。
   - `creative_generation_conflict`（409）：配置版本变了，或者任务状态有冲突。两种情况混在同一个错误码里，不去区分，刷新一次的成本很低。
   - `creative_action_unavailable`（422）：草稿里的模型已经从注册表撤下（`product.go:169`）。
   - `creative_generation_unavailable`、`creative_generation_parameters`：模型暂时不可用，或者参数定义已经变了。
4. **边界**：刷新只是更新界面显示。它不改被拒操作的请求体，不自动重新提交，也不改草稿里的 `model_key`。被拒的任务照旧由摄影师处理。

## 5. 参考了 buzzy 的哪些做法

### 5.1 借鉴的

| buzzy 的做法 | 位置 | 本设计 |
|---|---|---|
| 模型列表是模块级单例，并发请求合并成一个 | `canvas/composables/useModels.ts` | §4.1 |
| `Canvas.vue` 挂载时调用 `initCanvasModels()` 预取 | `canvas/Canvas.vue:252` | §4.2 |
| 一次返回所有模型，前端按 `outputModalities` 分组 | `useModels.ts` 的 `applyModelItems` | 后端返回全部，前端按 `kind` 筛（§2.1） |

### 5.2 留作后续的

- **模型自带参考素材上限（`referenceLimits`），连线时就检查**（`canvas/utils/connection.ts:229`）。这个体验很好，但要先确定每个模型对参考图数量、格式、比例的实际限制。到那时在 `CreativeGenerationModel` 上加字段即可，前端已经有全量目录，直接可用。
- **模型下线后自动回退到默认模型**（`ensureImageModelInList`）。我们的规则是「用户操作最高优先，系统不得在编辑队列之外改画布」，自动换模型等于替摄影师改了草稿，所以不照搬（D2）。

### 5.3 不照搬的

- **读过一次就不再刷新**：buzzy 的 `modelsReady` 一旦为真就不再请求，模型上下线必须刷新页面才能看到。本设计在回到前台和生成被拒时刷新（§4.1、§4.6）。
- **模型列表要等另一个请求**：buzzy 用 `Promise.all` 把模型和「无限额度规则」绑在一起，规则请求慢了模型也跟着慢，和我们现在模型等版本列表是同一个问题。本设计让目录独立读取。
- **每个模型的特有能力都写成单独的布尔字段**（`supportCharacterOrientation` 等）：每加一种模型都要改类型定义。我们继续用通用的 `parameters` 列表。
- **模型配置放在 GrowthBook 功能开关里**：我们的模型在 Go 代码里注册，数量很少，没必要为此引入功能开关平台。

## 6. 待拍板

| # | 问题 | 建议 |
|---|---|---|
| D1 | 节点级 `generation-catalog` 接口是删掉还是保留？ | **直接删除，不留兼容期**。<br>依据：这个接口是在 `f3396f2`（2026-09-28）加的，只存在于 `feat/creative-workspace-redesign` 分支，从没合进 `develop` 或 `main`，所以生产环境没有用到它的页面。前端打包进同一个 Go 二进制（`Dockerfile` 的 webui 阶段），发布和回滚时前后端一起切换，不会出现新前端配旧后端。<br>唯一的影响是：ECS 验收环境里在发布前打开、发布后一直没刷新的页面，读目录会 404，面板显示「重新读取」也读不回来，刷新页面就好。未完成的操作保存在本机草稿里，不受影响。按 owner 2026-10-09「旧的多余设计直接删」的原则，不留两套 |
| D2 | 草稿里的模型已经下线时，要不要自动换成默认模型？ | **不自动换**，维持现状（显示不可用、要求手动选），理由见 §5.2 |
| D3 | 契约改动怎么落档？ | 和代码同一个提交：改模块文档 §25.1 的那一句，再 `make generate` 提交两份生成物。这是创意空间内部契约，不涉及 CRM roadmap §4 |
| D4 | 路径叫 `/creative/generation/catalog` 可以吗？ | 和 `/creative/agent/catalog` 对称，建议就用这个 |

## 7. 验证

- **后端**：`make check-go PKG=./internal/creativegeneration/...`，以及 httpapi 里和 creative generation 相关的测试。
- **前端**：`make check-frontend`（tsc、oxlint、单测、build）。给 `generationCatalog.ts` 补单测，覆盖：
  - 并发调用只发一次请求。
  - 会话边界：从账号 A 切到 B、A 退出后重新登录 A、旧会话的响应晚于新会话返回，这三种情况下都不会留下或写入旧会话的目录；旧请求的失败和收尾处理也不改动新状态。
  - 刷新失败时保留旧目录。
  - `generationUnavailable` 的判断顺序；节点版本不对或画布已归档时，即使能找到模型，`canGenerate` 也为假。
- **队列**（`queue.test.ts`）：生成提交被拒时，`Job.code` 记下错误码，首次提交和恢复原操作两条路径都覆盖；没有 `code` 的旧草稿照样能读。
- **e2e**：
  - 跑 `creative-generation`、`creative-composer-baseline`、`creative-node-toolbar`、`creative-node-create`、`creative-node-label` 和 `creative-canvas-interaction`。
  - 在 `creative-composer-baseline` 里新加一条：给 mock 的目录请求计数，连续切换 3 个同类型节点、每次都打开模型下拉，断言列表立即可选、目录请求一共只发了 1 次。
  - 在 `creative-generation` 里新加「模型被撤下、页面一直在前台」：目录第一次返回模型 A，提交时 mock 返回 `422 creative_action_unavailable`，第二次读目录不再有 A。断言目录被重读了一次，下拉里显示「A（不可用）」，生成键不可点，草稿的 `model_key` 没变，也没有自动重新提交。
  - `creative-generation` 在「版本 1 · 当前采用」处的失败是改动前就有的，不算在这次里。
- **手动走查**：进入画布后选中图片节点，立即点开模型下拉，应该马上出现模型列表和说明；换到另一个图片节点重复一次；把页面切到后台超过 5 分钟再切回来，Network 里应该只多出一次目录请求。

## 8. 实施顺序

1. **契约**：改 `openapi.yaml`（新增路径、删除旧路径），运行 `make generate`，并改模块文档 §25.1。
2. **后端**：改 `Product.Catalog` 的签名和实现、handler 和路由，更新三处测试。
3. **前端**：
   1. 新建 `generationCatalog.ts` 并补单测。
   2. 在 `WorkspacePage` 预取。
   3. 改 `NodeGenerationPanel`，把目录和版本拆开读，`canGenerate` 加上节点级不可用的判断。
   4. 改 `CanvasView` 的模型名。
   5. 队列记录拒绝错误码，工作区据此刷新目录（§4.6）。
4. **e2e**：改 mock 路径，加请求计数断言，跑完 §7 的验证。

预计改动面：后端 3 个文件加测试；前端新建 1 个文件，改 5 个（另外 3 个加上 `queue.ts`、`journal.ts`）；e2e 的 10 个脚本只改 mock 路径，另新增两条用例。

## 9. 评审记录

2026-10-10 外部评审给出了 2 个高风险问题和 3 条建议，逐项对照源码核实后全部采纳：

- **配置冲突的刷新信号到不了面板**：新增 §4.6，由队列记录错误码。
- **模型下线返回的是 422，不是 409**：§4.6 的刷新条件补上了 `creative_action_unavailable`。
- **会话边界**：§4.1 写成明确的不变量。
- **运行时就绪检查和生成键门禁**：分别补在 §3 和 §4.3。
- **D1 的发布与回滚前提**：已核实旧接口没有发布过，结论仍是直接删除。

评审的负载估算（平均约 0.17 QPS）和「不引入 Redis、数据库表、配置中心或自动替换模型」的意见都与本设计一致，正文不另作修改。
