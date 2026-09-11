---
status: proposed
version: 0.3
created: 2026-09-09
---

# 画布模块：命令、图结构与保存同步

承接 CAN-01–11、FLOW-01/02/05/07/08。遵循[公共契约](common-contracts.md)及[内容](content.md)的身份/修订规则。画布节点是创作实例，连线是参考输入；没有自动执行图工作流，不关联 CRM。

## 1. 项目与快照 API

路由前缀 `/api/v1/creative`。项目创建同时创建唯一默认画布，空项目不要求 CRM；单项目多画布的 schema 位置保留，首期不开放多画布创建。

| API | 契约 |
|---|---|
| GET /projects | archived=false/true；按 updated_at DESC、id DESC，keyset分页。不新增“最近打开”排序与每次打开写操作 |
| POST /projects | operation envelope、name；同事务返回 project_id/default_canvas_id |
| POST /projects/{id}/rename /archive /restore | 项目 expected_revision；归档后的节点/内容写入拒绝，恢复只改项目状态 |
| GET /canvases/{id} | REPEATABLE READ 快照：canvas revision/topology_revision、nodes/edges/node_inputs、所需内容摘要、不可用资源诊断；不回整库或媒体字节 |
| GET /canvases/{id}/revision | 轻量版本查询；账号鉴权。画布可见时初始建议每5秒轮询，后台暂停、回前台立即刷新；可配置退避 |
| POST /canvases/{id}/commands | 有界原子命令批次；返回紧凑提交回执和 result_revision；节点详情通过快照刷新 |
| POST /canvases/{id}/undo /redo | 目标 change_id/change_group_id 与当前预期对象状态；redo 指向实际 undo_change_id，反转该次撤销记录；内部生成受控反向/重放命令，客户端不能提供任意 before_after |

项目 updated_at 仅表示项目元信息变化，不冒充画布最近活动；列表可另展示默认画布 updated_at，但不改变排序含义。内容摘要带 truncated 标记；进入文字编辑或构建 Agent 内容输入前通过 ReadRevision 读取该明确修订的完整载荷，不能拿截断预览替换完整正文。

快照中未知类型保留原始配置为只读节点，不能让旧客户端整图覆盖擦掉它。

命令回执包括 operation_id、change_id、result_revision、before_topology_revision、result_topology_revision、changed_ids、removed_ids、object_results、必要临时 ID 映射；不复制大正文/媒体到长期回执。object_results 包含每个受影响图对象的 kind/id/is_live 和提交后的实际版本：节点 placement_revision/data_revision，边/输入 revision；删除返回身份记录的最后版本及 is_live=false。移动批次还返回每个显式提交节点的实际版本，包括最终位置未变化的节点；未变化不推高节点版本。结果在修改对象的同一事务中固定并写入紧凑回执，不能重放时查当前表补值。before_topology_revision 与 result_topology_revision 是该事务执行前后真实拓扑版本，连同回执固定保存；纯内容/布局命令未改拓扑时二者相等。对象数量受批次影响上限约束，只含身份/版本，不含正文。

当前阶段统一在回执后刷新快照并叠加未确认编辑；未来若提供增量端点须校验 before/after 水位，不能把旧回执里的节点数据覆盖新状态。

## 2. 命令与前置条件

所有命令是具名、有 schema 的 union，拒绝未知字段。节点/边/输入允许在创建批次中预分配 cwnode/cwedge/cwinp 加 UUID 的 ID；服务端验证前缀、格式、账号/画布和身份是否从未使用。项目/画布 ID 由服务端生成；复制使用服务端新 ID 并返回映射。批次初始上限100个动作、总JSON大小1 MiB；实际影响实体数另受服务配置限额控制。一个批次一次事务/一次 canvas.revision 增长；同 key 重试回放。跨画布批次首期拒绝。未知类型仅选择、读取和保留；触及其无法安全解释的配置、端口或专业绑定的写命令整体拒绝，不删字段后假装成功。

| 命令 | 必须固定的输入 | 前置条件及效果 |
|---|---|---|
| AddNodes | 新实例ID、type_key、位置、可选父组、空/文字/明确资产来源 | type/内容合法；父链布局版本和相关结构；资产来源核对资产及修订；不允许原始 Blob URL |
| MoveNodes / ResizeNodes | 去重后的选择根、父组、目标相对坐标/尺寸 | placement_revision、父链及可能自动扩组的受影响布局读集；只改布局，不使纯文字输入失效 |
| UpdateNodeMetadata | 标题、参考意图等具名字段 | data_revision；不允许顺带改类型、关系或位置 |
| ReplaceNodeText / ReplaceNodeLink | 明确内容字段 | data_revision 与旧内容修订；按 Content Fork/Append 规则创建新修订，不改原资产 |
| ClearNodeContent | 明确节点 ID | data_revision；清空内容绑定但保留实例/连线，输入变为缺内容状态，不能伪称已理解；组无内容不适用 |
| BindNodeRevision | 受信来源修订、明确目标 | data_revision、可写/类型/用途；媒体用例只经 InTx 端口提交，不开放任意未获准修订绑定 |
| ConnectReference / DisconnectReference | 两端节点、端口、角色；断开时 edge_id/revision | topology_revision、有关节点 data_revision、边 revision；端口兼容、无自环/重复/引用环 |
| SetNodeInputs | 完整具名 slot 输入集（动态同画布节点或固定修订） | 目标 data_revision、相关来源数据版本与 topology_revision；与可见连线合并判环，不藏媒体ID于config |
| GroupNodes / UngroupNodes | 明确选择根或组、目标父组 | topology_revision、闭包布局读集；所有世界坐标保持，组不提供内容端口 |
| ReparentNodes | 目标父组、选择根 | topology_revision、源/目标父链及受影响布局；跨组使用显式命令，普通拖动不悄悄换父组 |
| DuplicateSelection | 根节点、平移量 | 来源闭包布局/内容版本及拓扑；重建子树与内部边/输入，新身份不共享可变配置 |
| RemoveNodes | 明确根节点，删除组须带 group_mode=subtree | topology_revision、删除闭包及相关依赖版本；删除节点/子树、端点边及动态输入，释放引用，不删库资产/专业文档 |

普通“删除组”删除组及其子树，UI 明示影响范围；保留子节点使用 UngroupNodes。结构动作都由服务端计算闭包并核对请求的 read_set 完整：节点、祖先、必要兄弟、边和输入的版本缺失返回422，不通过省略条件关闭并发保护。base_revision 只是来源水位；纯内容命令只检查实际读集，不因无关节点移动而全部冲突。

同一批次的前置条件针对执行前快照；服务端按顺序模拟批次、计算完整影响集合，拒绝矛盾动作或重复变更。批次内新对象以本批预分配ID引用；类型或权限失败整批回滚，不部分保存。

### 参考关系与删除影响

通用非组节点提供受类型目录约束的参考端口；每个输入保留明确来源类型（图片/文字/链接/视频/音频），目标按能力解释。不因为支持视频节点就承诺模型已经能理解视频。组不能作为参考端点；未来首尾帧等模式只在类型目录支持时出现。

同画布动态 node_inputs 与可见 edges 构成同一有向参考图，两类关系一起查环；固定内容修订没有动态节点依赖。删除上游节点时，移除所有引用该节点的动态边/输入，递增其下游节点 data_revision，防止旧 Agent 上下文继续写入。固定修订输入继续保留，不能因其历史来源节点消失被误删。

连入/断开参考边、改顺序或角色均影响目标的参考配置：递增目标 data_revision 与 canvas topology_revision，边本身 revision 递增。删除目标时其自身输入随删；其他受影响目标同样递增版本。metadata/文本写入仍只修改对应节点 data_revision。

## 3. 父子坐标与复制

坐标是画布单位，子节点相对父组左上角。世界坐标等于自身位置加所有祖先的位置。Group/UnGroup/Reparent 在服务端与前端预览使用相同规则；只把选择根移动一次，父子同时选中时去掉已选祖先覆盖的子节点。

构组时可将不同父组的选择根提升到共同祖先内的新组，保持世界坐标；旧父组和新父组的范围都在影响闭包内。空父组保留，不自动删除。解组提升直接子节点到祖父组，转换相对坐标；内层子树保持自己的坐标。

自动扩组时更新组原点/尺寸，并反向调整直接子节点的相对坐标，保住未移动子节点的世界位置；逐层向上计算。拖动组只移动父位置，不给每个后代重复应用位移。尺寸必须为正、所有坐标有限；不允许 NaN/Infinity。

复制只保留选中闭包内的动态边与动态 node_inputs，来源节点重映射为新ID；指向闭包外的动态输入不复制并返回 omitted_reference_ids，UI 提示该范围。固定修订输入可在用途校验后复制，继续独立保留内容。跨画布复制本期不自动建立动态引用。

### 删除、撤销与版本不回退

每次结构/字段修改同步保存 change 明细和涉及的内容修订保留。Undo/Redo 使用新的 operation_id 和下述受验证的逻辑 postconditions，物理版本仍单调增长；不能把“原数字版本相等”当作唯一后置条件，也不能偷换成当前版本。多步 Agent change_group 要么全部可逆，要么全部拒绝；一个已提交批次是一条撤销单元。

删除后的 ID 不能让 AddNodes 重新使用；恢复仅由服务端 Undo 在验证后允许。新增 creative_graph_identities 记录同账号/画布的对象类型、是否存活及最后版本（节点布局/数据、边/输入修订）。删除保留轻量身份，恢复时从最后版本递增，不能重置为1，防止删除→撤销后旧请求误以为还是同一状态。每次图对象写入都同步其最后版本；一致性扫描核对身份记录与活跃对象。

该身份记录只防止 ID 重用和版本回退，不保留媒体或无限期保存撤销内容；随画布生命周期保留，首期项目只归档、不提供硬删除。撤销记录过期即拒绝 Undo，但仍不允许客户端复用已占用的图 ID。

### 可验证的撤销状态链

每个图对象的 graph_identities.effect_heads 保存有 schema 的固定字段组到 `{state_token, value_hash}` 映射。state_token 是服务端生成且客户端不可指定的随机状态标记（不作为认证令牌），不是对象/媒体引用；value_hash 来自规范化字段值。字段组为 existence、placement、data、relations：节点位置/父组/尺寸属于 placement；正文修订、元信息和输入配置属于 data；子节点集合与所有关联边/动态输入的完整描述属于 relations；边和输入使用 existence/data。不存在的对象也有明确 absence 值及标记，身份记录继续保留。

普通编辑对实际变化的字段组生成新 state_token，即使手工改回旧值，也不会复用旧标记。change.before_after 使用严格版本化结构，逐对象记录字段组的 before/after 值、hash、token 和物理结果版本；真实媒体引用继续登记在 change_content_refs，token/hash 不构成保留根。一次批次对同一字段组只记录整批的首个 before 和最终 after。

Undo 在当前画布锁中同时检查：客户端观察的当前物理读集未变化、目标字段组当前 token/hash 等于待撤销操作的 after、现有数据计算的 hash 与记录一致、父子/参考/用途等图规则仍成立。通过后写回 before 值，并**恢复受验证的 before token/hash**，但所有实际行版本从当前值递增。只有服务端验证后的 Undo/Redo 可以恢复旧 token，普通命令和客户端均不能指定 token；缺失头记录或 hash 不一致是完整性异常，不能现场猜造状态链。

relations 参与结构撤销：新增/移除/修改边或动态输入要更新所有相关端点的 relations 头，组成员变化更新父组的 relations 头，连线目标还照常递增 data_revision。撤销“创建节点/组”因此不能顺手删掉后来新增的连线、输入或孩子；这类依赖变化未被合法撤销时整体冲突。无关对象或无关字段组的修改不要求一起撤销。

例：初态 S0/rev1；编辑A产生S1/rev2，编辑B产生S2/rev3。撤销B核对S2后恢复S1，但物理版本为4；再撤销A可核对S1，恢复S0且物理版本为5。重做指向实际撤销记录的反向效果：重做A恢复S1/rev6，重做B恢复S2/rev7。若中间摄影师手工写成同样的S1内容，普通编辑产生新标记M，不能被误认为受验证的撤销；此时撤销A拒绝。

整组撤销先按服务端提交顺序合成写集，以 `(object_id,field_group)` 为键：最早 before 是恢复目标，最后 after 是检查目标；同组后一个写入的 before token/hash 必须承接前一个 after。中间若有组外修改且没有经过合法撤销恢复状态链，不能忽略它来合成。全部组成员的明细/引用必须仍在保留期，全部字段组合成和现状校验通过后才原子写回，每个受影响物理版本从当前递增一次。Undo 的明细记录整组实际前后效果，Redo 反转这份记录，不逐条用旧版本同时匹配重叠写集。

状态头随轻量图身份保留，历史明细/正文仍按既定期限清理；明细过期就不能发起该次Undo，即使状态token还在。状态链只帮助校验可逆效果，不将系统变成从事件重建全部当前数据的事件溯源架构。

## 4. 前端保存与恢复

React Flow 使用受控 nodes/edges；适配层先排父节点再排子节点、映射 parentId，相对位置直接使用领域坐标。extent='parent' 会限制子节点移动，不直接套用于当前自动扩组规则。选区、悬停和 measured 等属于UI状态，不能进持久DTO。[React Flow 子流](https://reactflow.dev/learn/layouting/sub-flows)

每个画布一个 EditorStore，分为 acknowledged_snapshot、draft_intents、pending_commands、selection/viewport。高频拖动仅本地预览，松手一次命令；文字连续输入按编辑单元合并。按节点订阅并缓存组件，不让每次拖动重渲染整个侧栏。

| 情况 | 必须采取的动作 |
|---|---|
| 普通编辑 | 先记录可恢复草稿；形成命令时固定 operation/body/read_set 并落 IndexedDB，再发送；一个画布同标签页一次仅发一个命令 |
| 前一个本地命令已确认 | 只能用该命令回执中固定的 object_results 映射明确本地依赖，再与快照中的对象版本/is_live 比较；不是把所有草稿条件换成最新版本 |
| 回执/网络结果未知 | 保持原 body/key，查原操作或原样重试；不先释放被该操作使用的媒体，不标已保存 |
| 外部窗口/Agent 有变化 | 拉取服务端快照；只叠加读集未受影响的草稿。涉及同一输入/父链的本地草稿标冲突、保留双方 |
| 409 冲突 | 暂停该命令及依赖它的后续命令；展示重新应用/保留为新节点/放弃本地等实际可行动作；确认后生成新operation，不能偷改旧body |
| 页面刷新/断网 | 按账号/画布/标签页恢复 pending；先查询回执和最新快照再决定；过期命令保留草稿，不当新写入自动发出 |
| Agent 发送 | 先刷新相关读集、提交所选节点的待保存编辑；失败则保留请求，不拿服务器旧文字替代屏幕输入 |

结构依赖也必须映射：A新建节点将拓扑7推进到8，回执固定before=7/result=8，本地随后连线的B只能将明确依赖A的拓扑条件承接为8。若快照前外部新增节点使拓扑变9，B保留8并冲突；若仅其他节点内容变化、拓扑仍8，则在其余读集有效时可继续。只有A实际改变拓扑且before与草稿依赖链衔接时才推进；纯内容命令即使看到更新的拓扑也不能授权把旧草稿条件替换成它。缺少拓扑结果或链断裂时保留冲突，不从result_revision或“猜加一”推导。

例如A提交后把N从数据版本1改为2，而读取快照前另一窗口把N改成3：A回执仍返回2，依赖A的草稿B保留预期2并标冲突，不能改为3续发。若快照仅因其他对象变化而增长，N仍为2，B可以在原读集不变时继续。若回执缺对象结果、映射不完整或记录过期，先查询原回执；无法核实时保留冲突，不自行推断版本加一。

草稿创建时就记录观察到的服务端版本及本地依赖，不能到发送时直接用最新值填前置条件，否则会掩盖并发修改。viewport 仍为本机偏好，未配置则 fit 内容范围；不影响保存状态或服务端版本。

批次确认后通过快照重建基线，选区仍按活跃ID保留；删除选中节点及时移除选区及待发送引用。SSE 只是 Agent 提交后的通知，多窗口仍按 revision 轮询；每个节点不建立独立流连接。

## 5. 跨模块事务端口

| 端口 | 原子保证 |
|---|---|
| ValidateNodeTargetInTx | 核对账号、项目可写、节点类型、expected_data_revision；初始化上传与发布时都调用 |
| BindNodeRevisionInTx | 已锁定目标和合法修订后换指针、递增 data/canvas 版本、更新图身份/记录change及内容保留；参与外层publish/adopt操作，不另开事务或第二回执 |
| AddAssetNodesInTx | 使用库模块 ResolveAssetsInTx，随后锁画布，批量建节点与明确修订/来源快照；不重复上传或修改资产 |
| ReadNodeForSaveInTx | 在库根→项目/画布锁序下核对节点 data_revision，固定存入库的修订；不依赖可过期媒体 URL |
| ApplyToolCommandsInTx | Harness 已取得运行执行权；将运行实际读取的来源版本与命令读集合并，在图命令同事务核对 epoch/cancel/read_set、提交实际图效果和步骤回执 |

绑定目标失效只能由媒体协调器转候选；画布端口返回可分类的目标冲突，不吞并数据库错误或内部失败。回执由最外层 CommandExecutor 拥有；具体领域参与者只写自身状态/变更明细。

## 6. 实施验证

参见 [第二批切片](library-canvas-slices.md) 与 [SQL 草案](library-canvas-schema.sql)。服务测试必须覆盖同账号双窗口、组闭包/循环、目标输入变化使旧Agent过期、复制引用范围、删除→撤销后旧版本被拒绝及保存队列恢复。SQL 探针不等于 React Flow 集成/帧性能或 Harness 写工具已经完成。


## 7. 工具化基础能力

[画布工具化契约](../canvas-tooling.md)补齐共享能力目录、NodeDefinition有效动作和执行器边界。界面与Eino工具共用本模块查询/命令/节点执行应用入口；完整基础动作先具备正式应用能力，不能只留前端本地实现。节点执行是独立任务，受理与结果发布回执分开；内部text-compose执行器验证输入→任务→产物→有条件发布，未注册动作明确不可执行。真实生成由首期FND-13接入，3D仍后续，不因参考连线自动执行。

## 8. Prompt、状态与结果版本

首期新增[node-generation](../node-generation.md)契约：GetCanvas/GetNode返回类型化data、服务端status、prompt草稿与capabilities；SaveNodePrompt、SelectNodeVersion、DeleteNodeVersion、ListNodeVersions、ReuseVersionPrompt为共享应用能力。FND-04建立模型/命令和内部执行基线，FND-13完成真实媒体生成/面板/历史体验。版本列表是真实保留根，不以撤销过期清空；Prompt refs动态节点关系加入既有参考图/环检查，删除源节点时与edges/node_inputs一并处理。状态刷新不增加data_revision，采用版本才改变输出。

### FND-04 读集投影

普通画布命令只读取当前活跃图；历史身份仍保留在服务端，不随每次编辑整批提交。近期 change 摘要提供其涉及对象的当前 `read_set`，撤销/重做只使用目标 change 的读集（包含待恢复墓碑的实际版本）。`object_states`投影近期可撤销变化涉及的删除身份，以及所有活跃版本的身份、版本号与所属node_id。普通命令不携带版本；复制只带选中闭包的当前选用版本，删除带被删除闭包的活跃版本；活跃版本不受近期撤销窗口限制。它不是全量历史身份目录。
