# Skill 受信导入（creativectl）

Skill 的权威正文与资源现在落在 PostgreSQL 与平台对象存储里（FND-07 S1）。发布是一次部署动作，不是摄影师的请求：**没有任何 HTTP 端点能创建版本**，`creativectl` 是唯一入口。

语义权威源：`docs/product/creative-canvas-system/modules/skill-foundation.md` §6（导入协议）、§9（保留与回收）。本文只讲怎么运行。

## 包目录格式

一个包就是一个目录，操作者能看到什么，导入就发布什么：

```
reference-direction.v1/
├── manifest.json                      # 声明（严格解析，多一个字段就报错）
├── SKILL.md                           # 正文
└── references/comparison-checklist.md # 其余文件全部是声明资源
```

- 除 `manifest.json` 与 `SKILL.md` 外，目录里的**每个**文件都会成为该版本的声明资源，路径就是相对路径。没有「目录里有但没被声明」这种状态。
- `manifest.json` 里的 `version` 字段是旧内嵌包格式留下的，**读取但不采用**：版本号由服务端在 Skill 行锁下分配，文件里写的数字不可能是权威。
- 隐藏文件（`.` 开头）、符号链接、空文件一律**报错**而不是跳过。`.DS_Store` 这种操作者看不见的东西，要么变成一个永不删除的冻结版本的声明资源，要么在首次导入后改变 digest、把钉死的 operation id 烧掉；报错的代价只是一次 `rm`。
- 本阶段**只收 UTF-8 文本**。判定看字节，不看扩展名也不看 mime——两者都由导入方自己写，靠它们把关等于没关。图片、视频等资源类型按 `skill-foundation.md` §4.3 留给后续媒体能力。
- 上限：单文件 64 KiB、至多 20 个文件、整包 256 KiB。读取时按目录报告的大小先判再读，所以 `--package-dir` 指错地方的代价是一次 stat，不是把那个目录读进内存。

内置 Skill 包放在仓库根的 `deploy/creative-skills/`，一个子目录一个包（现在只有 `reference-direction.v1/`）。它在 Go 模块之外，因为已经没有任何代码编译它——服务端从数据库读 Skill，这个目录只给同步和导入读。镜像里这份目录在 `/usr/local/share/creative-skills/`：包不再随二进制发布之后，它必须作为数据被显式带进镜像。

## 内置 Skill 自动同步

服务每次启动（migrate 之后、开始监听之前）都会把 `CREATIVE_SKILL_PACKAGES_DIR` 下的包同步进平台目录，所以**加或改内置 Skill 只需要改 `deploy/creative-skills/` 并发布**，不用再手动跑导入：

- 包内容与该 Skill 当前版本的 digest 相同 → 跳过；不同（含新包）→ 追加一个版本并设为当前版本。旧版本保留，已有消息和运行仍固定各自的版本。
- operation id 由 slug、内容 digest、被追加的 revision 和尝试序号推出，不用手填：同步中途失败，下次启动重放同一个 id，不会多冻结一个版本；把包改回旧内容也会正常发布成新版本。失败的尝试拖过 24 小时导入窗口就成了终态，下次启动换下一个序号重新导入（单次同步最多跨过 32 个过期尝试）。
- 两个目录声明同一个 slug（比如 `a.v1` 和 `a.v2` 同时留着）时两个都不发布，日志里报出这几个目录名。否则它们会轮流成为当前版本，每次启动都多冻结版本。升级一个内置 Skill 就地改它的目录，或者删掉旧目录。
- 一个包读不出或发布失败不影响其它包，也**不阻断启动**：日志里是 `platform skill sync incomplete`（带 `published` / `unchanged` / `failed` 与第一个错误），成功是 `platform skills synced`。
- 从目录里删掉的包**不会**被下架。撤回平台 Skill 牵涉已固定到它的工作，要单独决定。
- `CREATIVE_SKILL_PACKAGES_DIR` 没值就跳过并记一条 `platform skill sync skipped`。镜像已设 `CREATIVE_SKILL_PACKAGES_DIR=/usr/local/share/creative-skills`；本机在 `backend/` 下跑 server 时设成 `../deploy/creative-skills`。`.env` 里不要写空的 `CREATIVE_SKILL_PACKAGES_DIR=`，空值会盖掉镜像里的默认。
- 以前手动导入过种子包的部署，首次同步时内容相同，直接跳过。

`go test ./internal/creativeskill/ -run TestSyncPlatform` 会把仓库里每个包实际读一遍、发布一遍，包写坏了在 CI 就会失败。

## 平台归属

内置 Skill 不属于任何账号。Skill 相关表的 `account_id` 列对它们填保留值 `platform`（`creativeskill.PlatformOwner`）：它不是账号，不能登录，生成的账号 id 是十六进制串或 UUID，撞不上它；写在代码里而不是配置里，因为内置 Skill 归谁不是部署能选的事。所有账号的 Skill 目录都会并入这份平台目录（只取 `origin='platform'` 的）。

谁能往平台目录发布由入口决定：现在是启动同步和 `creativectl --origin platform`，以后是管理员。迁移 `0067` 把早先挂在 `CREATIVE_PLATFORM_ACCOUNT_ID` 账号下的平台 Skill（连同版本、资源、导入记录，以及消息和运行上记录的归属）挪到了 `platform`；该账号自己的 Skill 不动。这个变量已删除，`.env` 里留着也不再生效。

## 手动导入

下面的命令用于账号自己的 Skill，或需要不经启动同步就发布的场合。

下面两条命令都在 `backend/` 下跑（Go 模块在那里），而包目录在仓库根，所以路径要往上走一级：

```bash
cd backend
go run ./cmd/creativectl skill import \
  --package-dir ../deploy/creative-skills/reference-direction.v1 \
  --operation-id 4d8f4d0e-7a2b-4c6d-9f31-5eed00000001 \
  --origin platform --activate
```

镜像里不存在这个错位：`creativectl` 是 `/usr/local/bin/` 里的二进制，包在 `/usr/local/share/creative-skills/reference-direction.v1`，直接给绝对路径。

| 参数 | 说明 |
|---|---|
| `--package-dir` | 包目录，相对当前工作目录解析 |
| `--account` | 账号 Skill 的归属账号，`--origin account` 时必填；平台 Skill 不属于账号，填了会被拒绝 |
| `--operation-id` | **必填 UUID**。重跑同一个 id 就是重放，返回它已经冻结的那个版本；换内容不换 id 会被判为冲突 |
| `--origin` | `account`（默认）或 `platform` |
| `--expected-revision` | 给已存在的 Skill 追加版本时必填，取上一次成功回执里的 `skill_revision`；新建 Skill 留空。它不是 `version_number`——Skill 的任何改动都会推进 revision，不只是新版本 |
| `--activate` | 把 Skill 的推荐版本指针移到新版本。已有消息和运行仍固定各自的旧版本 |

`--origin platform` 一律发布到平台归属，不接受 `--account`；`--origin account` 也不能把 `platform` 当账号填。「只有平台归属能发布 platform」这道闸门同时立在 `creativeskill.BeginImport` 里，不取决于是哪个二进制在调。

手动导入的 operation id 要钉死成字面量，上面那个 UUID 就是早先种子导入用的。每次发布都换一个新 id 会让同样的内容每次都多冻结一个版本。

退出码：`0` 成功 / `1` 内部错误 / `2` 输入不合法（含 `--expected-revision` 与 Skill 现状不符）/ `3` 未就绪（对象存储不可用、导入过窗、sweep 部分失败）/ `4` 冲突（operation id 已用于别的内容、或 Skill 已被其他发布抢先移动）。输出是一行 JSON。

`4` 只表示**别人先改了**，重试有意义；「新建 Skill 却填了 `--expected-revision`」和「追加版本却没填」都是 `2`，因为原样重试永远不会成功。

有一种 `4` 不是别人先改了：**manifest 的 JSON 编码是持久化契约**。重放靠的是 `request_hash`，而它由 manifest 的编码字节算出——改 `Manifest` 结构体、改 json tag、或在算 digest 前对字段做任何"规范化"，都会让同一个包算出不同的哈希，于是钉死的种子 operation id 永远报冲突且重试无用。`TestTheManifestEncodingIsAPersistedContract` 把这份编码钉在一个字面 digest 上，就是为了让这种改动必须是一次决定而不是副作用。API 要求的数组形状只在**读出时**补齐（`decodeManifest`），不参与哈希。

## 回收

失败或过窗的导入会留下没人引用的对象。**本阶段没有任何定时任务在扫 `expires_at`**，回收就是显式跑这条命令：

```bash
cd backend
go run ./cmd/creativectl skill reclaim --account <id> --limit 50
# 平台目录的导入
go run ./cmd/creativectl skill reclaim --platform --limit 50
```

顺序是有意义的：每个导入先在行锁里标记 `expired`，然后才删对象。标记过的导入永远无法再冻结版本（`FinalizeImport` 查同一个窗口、同一把锁），所以不存在「删完对象，finalize 却刚好成功」的窗口。正式版本一律不碰——版本的保留机制就是不删。

一次 sweep 不会因为某个导入卡住就停在那里：走完全部候选，最后报 `failed` 计数与第一个原因，退出码 3、`status: partial`。卡住的导入保留自己的对象清单，下次继续。

候选按 `updated_at` 从旧到新取一页，而不是按 `expires_at`。每次尝试都会在行锁里写 `updated_at`，所以删不掉的导入会自己往后排；按到期时间排的话，攒够一页删不掉的导入就能把后面所有导入永远挡在页外。

最典型的卡法是**换过存储**：驱动或 bucket 与暂存时记录的不一致就跳过并计入 `failed`。换驱动（oss 暂存、切 local 回收）只是删不掉；**换 bucket 更危险**——删一个不存在的对象不算错，删除会"成功"，清单随即被清空，原 bucket 里的字节从此没有任何记录。所以这两项都必须对上才动手。

已知边界：回收只处理导入**记录在案**的对象。如果 staging 在上传成功、记录事务尚未提交时进程就死了，那几个字节不在列表里，当前对象端口也没有按 key 前缀枚举的能力，因此这条命令找不到它们。真正的兜底随 FND-10 的通用到期回收一起上。

## 相关配置

| 变量 | 说明 |
|---|---|
| `CREATIVE_SKILL_LOCAL_ROOT` | local 驱动的资源根目录；缺省为 `AvatarLocalRoot` 同级 `creative-skills`。与 `creative-media` 分开，因为媒体对自己的根跑过期清扫 |
| `CREATIVE_SKILL_PACKAGES_DIR` | 内置 Skill 包目录，启动时同步；镜像默认 `/usr/local/share/creative-skills`，留空不同步 |

oss 驱动沿用 `OSS_*`（bucket 需开启版本控制，见 `docs/dev/object-storage.md`），对象写在 `creative-skills/{account}/imports/{importID}/` 前缀下。
