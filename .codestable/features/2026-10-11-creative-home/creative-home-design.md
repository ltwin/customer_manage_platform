---
doc_type: feature-design
feature: 2026-10-11-creative-home
requirement: V6-061、V6-062、V6-063、V6-064（首页与资产库改版第 1 期）
status: approved
date: 2026-10-11
---

# 创意空间首页第 1 期：侧栏、一句话开始、继续创作、项目页

## 0. 结论

- **范围**：把 `/creative` 从营销式大标题换成工作台首页，项目列表拆到 `/creative/projects`，两页共用可折叠的应用侧栏。原型是 `docs/prototypes/creative-workspace/v6/home.html`（commit 17a9bff）。
- **后端只补列表和归档，不新增组合接口**：
  - 项目新增 `edited_at`，作为「最近编辑」的排序依据（§1.2 说明为什么不能用 `updated_at`）。
  - 列表返回封面缩影和进行中状态，支持按名称搜索和三种排序。
  - 有任务在跑时，服务端拒绝归档。
- **一句话开始全在前端串**：
  1. 先走现有的 journal 队列建项目。
  2. 再用助手面板现有的「待确认操作」链发出首条消息：建对话 → 外发授权 → 运行，每一步的操作身份都先存进本机。
  3. 运行受理后进入画布，助手面板自动打开。
  
  任何一步失败都停在首页，文字和参考图都保留。结果未知时只能按原操作身份重放，直到得到明确的结果；明确被拒后才能修改重发，而且仍然用同一个项目。首页另存一条本机记录，刷新后能找回这次提交。
- **参考图用现有的暂存上传**（`target.kind = staged`）：不入库，也不建节点。消息引用的修订已经是保留根（`creative_message_content_refs`），所以暂存过期后附件照样可读。这一套由本期交付，做成共用模块，V6-043 以后接到画布助手里。
- **状态不做推送**：进入页面、切回标签页时重读；页面上有运行中的项目时，每 20 秒轮询一次。
- **归档和运行入口共用一条规则**：运行进入活跃状态的每个入口（新建、补充、重试）都要在项目行锁之下确认项目未归档。这也顺带补上了一个现有缺口：现在已归档的项目还能新建运行。

本期不做：

- 从模板开始（V6-065）。
- 复制项目（V6-050）。菜单里先不显示这一项。
- 资产库独立页（V6-067）。侧栏的「资产库」仍然指向现在的 `/creative/library`。
- 剩余点数（V6-022）。账号区不显示点数。
- 首次外发说明（V6-029）。首页和画布内的助手调用同一个入口，V6-029 落地后两处同时生效。

## 1. 现状

### 1.1 代码位置

| 关注点 | 现在的做法 | 代码位置 |
|---|---|---|
| 路由 | `/creative` 渲染 `ProjectsPage`，`/creative/library` 渲染 `WorkspacePage` | `frontend/src/App.tsx:124` |
| 项目页 | 营销式标题，有三处「新建项目」；标签叫「进行中 / 已归档」；封面是首字母加色块；没有改名和归档入口 | `editor/ProjectsPage.tsx` |
| 建项目 | `queue.enqueue('/projects', …)`，走每个标签页一份的 journal（单槽持久化发件箱，结果不确定时按操作身份查回执） | `ProjectsPage.tsx:120`、`useJournal.ts` |
| 项目列表 | 只按 `updated_at DESC, id DESC` 分页，参数只有 `archived`、`limit`、`cursor`；每个项目再单独查一次默认画布 | `creativecanvas/projects.go:25` |
| 助手发送 | 建对话 → `egress-consents` → `runs`。授权和运行这两步作为一条 `PendingAgentAction` 先写进 `sessionStorage`（`creative-agent-pending:{account}:{canvas}`），刷新后显示「重新发送」；建对话不在这条链里 | `AgentPanel.tsx:459-555`、`agentState.ts:90-130` |
| 默认模型 | 目录里第一个可用的模型；默认不选 Skill | `AgentPanel.tsx:255` |
| 引用素材 | 指令由 `text` 和 `content_ref` 段组成；服务端用 `RequireUsable(display)` 校验修订，并写进 `creative_message_content_refs` | `creativeagent/instruction.go:190` |
| 暂存上传 | `target.kind = staged`：上传后只发布内容，在 `creative_upload_candidates` 里挂一条带期限的 pending 记录 | `creativestaging/staging.go`、`media.ts:100` |

### 1.2 「最近编辑」的口径缺口

画布上的提交只推进 `creative_canvases.updated_at`（`service.go:288` 的 `advanceCanvas`）。`creative_projects.updated_at` 只在改名、归档、恢复时才变。所以现在的项目列表其实是按「最后一次改名或归档」排序的。V6-061 和 V6-063 写的「按项目 `updated_at` 倒序」照这个字段做，结果是错的。

也不能让每次画布提交都去改项目行的 `updated_at`。`creative_projects_notify` 触发器发出的通知是账号级的（`ID` 为空，见 0056 迁移），意思是「这个账号的所有画布都要重读」。那样的话，在一张画布上拖一下节点，这个账号所有打开的画布都会重新读取。

### 1.3 列表查询的约束

账号隔离的查询基座（`store.QueryPage`）只接受单表查询，列名和排序列必须是标识符，不能写表达式或联表。所以排序依据必须是 `creative_projects` 上实际存在的一列，不能在查询时用 `GREATEST(项目, 画布)` 现算。

## 2. 契约变更（`api/openapi.yaml`）

### 2.1 `GET /creative/projects`

新增两个查询参数：

| 参数 | 取值 | 说明 |
|---|---|---|
| `q` | 字符串，最长 100 字符 | 按名称搜索，大小写和全半角都不敏感（NFKC 后转小写，与资产库标题同一套规则），按包含匹配；`%`、`_` 按普通字符处理 |
| `sort` | `edited_desc`（默认）/ `edited_asc` / `name` | 最近编辑、最早编辑、名称 |

游标绑定 `archived + q + sort`，三者任一变化，旧游标就作废（沿用 `DecodePageCursor` 的 query 校验）。

### 2.2 `CreativeProject` 新增字段

```yaml
edited_at: {type: string, format: date-time}   # 最近编辑：画布内容提交与改名；归档、恢复不算
running:
  type: boolean
  description: 有排队中、运行中或核实中的助手运行或生成任务。归档禁用、状态轮询都只看它
waiting:
  type: boolean
  description: 有等你确认的运行（waiting_input / waiting_apply）
cover: {$ref: CreativeProjectCover}
```

两个状态分开返回，不合成一个枚举：卡片上显示哪个标签是展示规则（D3，前端决定），归档能不能点、要不要继续轮询是正确性判断，只看 `running`。合成枚举时 waiting 会盖住 running，归档菜单会被误开、轮询会提前停（2026-10-11 评审第 4 条）。

```yaml
CreativeProjectCover:
  required: [nodes, edges, more]
  properties:
    nodes:            # 最多 60 个，见 §3.3
      items:
        required: [id, x, y, width, height, kind]
        properties:
          id: {type: string}
          x: {type: number}
          y: {type: number}
          width: {type: number}
          height: {type: number}
          kind: {type: string}           # 节点类型去掉 core. 前缀；没有内容时为 empty
          content_revision_id: {type: string}   # 只有 image、video 类型才给，用来取缩略图
    edges:            # 两端都在抽样里的连线，最多 60 条
      items: {required: [source, target]}
    more: {type: boolean}                # 画布上的节点超过 60 个
```

封面不返回任何文字内容，文字块只画成灰条。

### 2.3 `CreativeProjectPage` 新增字段

```yaml
activity_counts:
  required: [running, waiting]
  description: >-
    本账号所有未归档项目里，running 为真的项目数和 waiting 为真的项目数，两者独立计数，
    一个项目可以同时计入两边；与当前页的筛选无关。首页问候语和「是否继续轮询」都用它
```

### 2.4 归档与运行入口

- `POST /creative/projects/{id}/archive` 新增错误码 `creative_project_busy`（409）：项目里有排队中、运行中或核实中的助手运行或生成任务。只有「等你确认」的运行时可以归档（D6）。
- 建运行、补充消息、重试这三个入口在项目已归档时返回 409 `archived_read_only`（「项目已归档，请恢复后编辑」）。实现时决定复用画布写入已有的这个错误码，不再另起 `creative_project_archived`：同一个事实只用一个码。以后让运行重新进入活跃状态的入口（例如 V6-033 的确认执行），都返回同一个错误码（§3.4）。

### 2.5 不变的部分

- 建项目、建对话、外发授权、建运行、上传：契约都不变。
- 字段 `updated_at` 保留原有含义，不改语义。

## 3. 后端

### 3.1 迁移 `0060_creative_project_home`

```sql
ALTER TABLE creative_projects ADD COLUMN edited_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp();
UPDATE creative_projects p SET edited_at = GREATEST(p.updated_at, c.updated_at)
  FROM creative_canvases c WHERE c.account_id=p.account_id AND c.project_id=p.id AND c.is_default;
-- 规范化名称由数据库维护：旧版本应用只写 id、name 也能插入，改名时自动跟上
ALTER TABLE creative_projects ADD COLUMN normalized_name TEXT COLLATE "C"
  GENERATED ALWAYS AS (btrim(lower(normalize(name, NFKC)))) STORED;
CREATE INDEX creative_project_edited ON creative_projects(account_id, edited_at DESC, id DESC);
CREATE INDEX creative_project_name ON creative_projects(account_id, normalized_name, id);
CREATE INDEX creative_agent_run_live ON creative_agent_runs(account_id, canvas_id)
  WHERE state IN ('queued','running','reconciling','waiting_input','waiting_apply');
CREATE INDEX creative_execution_live ON creative_node_executions(account_id, canvas_id)
  WHERE state IN ('queued','running','reconciling');
```

- **改写 `notify_creative_canvas_changed()`**：`creative_projects` 上只有 `edited_at` 变化的更新，不发通知（比较 `to_jsonb(OLD) - 'edited_at'` 和 `to_jsonb(NEW) - 'edited_at'`）。这样画布提交顺带推进 `edited_at`，也不会让同账号的其他画布重读（§1.2）。
- **down 迁移**：恢复 0056 版本的函数，删掉两列和四个索引。
- **测试**：按惯例补 store 回滚链测试，包括 label 列表和 `rollbackStagedUploads` 的链头。
- **和旧版本应用共存**（2026-10-11 评审第 3 条）：
  - 两个新列都不需要应用写：`edited_at` 有默认值，`normalized_name` 是生成列。所以旧版本应用在新 schema 上建项目、改名都不会失败。已在本机 PG 17 核实 `normalize`、`lower`、`btrim` 都是 immutable，可以用在生成列里。
  - 旧版本应用的画布提交不会推进 `edited_at`，「最近编辑」的排序会暂时滞后，但不会出错。
  - 回退顺序：只回退应用镜像，不跑 down 迁移。down 迁移只在确认不再需要新列时才单独执行。
  - 搜索词在 SQL 里用同一个表达式规范化（`btrim(lower(normalize($q, NFKC)))`），不在 Go 里另写一份，两边的规则不会分叉。
  - 补两项验证：在新 schema 上执行旧版本应用的插入语句（只写 id、name）和改名语句，断言 `normalized_name` 正确。

### 3.2 写入点

- **画布提交**：生成任务的状态推进（排队、运行、完成）也经过 `advanceCanvas`，所以也算编辑。这符合「最近有动静的项目排在前面」，不另做区分。`advanceCanvas` 里加一句 `UPDATE creative_projects SET edited_at=clock_timestamp() WHERE id=$2`。`lockCanvas` 已经对这一行加了 `FOR UPDATE`，所以不会增加锁竞争。助手工具改画布、生成结果落到节点，都走画布命令，自然也被覆盖。
- **建项目**：两个新列都由数据库填，插入语句不变。
- **改名**：同时更新 `name` 和 `edited_at`；`normalized_name` 由生成列跟上。
- **归档、恢复**：只改 `archived_at`、`revision` 和 `updated_at`，不动 `edited_at`。

### 3.3 `ListProjects`

签名改为接收一个 `ProjectQuery{Archived, Q, Sort, Limit, Cursor}`。

- **只读快照**：整次列表读取放在 `WithReadSnapshot` 里，用 `RequireCreativeRead` 校验权限，不用写事务和 `RequireCreativeCapability`。后者会拿账号级的创意写锁，列表读取期间同账号的画布写入和运行入口都得等（2026-10-11 审计第 3 条）。
- **游标只带自己排序需要的字段**：名称排序带规范化名称，按时间排序带 `edited_at`。名称在规范化前最多 200 字，NFKC 最坏把一个字符展开成 33 字节（U+FDFA），编码后的游标约 9KB，所以解码上限定为 16KB。服务前面没有反向代理，Go 默认允许 1MB 的请求头。不截断名称，因为截断后的排序键会让翻页漏项或重复（审计第 2 条）。

- **排序与游标**：按 `edited_at` 或 `normalized_name` 加 `id` 分页。名称排序用 `"C"` 排序规则（码位序），和资产库标题排序是同一规则（D4）。
- **默认画布**：改成一次查询，`project_id = ANY($2)`，不再按项目逐个查。
- **进行中状态**：
  1. 用 `project_id = ANY($2)` 查这一页项目的全部画布，得到画布到项目的对应关系（和上一步合成一条）。
  2. 用同一组 `canvas_id` 分别查 `creative_agent_runs` 和 `creative_node_executions` 里进行中的记录，两张表都有新加的部分索引，得到每个项目的 `running` 和 `waiting`。
  3. `activity_counts` 用同样两条查询，不加 `canvas_id` 条件，再排除已归档项目的画布。
- **封面**：每个项目对默认画布执行一次 `QueryPage(creative_nodes, canvas_id=$2, ORDER BY id, LIMIT 61)`，走现有的 `creative_nodes_canvas(account_id, canvas_id, id)` 索引。读到 61 行时 `more = true`，丢掉多出来的那行。
  - 连线一次查完整页：`canvas_id = ANY($2) AND source_node_id = ANY($3) AND target_node_id = ANY($3)`，`$3` 是全部抽样节点的 ID。每个画布最多保留 60 条。
  - 节点 ID 由客户端随机生成，所以按 ID 取前 60 个，相当于一个稳定的随机抽样。版式的边界框按抽样计算。
  - **世界坐标**：分组里的节点存的是相对父组的坐标。抽样后逐层补读不在样本里的祖先（每层整页一次查询），把坐标累加成世界坐标再返回，和编辑器的 `worldPoint` 同一算法。画布最多 500 个节点，所以最多补读 500 层。没被抽中的分组本身不画（审计第 1 条）。
  - 耗时只随「每页项目数 × 60」增长，不随画布大小增长（V6-063 AC-03）。
- **查询次数与负载**：满页 30 个项目时，一次列表请求约 35 次查询：列表 1 次、画布 1 次、状态 4 次、节点 30 次、连线 1 次，都在同一个只读事务里，每次都是走索引的有界读取。评审按 100 个活跃页面、20 秒轮询估算约 5 次列表请求/秒，对应约 175 次查询/秒。实际只有页面上有 `running` 项目时才轮询（§4.5），侧栏和首页只取 5 个项目，所以这是上限。
- **账号隔离**：所有查询都走 `TxAccountScope`，`account_id=$1` 由基座加上。手写的子查询要显式写 `account_id=$1`。

### 3.4 归档与运行入口的同一条规则

规则：**运行进入活跃状态的每个入口，都要在项目行锁之下确认项目未归档；归档在同一把锁之下确认没有活跃任务。**两边拿同一把锁，所以总有一方先提交，另一方看到的是对方的结果（2026-10-11 评审第 2 条）。

- **归档**（`setArchived`，`archive = true`）：项目行已经是 `FOR UPDATE`。在同一个事务里查这个项目所有画布上有没有排队中、运行中或核实中的运行和生成任务（用上面两个部分索引）。有就返回 `ErrProjectBusy`，对应 409 `creative_project_busy`。只有等待中的运行时允许归档（D6）。恢复不做这项检查。
- **运行入口**：入口在 `lockControlledRun` 或抢占槽位之前，先锁项目行（`creativecanvas.LockProjectForWorkInTx`：由画布找到项目，`creative_projects FOR UPDATE`）。项目已归档就返回 `creativecanvas.ErrArchived`，对应 409 `archived_read_only`。要改的入口：
  - `CreateRun`（`runs.go:203`）：现在完全不检查归档，这是本期顺带补上的现有缺口。
  - `SupplementRun`（`waiting.go:37`）：会把 waiting_input 的运行重新入队。
  - `RetryRun`（`retry.go:21`）。
  - 以后新增的「继续执行」入口（V6-031 的回答、V6-033 的确认），在各自的 design 里引用这条规则。
- **生成任务**：走 `lockCanvas(write=true)`，已经先锁项目行、再判断归档（`service.go:274`），不用改。
- **锁顺序冻结为：项目行 → 运行槽位（`creative_agent_slots`）→ 网关调用组 → 运行行**。已核实 `creativeagent` 里拿槽位的事务（`execute.go:140`、`execute.go:855`、`runs.go:594`、`control.go:38`）都不碰 `creative_projects`；画布工具改画布用的是单独的事务（`canvas_tools.go:221` 经由 `GetCanvas` 和画布命令）。所以项目行放在最前面不会形成环。开工时在 `control.go` 的锁顺序注释里写明这一条。
- **已归档项目里的等待运行**：保持等待状态，截止时间照常计算，到期后按现有逻辑结束。面板里补充消息会被拒绝，文案是「项目已归档，恢复后才能继续」。
- **测试**（storetest，起两个并发事务）：
  - 一边归档，一边补充同一个等待中的运行：只有一方成功，最终状态不会是「已归档且运行中」。
  - 一边归档，一边新建运行：结果同上。
  - 已归档项目上的建运行、补充、重试都返回 `archived_read_only`；恢复项目后，补充可以成功。

## 4. 前端

### 4.1 目录与路由

新建 `frontend/src/creative-canvas/home/`：

| 文件 | 内容 |
|---|---|
| `AppSidebar.tsx`、`sidebar.css` | V6-061 侧栏 |
| `HomePage.tsx`、`home.css` | 首页：问候语、一句话开始、继续创作、资产库最近添加 |
| `HomeComposer.tsx` | 一句话输入框、参考图、Skill 和模型选择、场景 chip |
| `ProjectsPage.tsx`、`projects.css` | 从 `editor/` 搬过来重写 |
| `ProjectCover.tsx` | 封面缩影（SVG），首页和项目页共用 |
| `projectList.ts` | 列表读取、状态轮询、`activity_counts` |
| `chatAttachments.ts` | 对话附件的共用模块（§4.4），V6-043 复用 |
| `startProject.ts` | 一句话开始的步骤链（§4.3） |

路由调整：

- `/creative` → `HomePage`；`/creative/projects` → `ProjectsPage`；`/creative/library` 不变。
- 旧链接 `/creative?view=archived` 跳转到 `/creative/projects?view=archived`。
- 画布页左上角「返回创意空间」仍然回 `/creative`。

### 4.2 侧栏（V6-061）

- **结构与尺寸**：照搬原型 `side.mjs` 和 `side.css`，只把颜色换成产品的 token。展开 216px，折叠 56px，图标位置固定，折叠只是把文字收起来。
- **折叠偏好**：存在 `localStorage['creative-side-collapsed']`，读写都包在 try/catch 里，读不到就默认展开。
- **断点**：761–1100px 用 CSS 强制折叠（`.forced`），同时隐藏切换按钮，不改写存下的偏好。≤760px 变成顶栏，只留品牌和三个入口。
- **折叠时的名称提示**：悬停或聚焦时用 `position: fixed` 的提示层显示，避免被侧栏的 `overflow: hidden` 裁掉。
- **最近打开**：
  - 侧栏自己请求 `GET /projects?limit=5`，也就是默认 `edited_desc`。在首页时，与「继续创作」共用同一次请求（首页请求 5 个，「继续创作」取前 4 个）。
  - 缩略图用封面里的第一张图片，没有图片就显示空白方块。
  - 状态点和卡片标签用同一个展示规则：`waiting` 显示白点，否则 `running` 显示琥珀点。点击进入画布。
- **底部**：返回 CRM 和账号区。账号区只显示头像首字和账号名，点数随 V6-022 再加。
- **导航**：首页、项目、资产库三项；当前页的那一项加 `aria-current="page"`。

### 4.3 一句话开始（V6-062）

**发送前的检查**全部在本地完成，不发任何请求：

- 文字不能为空。
- 助手目录已经读到，所选模型可用。
- 参考图都已上传完成。
- 有参考图但模型不支持识图时，提示「换一个识图模型或移除图片」。

**步骤链**（`startProject.ts`）：

1. **建项目**：`queue.enqueue('/projects', {name}, 'project-form')`，和现在一样。
   - 项目名取这句话的第一行，去掉首尾空白后截到 30 个字，超出时加「…」。
   - 拿到 `default_canvas_id` 后，把 `{projectID, canvasID, name}` 写进首页待办记录（见下）。
2. **组装待确认链**：用 `agentState.ts` 组装一条待确认链，并写进 `creative-agent-pending:{account}:{canvasID}`，也就是新画布自己的待确认槽位。
   - 链上依次是建对话、外发授权、建运行三步，操作身份在写入时全部确定。
   - 外发授权：`mode = selected_revisions`，`selected_revision_ids` 是参考图的修订；`data_classes` 是 `text` 和 `canvas_metadata`，所选模型支持识图时再加 `image_preview`。这和画布内发送完全相同。
   - 指令：有 Skill 时放一个 `skill_ref`，然后是文字段，参考图按添加顺序作为 `content_ref` 追加在后面。
3. **执行这条链**：用和面板同一个 `performAgentChain()`（从 `AgentPanel.perform` 抽出来）逐步执行。每一步成功后先替换存储里的链，再执行下一步。
4. **进入画布**：运行受理（202）后清掉链，进入 `/creative/canvases/{canvasID}`，路由 state 带 `{openAgent: true}`。面板第一次打开时默认选最新的对话，正好就是刚建的那个，所以不需要再传对话 ID。

**`agentState.ts` 的改动**：

- 现在的 `PendingAgentAction` 只支持「授权 → 运行」这一跳，要改成通用的步骤列表：`steps: [{path, operation, createdAt, payload}]`，再加一个下一步路径的模板。建对话返回的对话 ID 会填进后面两步的路径。
- 面板自己的发送也改用这条链。顺带修掉一个已有的缺口：建对话这一步的结果不确定时，重试会再建出一个空对话。
- 旧格式的待确认记录读到后转换成新格式，转换不了就按「记录损坏」处理，沿用现有的提示。

**首页待办记录**（2026-10-11 评审第 5 条）：

- 待确认链按画布存放，首页刷新后只靠它是找不回来的。所以首页另存一条记录：`sessionStorage['creative-home-start:{account}']`，和 journal 一样是每个标签页一份。
- 内容：`{text, model, skill, attachments: [{id, title, kind, source, digest?}], projectOperation?, projectID?, canvasID?, name?, conversation?, chain?: {operations}}`。`conversation` 是被明确拒绝之前已经建好的对话，改了再发时接着用，不再建一个（实现时补充）。`chain` 只存这条链的操作身份，用来认出画布槽位里是不是同一条链，请求体仍然以画布槽位为准。
- 写入时机：
  1. 按下发送、通过本地检查后，写入草稿部分。
  2. 建项目成功后，先持久保存 `projectID`、`canvasID`、`name` 和 `projectOperation`，再由 journal 清除原操作；保存失败时保留 journal 原身份。刷新后查询回执也遵循这个顺序。`projectOperation` 用来识别项目回执已保存、journal 尚未清除的中断情况。
  3. 组装好链、写进画布槽位后，补上 `chain`。
- 清除时机：运行受理；被明确拒绝后摄影师自己清空输入框；或者首页发现这条链已经在别处处理掉了（见下）。
- 首页加载时读这条记录：
  - 没有 `projectID`：恢复输入框里的文字和参考图，像什么都没发生过。
  - 有 `projectID`、没有 `chain`：恢复输入框，上方说明「项目「{name}」已建好，消息还没发出」。再按发送时用这个项目，不再新建。
  - 有 `chain`，画布槽位里也还是同一条链：结果未知，按下面「结果未知」的方式处理。
  - 有 `chain`，但画布槽位里已经没有它了：说明有人在画布里重新发送成功，或者确定被拒后恢复成了草稿。首页清掉记录，不再重放。
- 附件记录的是修订 ID 和标题，刷新后不用重新上传。暂存已经过期的话，发送时会按「参考图已过期」被拒。

**失败与重试**：

失败分两类，处理方式不同。判断规则和面板一致（`AgentPanel.tsx:444`）：4xx 并且不是 408 或 429，就是**明确被拒**；其他情况（网络中断、超时、5xx、408、429）都是**结果未知**，服务端可能已经受理。

- **建项目失败**：沿用 journal 现有的提示，包括「查询并恢复原保存」。
- **结果未知**（2026-10-11 评审第 1 条）：
  - 链原样保留在画布槽位里，请求体和操作身份都不变。输入框锁定，和面板 `locked = !!pending` 的规则一样，不能改文字，也不能增删参考图。
  - 上方说明「项目「{name}」已建好，消息还没确认送达」，并给两个按钮：
    - 「重试发送」：按同一组操作身份重放。服务端如果已经受理，会回放原来的回执（`creativeops/executor.go:62`），不会再起一次运行。
    - 「进入画布」：画布里的面板读到同一个槽位，会显示「上一条还没有确认送达 · 重新发送」。
  - 只有重放得到明确的结果，才会解锁：受理了就进入画布；被明确拒绝就按下一条处理。不提供「放弃这条、改了再发」的出口。否则旧请求实际已经受理时，新请求会再起一次运行。
- **明确被拒**（模型不可用、Skill 不可用、引用失效、参考图过期、项目已归档）：
  - 清掉这条链，输入框解锁，文字和参考图保留，上方说明原因（文案沿用 `agentState.ts:268` 的映射）。
  - 摄影师修改后再发送：在同一个项目上组装新链，用新的操作身份，不建第二个项目；对话已经建好的话也接着用。

**其他入口**：

- 「空白画布」：只走第 1 步，项目名用「未命名项目」，然后进入画布，和现在的新建一样。
- **场景 chip**：前端写死，每个是 `{标签, 示例文字, skill slug}`。点击后把示例文字填进输入框；目录里有这个 slug 的平台 Skill 时一并选上，没有就只填文字。点击不会发送。
  - 只写已经上线的平台 Skill，不照搬原型的 5 个场景。现在只有一个，「参考整理与创作方向」（`reference-direction`）；以后加了 Skill 再补（owner 2026-10-11）。
- **资产库最近添加**：
  - 请求 `GET /creative/assets?sort=recent&kind=image&limit=12`，显示成一行，等高、保持原比例。
  - 悬停时出现「+ 参考」，点击后把这个素材的内容修订加为参考图。从资产库加的参考图不经过上传。
  - 列表为空时整行不出现。

### 4.4 对话附件共用模块（V6-062 交付，V6-043 复用）

`chatAttachments.ts` 管理一组附件，每一项的状态是 `uploading`、`ready` 或 `failed`。

- **来源**：
  - 上传、拖入：`uploadFile(file, {kind: 'staged'})`，完成后取 `content_revision_id`。
  - 从资产库选：直接用素材的内容修订。
- **数量**：一次最多 6 张。超出时整批拒绝，并说明「最多 6 张」。这个上限同时受目录的 `limits.attachments` 和 `image_preview_count` 约束，取三者中最小的。
- **去重**：
  - 上传的文件按内容的 SHA-256 判重（`crypto.subtle`），在开始上传之前计算。从读取文件、计算摘要到上传完成，全程阻止发送；页面卸载后不再追加附件或启动上传。摘要随附件保存到首页恢复记录，刷新后仍能判重；旧记录没有摘要时仍可恢复。
  - 从资产库选的按修订 ID 判重。
  - 同一张图一次来自上传、一次来自资产库时，本期认不出来，算作已知限制。
  - 判重命中时提示「这张图已经添加过」。
- **失败**：每一项单独显示失败原因（`describeFailure`），可以重试或移除。重试会重新上传。
- **移除**：上传中的项移除时，先取消上传（`cancelUpload`）。
- **不入库，不建节点**：暂存记录过期后由现有的清理任务回收。消息发出后，修订由 `creative_message_content_refs` 保留（`creativereference/projection.go:41`）。
- **暂存过期**：参考图如果在发送前已经过期，服务端会按「找不到」拒绝，文案是「参考图已过期，请重新添加」。

### 4.5 继续创作与封面（V6-063）

- **卡片**：首页取列表前 4 个，按 `edited_desc`。
- **`ProjectCover`**：
  - 把抽样节点按边界框等比缩放进卡片，保留 8% 的边距。
  - 图片和视频节点用 `mediaTicket(revision, 'display', 'display')` 取图，和画布卡片同一个票据缓存，每个封面最多取 6 张。票据接口只有 original、display 两种角色，没有更小的缩略图。
  - 文字节点画成灰色线条块，空节点画成虚线框，连线画成细线。
  - `more` 为真时右下角写「部分节点」。
  - 一个节点都没有时，居中显示「空白画布」。
- **状态标签**：只有左上角一个标签，卡片不加琥珀描边。`waiting` 为真时显示白点和「等你确认」；否则 `running` 为真时显示琥珀点和「运行中」（D3）。这只是展示规则，归档和轮询不看标签，直接看 `running`。
- **问候语**：按 `activity_counts` 写「N 个项目在运行，M 个等你确认」，数量为 0 的那一半不写，两个都是 0 时只显示问候。
- **刷新**：
  - 首页和项目页在挂载、`visibilitychange` 变成可见时重读列表。
  - `activity_counts.running > 0`，或者当前列表里有 `running` 为真的项目时，每 20 秒重读一次；都没有就停止轮询。只有 waiting 的项目不轮询：它要等摄影师自己去处理。
  - 从画布返回首页时，组件会重新挂载，所以一定会重读（V6-063 AC-04）。
- **「等你确认」何时出现**：取决于 V6-031 和 V6-033 什么时候产生 `waiting_input` 和 `waiting_apply` 这两种运行。白点相关的验收随这两条一起关闭；本期先用 mock 验证渲染。

### 4.6 项目页（V6-064）

- **标签**：「当前 / 已归档」，对应 URL 参数 `?view=archived`。
- **搜索**：输入后防抖 300ms 再请求；搜索词写进 URL 参数 `?q=`。
- **排序**：下拉选择，写进 URL 参数 `?sort=`。切换排序时保留搜索词。
- **新建**：只在右上角放一个按钮，网格里不放占位卡。
- **卡片「⋯」菜单**：
  - **打开**。
  - **重命名**：卡片标题原地变成输入框。Enter 或失焦时提交，Esc 取消；去掉首尾空白后为空就恢复原名；长度不超过 200 字。请求带上 `expected_revision`。版本冲突时重读这个项目并提示「项目已在别处改过」。这套规则做成 `projectName.ts`，V6-044 复用。
  - **归档**：项目 `running` 为真时，菜单项置灰，并注明「有任务在运行，结束后可归档」。显示的是「等你确认」标签也一样，因为看的是 `running`，不是标签。服务端返回 `creative_project_busy` 时显示同样的文案并刷新列表。只有等待中的运行时可以归档，等待中的运行要等项目恢复后才能继续（D6）。
  - **恢复**：只在已归档标签里出现。
  - **复制项目**：V6-050 完成前不显示。
- **撤销**：归档或恢复成功后，卡片从当前标签里移走，toast 显示「已归档「{name}」· 撤销」。撤销就是用新的修订号发相反的操作。撤销也失败时，toast 改为说明原因。
- **空态**：三种情况文案不同。
  - 「还没有项目」，附一个新建按钮。
  - 「还没有归档的项目」。
  - 「没有名称包含「{q}」的项目」，附一个清除搜索的按钮。
- **写操作的请求方式**：直接调用 `api.send`，每次点击生成一个操作身份。不走 journal，这样项目页的改名、归档不会占住 journal，影响新建。请求失败时 toast 提示重试，重试复用同一个操作身份。

## 5. AC 对照

| 条目 | AC | 落点 | 验证方式 |
|---|---|---|---|
| V6-061 | AC-01 宽度、图标位置、本机偏好 | §4.2 | e2e：两种宽度的截图；`localStorage` 抛错时默认展开 |
| | AC-02 折叠时的提示 | §4.2 fixed 提示层 | e2e：悬停、聚焦后提示的位置在侧栏右侧之外 |
| | AC-03 断点与无横向滚动 | §4.2 CSS | e2e：在 1280、1000、700 三种宽度下，`scrollWidth ≤ clientWidth` |
| | AC-04 高亮、最近打开 5 个、状态点 | §4.2、§3.3 | e2e 用 mock，加上 storetest 验证 `edited_at` 排序 |
| | AC-05 最近打开区域内滚动、底部 | §4.2 | e2e：mock 12 个导航项时，最近打开在自己的区域里滚动 |
| V6-062 | AC-01 Enter 发送、不重复建项目 | §4.3 | e2e：<br>① mock 让第 3 步失败一次后再重试，断言 `POST /projects` 只发了 1 次，三步的 Idempotency-Key 不变。<br>② mock 让建运行时服务端已受理但响应断网：断言输入框锁定，重试时 Idempotency-Key 不变，`POST /runs` 一共只受理了 1 次。<br>③ 结果未知时刷新首页：断言记录被找回、输入框仍锁定。<br>④ 明确被拒（422）后修改文字再发：断言用的还是同一个项目，没有新的 `POST /projects` |
| | AC-02 参考图、Skill、模型、外发规则 | §4.3、§4.4 | e2e 断言授权请求体和画布内发送的一致（同一个函数组装） |
| | AC-03 附件规则 | §4.4 | 单测 `chatAttachments`；e2e 覆盖上传中不可发送、失败重试、去重、第 7 张被拒、模型不识图时的提示 |
| | AC-04 场景 chip | §4.3 | e2e |
| | AC-05 空白画布与失败停留 | §4.3 | e2e：建项目返回 500，以及第 2 步返回 503 |
| | AC-06 最近添加 | §4.3 | e2e：资产库为空时这一行不出现 |
| V6-063 | AC-01 4 个、缩影、空白画布 | §4.5、§3.3 | e2e 截图；storetest 校验封面坐标、连线和 `more` |
| | AC-02 状态标签、问候语 | §4.5 | e2e 用 mock；白点随 V6-031、V6-033 验收 |
| | AC-03 账号隔离、抽样上限 | §3.3 | storetest：另一个账号的节点和运行不出现；5000 个节点的画布只读出 61 行，用 `EXPLAIN (ANALYZE)` 的行数断言 |
| | AC-04 返回首页或刷新后状态最新 | §4.5 | e2e：mock 让状态从运行中变成空闲，切回标签页后断言状态点消失 |
| V6-064 | AC-01 标签、服务端搜索、三种排序 | §2.1、§3.3 | storetest：跨页搜索、名称排序翻页；e2e 验证切换排序时保留搜索词 |
| | AC-02 就地重命名 | §4.6 | 单测 `projectName`，加上 e2e |
| | AC-03 撤销、运行中不能归档 | §3.4、§4.6 | storetest：有进行中的运行时归档返回 busy；只有等待中的运行时可以归档；归档和补充、归档和新建并发时只有一方成功；已归档项目上的建运行、补充、重试都被拒。<br>e2e：撤销；同时有等待和运行的项目，归档菜单置灰 |
| | AC-04 新建只有一处、三种空态 | §4.6 | e2e |

## 6. 拍板结论

D1–D6 均按下表建议拍板（owner 2026-10-11）。

| # | 问题 | 建议 |
|---|---|---|
| D1 | 「最近编辑」用什么字段？ | **新增 `edited_at`**：画布内容提交和改名时更新，归档、恢复不更新。V6-061 AC-04 和 V6-063 AC-01 里写的「项目 `updated_at`」改成「项目 `edited_at`（最近编辑）」，和本设计同一个提交改。原因见 §1.2 和 §1.3 |
| D2 | 一句话开始要不要做后端组合接口？ | **不做**，三步由前端串起来，复用面板的待确认链（§4.3）。每一步本来就有幂等键，组合接口只是把同样的事搬到服务端，还要另做失败回滚，「建好项目但消息没发出」的局面也照样会出现 |
| D3 | 同一个项目既在运行又在等你确认，卡片标签显示哪个？ | **显示「等你确认」**：它要求摄影师动手，琥珀点只是告知。这只是展示规则：问候语里两边独立计数，归档和轮询只看 `running` |
| D4 | 名称排序要不要按拼音？ | **本期按码位序**，和资产库标题排序同一规则。拼音排序要用 ICU 排序规则（`zh-x-icu`），等资产库和项目页一起改，不单独给项目页做 |
| D5 | 状态要不要实时推送？ | **不推送**：进入页面、切回页面时重读，有运行中的项目时每 20 秒轮询一次。首页不是常驻页面，推送的收益很小 |
| D6 | 只有等待中的运行时，项目能不能归档？ | **能归档**：等待中的运行保持等待，截止时间照常走；项目恢复后才能补充或继续。另一种做法是等待中也不让归档，规则更简单，但摄影师必须先回去处理或取消那条运行，才能把项目收起来 |

## 7. 验证

- **后端**：
  - `make check-go PKG=./internal/creativecanvas/...`，以及 httpapi 里与项目相关的测试。
  - storetest 要起 Docker，覆盖 §5 列出的排序、搜索、游标、封面上限、状态映射、归档拒绝、账号隔离，以及画布提交推进 `edited_at` 但不发项目通知。
  - 归档与运行入口的并发测试（§3.4）。
  - 旧版本应用写新 schema（§3.1）：只写 id、name 的插入和改名都成功，`normalized_name` 正确。
  - 迁移回滚链测试。
- **契约**：`make generate`，提交 Go 和 TS 两份生成物。
- **前端**：
  - `make check-frontend`。
  - 新单测：`chatAttachments`、`projectName`、`agentState` 的链式待确认（包括旧格式的转换）、首页待办记录的四种恢复分支（§4.3）。
- **e2e**：新增两份纯 mock 脚本，只需要 Vite：
  - `creative-home.e2e.mjs`，覆盖 V6-061、062、063。
  - `creative-projects.e2e.mjs`，覆盖 V6-064。
  - 改动面板发送链后，回归 `creative-agent.e2e.mjs`（5179 的 QA 页）和 `creative-agent-blocks.e2e.mjs`。
- **浏览器证据**：首页和项目页在 1280、1000、390 三种宽度下的截图，附进验证记录。
- **合回 develop 前**：跑 `make check`。

## 8. 实施顺序

一条一个提交，每个提交同时更新总表状态、勾选 AC、回填追踪表。

1. **后端与契约**（V6-063 和 V6-064 的服务端部分）：迁移、`edited_at` 的写入点、列表查询、归档与运行入口的规则（§3.4）、OpenAPI 和生成物、storetest。总表里这两条标为「实现中」。
2. **V6-061 侧栏与路由**：新的路由和外壳。这时首页只有侧栏和「继续创作」的占位，旧项目页的内容先搬到 `/creative/projects`。
3. **V6-063 继续创作与封面**：`ProjectCover`、状态标签、问候语、轮询。
4. **V6-064 项目页**：搜索、排序、菜单、撤销、空态。
5. **V6-062 一句话开始**：`agentState` 改成链式，抽出 `performAgentChain`，实现 `chatAttachments`、`HomeComposer`、场景 chip 和最近添加。

## 9. 评审记录

2026-10-11 外部评审（档位 S1）给出 1 个阻塞、2 个高风险和 2 条建议，逐项对照源码核实后全部采纳：

1. **结果未知时丢弃旧链会重复发送**（阻塞）：属实。设计原文允许失败后改了文字就丢弃旧链；而 `creativeops/executor.go:62` 只按原操作身份回放回执。已改为区分明确被拒和结果未知，结果未知时锁定输入框、只能原样重放（§4.3）。
2. **归档防竞态漏了等待状态的恢复**（高风险）：属实，而且比评审说的范围更大。`SupplementRun` 经由 `lockControlledRun` 重新入队，不锁项目也不查归档；`CreateRun` 和 `RetryRun` 同样不查。已改为一条规则覆盖全部运行入口，并冻结锁顺序（§3.4），D6 交 owner 定。
3. **迁移不兼容旧版本写入**（高风险）：属实。`service.go:182` 建项目只写 id、name。已改为由数据库生成 `normalized_name`，`edited_at` 带默认值，并写明回退顺序和两项验证（§3.1）。
4. **展示状态同时承担归档和轮询判断**（建议）：属实。已拆成 `running` 和 `waiting` 两个布尔值，计数也按两者独立计（§2.2、§2.3、§4.5、§4.6）。
5. **首页恢复链找不回来**（建议）：属实。已加首页待办记录和四种恢复分支（§4.3）。

评审的负载估算（满页约 90 次查询）也被采纳：画布和连线改为整页一次查询，满页约 35 次（§3.3）。

2026-10-11 实现后审计给出 3 个 P2，逐项核实后全部修复，各补一条回归测试；把修复撤回后，三条测试都会失败：

1. **分组节点的封面坐标错误**：属实，封面直接返回了相对父组的坐标。已改为世界坐标（§3.3）。
2. **合法游标会被下一页拒绝**：属实，游标总是带上完整的规范化名称，而解码上限只有 2048 字节。已改为只带排序需要的字段，并放宽上限（§3.3）。
3. **列表读取持有账号写锁**：属实。已改为只读快照（§3.3）。

评审认可的取舍不变：新增 `edited_at`、前端串联现有接口、复用暂存附件、首页轮询；不做组合接口、MQ、独立工作流服务或封面预计算。
