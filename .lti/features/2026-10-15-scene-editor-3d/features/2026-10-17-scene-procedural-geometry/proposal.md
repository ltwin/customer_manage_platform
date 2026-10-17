---
risk:
  factors:
    - trust-boundary
    - data-migration
    - concurrency
    - perf-sensitive
    - broad-impact
behavior: changed
reads:
  - creative/scene
  - creative/documents
decisionsPending: false
---
# 本次交付：程序化几何（C2 `scene-procedural-geometry`）

## Why

Epic `scene-editor-3d`（批准基线 v2）的第二个子项。C1 已交付场景文档和编辑器外壳（2026-10-17 合入 develop，`59d2ea3`），但场景里只有 7 种内置基础体。CG 合成需要柱子、拱门、台阶这类具体物件，epic 定下的做法是：物件几何由一段代码生成，在禁网的沙箱里烘焙成 GLB，GLB 是几何的唯一事实源。

roadmap 对 C2 的要求：在编辑器的代码面板里粘贴或修改几何模块，沙箱编译出 GLB，生成 `model` 内容并放进场景；调参数会重新烘焙；共用的模型被单独修改时先分叉。

本批要消除三个不确定性：

1. **沙箱的安全性和可用性**（epic 未决 #2）。实施前已做穿刺：iframe + Worker 方案在 Chromium 和 WebKit 上都成立，见 `discovery/research/sandbox-spike.md`。穿刺暴露的浏览器差异需要修订 epic 契约，见 Risk Notes。
2. **烘焙、上传、校验链路的时延**：一次调参要经过编译、暂存上传、worker 校验和提交命令，实测后才知道快慢。
3. **GLB 结构校验**：服务端不执行代码，只能靠解析 GLB 把住几何事实。

C2 只做摄影师手动写代码和调参数。助手写模型（`write_model`）和客户端任务属于 C3。C2 交付的沙箱、模型内容和模型命令，正是 C3 的基础。

## What Changes

**交付的 epic 目标契约**（`epic bind --deliver`，正文在 epic `contracts.md`，以本次修订后的批准版本为准）：
- `patterns: 几何模块契约`
- `patterns: 客户端代码沙箱`
- `data: 模型内容`
- `api: 场景模型命令`

**沙箱与建模标准库（前端）**：
- 独立的沙箱页面 `/scene-sandbox/`，带专用 CSP，由 Go 静态服务和 Vite 开发服务器以同样的方式提供。
- 编辑器宿主每次编译都新建一个 Worker，硬超时 5 秒。宿主自己解析返回的 GLB、重新计算 manifest 并检查上限，不信任沙箱报的数字。
- 建模标准库 `lib@1`：布尔运算（基于 `three-bvh-csg`）、挤出、旋转体、阵列、槽位材质。
- 安全样例集纳入 `make check-frontend`（Chromium）；WebKit 手动跑一遍并记录结果。

**模型内容（后端）**：
- 内容类型加 `model`；暂存上传的声明类型加 `model`（`.glb`，上限 16 MiB）；新增一条迁移。
- worker 校验阶段用 Go 解析 GLB 结构，并算出包围盒、三角形数、槽位、部件和挂点，作为服务端认定的 manifest。
- 缩略图作为模型内容的 `thumbnail` 对象，可以通过现有媒体读取票据访问。
- 媒体 GC 的类型范围加 `model`。

**场景模型命令（后端）**：
- 场景命令新增 `commit_model` 和 `set_instance_model`，`add_objects` 放开 `model_instance`。
- `commit_model` 消费两个暂存上传（GLB 和缩略图），核对代码哈希、参数和 manifest，写入不可变的模型内容修订，并登记为本修订的引用。
- 读取场景时返回 `models`：每个被引用的模型修订的 manifest 和生成参数（不含代码）。代码通过现有的 `GET /creative/content-revisions/{id}` 读取。

**编辑器（前端）**：
- 渲染模型实例：按模型修订加载并缓存 GLB，用 clay 材质显示；加载中或不可用时显示占位。
- 代码面板：
  - 新建代码模型：在添加菜单里选「代码模型」，从模板开始或直接粘贴代码。
  - 修改选中实例的代码，然后重新生成。
  - 编译出错时显示错误和行号，场景不变。
- 参数面板：按模型的参数定义自动生成表单，改值后重新烘焙。共用模型默认只改当前实例（分叉），也可以选择「应用到全部 N 个实例」。
- 一次调参或重新生成是一个撤销步骤。重新烘焙期间先在本机显示新几何，状态栏提示保存进度。

**不在本批**：助手工具和客户端任务（C3）、材质槽的分配界面（C5）、模型存入资产库（C6）、外部 GLB 导入（下一版）。

## Acceptance Criteria

- **AC-1 新建代码模型**：**WHEN** 摄影师在添加菜单选「代码模型」，用默认模板点「生成」，**THEN** 物件出现在打开菜单时指针所指的地面位置（`mount: floor` 时底面贴地）并被选中；保存后产生 1 个场景修订和 1 个模型内容修订；关闭再打开，物件和几何一致。
- **AC-2 调参数**：**WHEN** 摄影师在属性栏把模型参数（例如高度）改成新值，**THEN** 几何随之变化，产生 1 个场景修订，Cmd+Z 一步回到原几何，Cmd+Shift+Z 重做。**WHEN** 参数越界，**THEN** 输入框按参数定义的范围限制，不会提交越界值。
- **AC-3 共用模型先分叉**：**WHEN** 用 Shift + D 复制出第二个实例后，修改其中一个的参数，**THEN** 只有它变化，另一个保持原样。**WHEN** 修改时选择「应用到全部 2 个实例」，**THEN** 两个一起变化，仍然只产生 1 个场景修订。
- **AC-4 修改代码**：**WHEN** 摄影师在代码面板修改代码并重新生成，**THEN** 选中实例换成新几何，原参数中新代码仍然支持的值保留。**WHEN** 新代码增删了参数或改了范围，**THEN** 参数面板随之更新，保存的生成信息带新的参数定义。**WHEN** 代码有语法错误、运行出错、返回的不是 three.js 对象或超过 5 秒，**THEN** 面板显示错误（语法和运行错误标出行号），场景不变、不产生修订。
- **AC-5 上限**：**WHEN** 代码超过 64 KiB、三角形超过 30 万或 GLB 超过 16 MiB，**THEN** 编辑器给出明确提示，不上传、不产生修订。
- **AC-6 沙箱安全**：**WHEN** 模型代码尝试访问网络（`fetch`、XHR、WebSocket、EventSource、动态 `import`、`importScripts`）、cookie、`localStorage`、IndexedDB、父页面 DOM、顶层导航、`eval`，或死循环、耗尽内存，或产出内含外链 URI 的 GLB，**THEN** 访问失败，或在时限内被终止；编辑器保持可用，攻击者地址收不到任何请求。样例集在 Chromium 上纳入 `make check-frontend`，WebKit 手动运行一次并记录结果。
- **AC-7 服务端把关**：
  - **WHEN** 提交的 manifest 与服务端从 GLB 算出的不一致、代码哈希不符、参数不符合定义，或 `ref` 未定义 / 重复，**THEN** 返回 400 `validation_failed`，不产生修订。
  - **WHEN** 暂存上传不存在、属于其他账号、已过期或已被消费，**THEN** 返回 409 `creative_upload_unavailable`；**WHEN** 引用的模型修订不可用，**THEN** 返回 400 `creative_model_unavailable`。两者都不会让编辑器误以为场景进了回收站或不存在。
  - **WHEN** 上传的 GLB 不在接受的静态子集内（容器或外部 URI 不合规、声明的包围盒与顶点数据不符、索引越界、节点成环，或含形变、骨骼、动画、纹理、未知扩展，或超出预算），**THEN** 上传标记为失败，无法提交；编辑器在上传前就用同一规则拦下并提示原因。
- **AC-8 并发**：**WHEN** 一个标签页移动某个模型实例，另一个标签页同时修改它的参数，**THEN** 两边都成功（摆放与内容是不同单元）。**WHEN** 两边都改它的参数，**THEN** 后到者收到冲突提示。**WHEN** 摄影师改参数后、还在烘焙上传时，另一个标签页或助手改了同一实例的参数，**THEN** 摄影师的保存收到冲突提示，不会覆盖对方的修改。
- **AC-9 读取与可用性**：**WHEN** 读取场景，**THEN** `models` 包含快照引用的每个模型修订的 manifest 和生成参数，不含代码。**WHEN** 引用的模型修订不可用，**THEN** 编辑器显示「模型不可用」占位，场景其余部分照常可编辑。
- **AC-10 GC**：**WHEN** 模型修订被任一场景修订引用，**THEN** 媒体 GC 不回收它的 GLB 和缩略图。**WHEN** 场景被彻底删除，**THEN** 不再被引用的模型修订按媒体 GC 规则回收。
- **AC-11 时延实测**：本机记录「改参数 → 已保存」各阶段耗时（编译、宿主校验、缩略图、上传与 worker 校验、提交命令）的 p50 / p95，结论写进 verification。假设本机 p50 ≤ 3 秒，超出时评估把小模型改为随命令直接提交。

## Impact

- **后端**：`creativescene`（新操作、引用登记、读取 `models`）、`creativecontent`（`model` 类型与写入入口）、`creativemedia`（GLB 校验、上传类型、`thumbnail` 票据角色、GC）、`creativestaging`、新迁移 0074、`platform/httpapi`（沙箱页面的 CSP）、`api/openapi.yaml` 与两份生成物。
- **前端**：`frontend/src/creative-scene/`（沙箱宿主、代码面板、参数面板、模型渲染）、新的沙箱构建入口与 `public/scene-sandbox/`、`creative-canvas/editor/media.ts`（上传类型 `model`）、`package.json`（`three-bvh-csg`、CodeMirror 6）。
- **契约**：交付上列 4 条 epic 目标契约；修订 epic 契约（见 Risk Notes）；修改已交付的 `api: 读取场景`（模型地址改走媒体票据）。
- **部署**：creative-worker 必须先于 server 升级，否则旧 worker 会把 GLB 上传判为不支持的格式，GC 也不会处理 `model`。

## Risk Notes

- **信任边界**：模型代码可能来自网页注入（C3 之后），沙箱是必需的。穿刺已在 Chromium 和 WebKit 上验证网络、存储、DOM、导航、eval 全部被拦截，死循环和内存炸弹不影响宿主；真实 Safari、Edge 和 Windows 未测。服务端从不执行代码，以 worker 解析 GLB 得到的 manifest 为准。
- **穿刺带来的 epic 修订（需 owner 批准，`epic revise`）**：
  1. `patterns: 客户端代码沙箱`：
     - CSP 不能用 `script-src 'self'`：WebKit 在 opaque origin 里匹配不到，必须写明确的站点源，并限定到沙箱路径。
     - Worker 必须是 classic：Chromium 拒绝 opaque origin 里从 Blob 创建的 module Worker。
     - 每次编译新建 Worker；宿主重新计算 manifest。
     - 安全样例集在 `make check-frontend` 里只跑 Chromium。
  2. `patterns: 几何模块契约`：`Math.random` 不能禁用（three.js 生成 UUID 要用），改为每次编译从固定种子开始的确定性序列；`Date` 仍不可用。补上 `lib@1` 的函数清单。
  3. `data: 模型内容`：
     - 增加「暂存形态」：worker 校验通过后，暂存候选持有一条只含 GLB 和服务端 manifest 的模型修订，不能被场景引用；`commit_model` 另写完整修订。
     - 缩略图可经 `thumbnail` 角色的票据读取。
  4. 顺带处理 C1 留下的两处：roadmap 补上历史清理子项 `scene-history-cleanup`；design「写入频率」的合并措辞改为「同一次连续编辑」。
- **时延**：一次调参要走完编译、暂存上传（多次 worker 任务跳转）、校验和提交。开发环境连远程数据库时会更慢。缓解：本机先显示新几何，保存在后台进行；AC-11 实测后再决定是否需要随命令直接提交小模型。
- **影响面**：上传类型、媒体票据角色和 GC 是创意空间共享的媒体链路。新增值只扩大范围，不改变已有类型的行为；回归靠 `creativemedia`、`creativestaging`、`creativecontent` 的现有测试。
- **迁移**：0074 扩大 4 个 CHECK（内容类型、上传和候选的声明类型、候选的采用目标）。存在 `model` 内容或上传时拒绝回滚。
- **方案评审**：做了两轮，阻塞和高风险项都已改进设计。第一轮 6 项：沙箱页面路径、宿主加载 GLB 时会外联、manifest 比对规则、错误码冲突、跨意图的模型引用、`all_instances` 的并发。第二轮 3 项：沙箱结果缺少模块信息、编辑基线取得太晚、GLB 自报范围不可信。

## owner 决定（2026-10-17，均按推荐）

1. **代码面板用 CodeMirror 6**：语法高亮、行号、出错行标记，只在打开代码面板时加载（约 130 KB gzip）。
2. **模型 GLB 和缩略图走现有的媒体票据接口**：读取场景不再返回 `glb_url`、`thumbnail_url`，编辑器按模型修订调用 `POST /creative/media-access-tickets` 取地址并缓存；修改 C1 已交付的 `api: 读取场景`（这两个字段一直为空，没有调用方）。
3. **只接受受限的静态 GLB 子集**，写进长期契约 `data: 模型内容`：只有三角网格、常量材质和场景树；包围盒从顶点数据算出并核对文件声明；形变、骨骼、动画、纹理、灯光、摄像机和扩展一律拒绝。外部 GLB 导入（下一版）需要时再按需放开。
