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

种子包在仓库根的 `deploy/creative-skills/reference-direction.v1/`。它在 Go 模块之外，因为已经没有任何代码编译它——服务端从数据库读 Skill，这个目录只给导入命令读。镜像里这份目录在 `/usr/local/share/creative-skills/`：包不再随二进制发布之后，它必须作为数据被显式带进镜像。

## 导入

下面两条命令都在 `backend/` 下跑（Go 模块在那里），而包目录在仓库根，所以路径要往上走一级：

```bash
cd backend
go run ./cmd/creativectl skill import \
  --package-dir ../deploy/creative-skills/reference-direction.v1 \
  --account "$CREATIVE_PLATFORM_ACCOUNT_ID" \
  --operation-id 4d8f4d0e-7a2b-4c6d-9f31-5eed00000001 \
  --origin platform --activate
```

镜像里不存在这个错位：`creativectl` 是 `/usr/local/bin/` 里的二进制，包在 `/usr/local/share/creative-skills/reference-direction.v1`，直接给绝对路径。

| 参数 | 说明 |
|---|---|
| `--package-dir` | 包目录，相对当前工作目录解析 |
| `--account` | 发布账号 |
| `--operation-id` | **必填 UUID**。重跑同一个 id 就是重放，返回它已经冻结的那个版本；换内容不换 id 会被判为冲突 |
| `--origin` | `account`（默认）或 `platform` |
| `--expected-revision` | 给已存在的 Skill 追加版本时必填，取上一次成功回执里的 `skill_revision`；新建 Skill 留空。它不是 `version_number`——Skill 的任何改动都会推进 revision，不只是新版本 |
| `--activate` | 把 Skill 的推荐版本指针移到新版本。已有消息和运行仍固定各自的旧版本 |

`--origin platform` 只认配置里的 `CREATIVE_PLATFORM_ACCOUNT_ID`：哪个账号持有平台目录是部署事实，不能由一个打错的 flag 决定。未配置该变量时该命令直接拒绝（退出码 3），不会退而求其次挑一个账号。这道闸门同时立在 `creativeskill.BeginImport` 里——它是账号隔离的一部分，不该取决于是哪个二进制在调。

**种子导入的 operation id 是部署里钉死的字面量**，上面那个 UUID 就是。每次发布都换一个新 id 会让同样的内容每次都多冻结一个版本。

退出码：`0` 成功 / `1` 内部错误 / `2` 输入不合法（含 `--expected-revision` 与 Skill 现状不符）/ `3` 未就绪（平台账号未配置、对象存储不可用、导入过窗、sweep 部分失败）/ `4` 冲突（operation id 已用于别的内容、或 Skill 已被其他发布抢先移动）。输出是一行 JSON。

`4` 只表示**别人先改了**，重试有意义；「新建 Skill 却填了 `--expected-revision`」和「追加版本却没填」都是 `2`，因为原样重试永远不会成功。

有一种 `4` 不是别人先改了：**manifest 的 JSON 编码是持久化契约**。重放靠的是 `request_hash`，而它由 manifest 的编码字节算出——改 `Manifest` 结构体、改 json tag、或在算 digest 前对字段做任何"规范化"，都会让同一个包算出不同的哈希，于是钉死的种子 operation id 永远报冲突且重试无用。`TestTheManifestEncodingIsAPersistedContract` 把这份编码钉在一个字面 digest 上，就是为了让这种改动必须是一次决定而不是副作用。API 要求的数组形状只在**读出时**补齐（`decodeManifest`），不参与哈希。

## 回收

失败或过窗的导入会留下没人引用的对象。**本阶段没有任何定时任务在扫 `expires_at`**，回收就是显式跑这条命令：

```bash
cd backend
go run ./cmd/creativectl skill reclaim --account <id> --limit 50
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
| `CREATIVE_PLATFORM_ACCOUNT_ID` | 受信平台发布账号；留空 = 本部署没有平台目录 |

oss 驱动沿用 `OSS_*`（bucket 需开启版本控制，见 `docs/dev/object-storage.md`），对象写在 `creative-skills/{account}/imports/{importID}/` 前缀下。
