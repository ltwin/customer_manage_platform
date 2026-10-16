## creative/documents: ADD 从画布新建文档

画布 `add_node` 对已注册且允许新建的文档类型，在同一事务里创建文档、第 1 个修订和画布节点；撤销只移除节点，文档保留，可以放回。

### Scenario: 新建与撤销
- GIVEN 账号的画布上没有该文档类型的节点，且该类型已注册并允许新建
- WHEN 摄影师以该类型调用 `add_node`，随后撤销这次画布改动
- THEN 新建时文档（修订号 1）、修订行和节点同时出现；撤销后节点消失，文档仍可读取，也可用 `add_node` 带 `document_id` 放回

### Scenario: 不允许新建的类型
- GIVEN `internal.document` 已注册但不允许新建
- WHEN 以 `internal.document` 调用 `add_node` 新建
- THEN 返回 400 `validation_failed`，不产生任何行

## creative/documents: ADD 按单元的并发写入

文档的写入是一批原子操作，按单元乐观并发：每个单元的修订号等于它最后一次被改动时的文档修订号，单调递增、永不复用。

### Scenario: 改不同单元
- GIVEN 文档当前修订为 N，摄影师和助手都持有修订 N 的单元修订号
- WHEN 两者几乎同时提交，分别改不同单元
- THEN 两者都成功，修订号依次为 N+1 和 N+2，最终快照包含两处改动，各单元的修订号等于改动它的那个修订

### Scenario: 改同一单元
- GIVEN 摄影师和助手都基于修订 N 改同一个单元
- WHEN 两者几乎同时提交
- THEN 先到者成功；后到者返回 409 `creative_document_conflict`，列出冲突单元及预期和当前修订号，不产生修订

### Scenario: 非法批次
- GIVEN 一批操作里同一单元出现两次，或缺少某个涉及单元的预期修订号
- WHEN 提交
- THEN 返回 400 `validation_failed`，整批不生效

## creative/documents: ADD 恢复旧版本

可以把整份文档或指定单元恢复到旧修订的内容，恢复本身产生一个新修订并记录来源。

### Scenario: 整份恢复
- GIVEN 文档当前修订为 N，旧修订 k 存在
- WHEN 摄影师带预期文档修订号 N 单独提交 `restore` 到 k
- THEN 产生修订 N+1，内容等于 k，所有单元修订号变为 N+1，修订记录 `restored_from = k`；之后用旧修订号提交的任何请求都冲突

### Scenario: 整份恢复遇到并发修改
- GIVEN 摄影师读到修订 N 之后，助手只改了正文、标题没变，文档到了修订 N+1
- WHEN 摄影师以预期文档修订号 N 提交整份恢复
- THEN 返回 409 `creative_document_conflict`，冲突单元为 `*`，当前内容不变

### Scenario: 恢复已删除的单元
- GIVEN 某个单元在修订 k 中存在、当前已被删除
- WHEN 以该单元预期为「不存在」（null）、集合单元为当前修订号提交 `restore_units`
- THEN 该单元回到 k 的内容；如果它在此期间经历过新增再删除，原请求因集合单元修订号变化而冲突

### Scenario: 恢复含失效引用的旧版本
- GIVEN 旧修订 k 引用图片 A，当前修订已不引用 A，A 的展示授权已撤销
- WHEN 摄影师恢复到 k
- THEN 恢复成功，读取时 A 标为不可用；普通编辑新增 A 则返回 403 `creative_usage_denied`

### Scenario: 按单元恢复
- GIVEN 修订 k 之后单元 A 和 B 都被改过
- WHEN 提交 `restore_units`，只恢复单元 A 到 k
- THEN A 回到 k 的内容并获得新修订号，B 的内容和修订号不变

## creative/documents: ADD 修订历史与写入者

每个修订记录写入者（摄影师或助手）、助手的运行 ID 和改动单元，可以按运行查询，也可以读取旧修订的指定单元。

### Scenario: 按运行查看改动
- GIVEN 助手在一次运行里写了两个修订，摄影师之后又写了一个
- WHEN 按该运行 ID 列出修订
- THEN 只返回助手的两个修订，按修订号倒序，每项带改动单元

### Scenario: 查看某版本之后的变化
- GIVEN 助手最后一次写入产生了修订 r，之后摄影师又写了两个修订
- WHEN 以 `since_revision = r` 列出修订
- THEN 只返回摄影师的两个修订，每项带写入者和改动单元

## creative/documents: ADD 回收站与彻底删除

回收站中的文档可读不可写；只有回收站中的文档可以被摄影师彻底删除，删除后历史一并消失，挂它的节点变为不可用。

### Scenario: 回收站只读
- GIVEN 文档已进回收站
- WHEN 提交任何写命令
- THEN 返回 409 `creative_state_conflict`；读取仍然成功，默认列表不显示它

### Scenario: 彻底删除
- GIVEN 文档在回收站，并且有画布节点挂着它
- WHEN 摄影师彻底删除它
- THEN 文档及其修订、引用、工作会话指针都被删除，节点读取时显示为不可用；不在回收站的文档不能被彻底删除

## creative/documents: ADD 历史引用受 GC 保护

文档任一修订（包括历史修订）引用的内容都不会被媒体 GC 回收，直到文档被彻底删除。

### Scenario: 只被历史修订引用
- GIVEN 某张图片只被文档的旧修订引用，当前修订已不再引用它
- WHEN 媒体 GC 运行
- THEN 这张图片不被回收；文档彻底删除后，它不再因这份文档被保留

## creative/documents: ADD 账号隔离

文档只对所属账号可见；其他账号的读取、写入、列表、历史都表现为不存在。

### Scenario: 跨账号访问
- GIVEN 账号 A 有一份文档
- WHEN 账号 B 读取、写入或列出 A 的文档或其历史
- THEN 返回 404 `not_found`（列表中不出现），与文档不存在无法区分
