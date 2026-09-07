# 创意空间先导实现

产品契约见 `.codestable/epics/creative-workspace-redesign.md`，当前进度见对应 work 游标。界面称「创意空间」，内部 `creativeworkspace` 是独立业务域，不复用旧策划状态机、CRM 联动或经营规则。

## 入口与边界

- 前端：`/creative-workspaces`、`/creative-workspaces/:id`，`?view=live` 为现场模式。旧 `/shoot-plans*` 路由保留；准入后只读。
- API：`/api/v1/creative-pilot` 与 `/api/v1/creative-workspaces*`，以 `api/openapi.yaml` 为机器契约。HTTP 层负责输入/封套与幂等编排，业务状态与账号隔离在领域与 store 层。
- `0036_creative_workspace` 是新增迁移，不改写旧数据。卡片账号持有；成员关系承载空间、顺序、分组；图片使用 `creative/` 不可变命名空间；拍摄项复制内容并保留主来源回跳；执行事实追加保存。
- `CREATIVE_WORKSPACE_PILOT_ACCOUNTS` 是逗号分隔的允许准入账号 ID。默认空值不允许新账号自行开启。已准入账号不因配置移除而转换 writer，需显式 stop；测试 fixture 只允许其固定合成账号。

## 切换与恢复

准入 preflight 只读；Enroll 在事务级账号锁之后重新执行清场谓词再写 capability。旧 mutation 的标记 scope 在每次业务事务开始后取同一锁并重读状态，新写与 Stop 使用相同 barrier。订单对旧终态历史的内部级联保持原路径。

`pilot_new_write → stopped` 保留全部创意空间只读，并保持旧历史只读；本轮没有自助重新开启入口。归档空间只能执行单独的恢复动作，不能夹带改名、关联或内容修改。迁移 down 是有损退回空结构的开发工具，不是产品停止或恢复方案；已有创作数据应采用 forward fix 或备份恢复。

图片上传已知回滚时用独立的有界 context 清理；提交回执不明时不删除对象，避免破坏可能已经持久化的资产。此类对象保留到停止写入后与数据库快照核对；没有自动删除孤立对象的路径。

本地备份沿用 `planning-media-manifest` 与 schema-v2 备份包，清单与恢复校验同时覆盖 `planning/` 和 `creative/`，不把新资产纳入旧媒体 GC。旧版本校验器不识别新命名空间，恢复含创意素材的包必须使用本版本或更新版本。数据库快照与整个 planning-media volume 必须配套恢复；OSS 仍沿用项目既有独立备份限制，不将本地验证宣称为 OSS 恢复证据。

## 验证

- 后端：`make check-go`；更窄开发检查可选 creativeworkspace、platform/httpapi、planningmedia 包。
- 前端：`make check-frontend`；契约：`make generate-check`；备份脚本：`make check-ops`。
- 浏览器：设置临时目录 `CREATIVE_BROWSER_FIXTURE`，运行 `go test ./internal/platform/httpapi -run '^TestCreativeBrowserPreview$' -count=1 -timeout=20m`。它通过 storetest 创建隔离真库，在 127.0.0.1:18089 提供测试 API，并把短期 fixture token 写入该目录的受限文件。Vite 的 /api 代理指向该地址后运行 `frontend/scripts/creative-workspace.e2e.mjs`。结束时 POST 测试服务 `/__end`，fixture 与数据库被回收。
- 浏览器脚本只为测试认证提供 refresh 与 me fixture；所有创意空间、图片、订单、备忘与执行写入均走真实 API/数据库。截图覆盖桌面、375px、断网与失败重试；这些合成记录不得进入真实 pilot cohort。

## 真实项目证据尚未完成

`creative_observation_events` 只保留 space_open、live_open、live_unverified 原始观察；在线时间由服务端覆盖，离线补报明确未核验。它不接受 live=true，也不自行生成 go 结论。

正式试点前按 Epic 冻结 enrollment、业务资格、连续项目 ledger、版本化拍摄窗口、提前终止条件与参与者。真实项目需要摄影师登记与回访，不能由自动测试替代。至少五个项目后的可重算报告、owner disposition、旧 Epic 终态处置和部署授权仍按 ITEM-5 / 最终 gate 处理。
