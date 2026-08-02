---
doc_type: feature-design
feature: 2026-08-02-avatar-media-safety-net
requirement: account-center
roadmap: account-center
roadmap_item: avatar-media-safety-net
execution_lane: goal
execution_lane_reason: epic child batch under account-center
status: approved
summary: 中性化头像对象 seam并建立双 key codec、v1/v2 manifest 与 restore 兼容安全网，且不改变任何用户可见客户头像行为
tags: [avatar, media, backup, restore, infrastructure]
---

# avatar-media-safety-net · 头像媒体安全网 design

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| `avatarmedia` | 共享头像对象／校验／inventory／manifest primitives 包 | `backend/internal/avatarmedia`；不是 customer 业务模块 |
| Content decode confirm | 解码确认 JPEG／PNG／WebP、大小／尺寸边界、声明与实际格式一致，**返回原始合规字节** | 共享 primitive；**不得**强制重编码为单一 media type |
| Customer normalize | customer 专属：≤512 边长重采样 + PNG 重编码 | 留在 `customer/avatarimage`（调用共享 decode 之后）；账号头像不用它 |
| Read-path integrity decode | `avatar_application` 读路径 `image.DecodeConfig` 校验 | 同属共享 decode confirm 范围 |
| CustomerKey / AccountProfileKey | 两类 canonical key | roadmap §4.4 |
| typed `Key` / `Prefix` / `Cursor` | ObjectStore 端口参数类型 | 本条采用 typed；adapter 不接受未解析任意相对路径字符串作为权威输入 |
| typed inventory | Local.Inventory → ParseKey 归类 | Inventory 不进最小 ObjectStore port |
| PointerSource | subject-neutral current pointer 列举 | 本条仅 customer 实现 |
| Manifest v1 / v2 | 既有／新 format | 新 CLI generate 只出 v2；内部保留 v1 **投影生成器**供 verify |
| ops package layer | backup／restore compose + `v1-ops-package.py` | 与镜像内 `avatar-manifest` 独立版本化 |
| synthetic profile-like marker | **(1)** 目标 DB 在 `DATABASE_COUNT_TABLES` 已计数字表中多出的一行（推荐 `customers` 或 `settings`）；**(2)** 头像根下物理 `avatars/{account_id}/account-profile/.../content` 对象 | 非产品 migration；整包 restore 后两者都必须不存在 |

## 1. 决策与约束

### 需求摘要

- **做什么**：中性化共享 decode／ObjectStore／inventory／manifest；Go＋Python dual key；ops 双格式；customer-only v2 writer；经**本 feature 自有**真实 restore 路径证明 v1 整包不保留目标 marker。
- **为谁**：`account-profile-center` 与运维；无新用户可见能力。
- **成功标准**：customer 零漂移；双边 dual key；v2 generate＋ops 双格式；破坏前 package-validate；synthetic marker（DB 行＋物理 account-profile 对象）经真实 restore 后均不存在。
- **明确不做**：产品 profile schema／API／UI；改客户公开 API／revision／GC grace／**customer PNG 归一化语义**；mixed v2 writer；OSS／缩略图／匿名直链；选择性 import；**不把账号头像做成 customer 式 PNG 重编码**。

### 复杂度档位

基础设施／兼容迁移默认；偏离：`dual-format fail-closed`、`characterization-first`、`dual-language codec`、`typed ObjectStore keys`。

### 关键决策

- **D1 typed ObjectStore**：端口改为 `PutImmutable(ctx, Key, …)`／`List(ctx, Prefix, Cursor, int)` 等（名称以 implement 为准）；内部可有 string 适配，但公开 port 与 Local 入口以 typed builder 结果为准，对齐 roadmap §4.4。characterization 锁住 customer 行为。
- **D2 跨语言 dual key**：Go 与 `v1-ops-package.py` 各一份唯一 codec；共享已提交 golden（Go 测试负责生成并比对 golden；Python 只消费，不反调 `go run`）。
- **D3 v2 schema**：roadmap §4.4；含重复 JSON key 拒绝；`(account_id,subject_kind,subject_id)` 唯一。
- **D4 v1 reader + 内部投影生成器**：CLI／对外不再 generate v1；**binary 内保留 v1 投影 Generate**（customer-only 视图、排除 account-profile key、`customer_id` 字段）专供 `Verify` DeepEqual；A7 比对对象是该投影。
- **D5 PointerSource**：中性接口；本条仅 customer 实现；不启用 mixed writer。
- **D6 整包 restore 语义已存在**：本条证明非保留，不新建替换语义。
- **D7 restore 阶段序**：

  | 阶段 | 本条态度 |
  |---|---|
  | package-validate（schema／inventory／format） | 已具备骨架；需新增 v1｜v2 dispatch、metadata allowlist+派生、typed dual-key |
  | destructive replace | 不变 |
  | restore-verify（`avatar-manifest verify`） | **仅替换后**；不得前移 |
  | `postgres_server_major` | 沿用现状判据 |

- **D8 strict decode**：未知字段／重复 key／非 canonical `generated_at`／未知 format；Go＋Python 对齐。
- **D9 ops 双格式**：`manifest_schema_version` ∈ allowlist `{customer-avatar-exact-generation-v1, avatar-exact-generation-v2}`，且必须等于实际 `manifest.format`（派生决定选哪个合法值，**不**允许任意自创 format 自洽通过）。
- **D10 content seam 拆分**：
  1. **共享** decode confirm → 原始字节 + 真实 media type（供 customer 与未来 account-profile）；
  2. **customer 专属** normalize（512＋PNG）留在 customer，调用共享层之后；
  3. 读路径 integrity decode 迁入共享层；
  4. `ValidationError` 归属：中性错误类型或 avatarmedia 导出、由 httpapi 映射——characterization 锁客户错误码／文案零漂移。
- **D11 typed inventory**：Local.Inventory 返回 ObjectItem；ParseKey 归类；`.avatar-tmp-*` → integrity（NewLocal 之后再种 tmp，避免被构造清理）。
- **D12 ADR-004 补充**：共享 primitives；主体自有 pointer／GC；双格式；整包 restore；**正向**：ops 脚本须不晚于镜像切 v2；**回滚**：已生成 v2 后须保留新 maintenance／ops binary。附脚本×镜像配对表进 runbook。
- **D13 分支门禁**：实现前离开受保护 `main`（或 owner 书面授权检出环境）。
- **D14 A17 harness（本 feature 自有，不改 v1-hardening 冻结 catalog）**：
  - 新增 `scripts/test-avatar-media-v1-restore-wipe.sh`（名以 implement 为准），内部调用真实 `restore-compose.sh`／`v1-ops-package.py`；
  - **不**把 A17 塞进 `v1-hardening-ops-case-catalog.yaml`（避免跨 feature 冻结产物与 `restore-success`×v2 generate 的 manifest_sha256 陷阱）；
  - Marker：目标在 restore 前 (1) 于已计数字表插入多余行；(2) 写入物理 account-profile generation；
  - 断言：restore 成功后多余行不在（`database_counts` 与 v1 包一致即可机械证明）；物理 `avatars/*/account-profile/**` 不存在；**不**要求 after 的 v2 generate manifest 摘要等于 v1 包摘要；
  - A16：package-validate／restore 入口坏包 → 目标未改（selftest + safety 脚本即可）。
- **D15 可选加固**：本条可顺手给 `v1_ops_results.py` 补 `oracle_class` allowlist（unknown 拒绝），保护 hardening 日后扩展；**非** A17 前置依赖。

### 执行风险与证据计划

- **Top 3**：① ops 旧脚本×新镜像静默拒备份 → D9/D12 顺序；② Go／Python 漂移 → golden；③ A17 假 harness → D14 自有脚本+物理断言。
- **必跑命令**：见 checklist `dod.commands`。
- **范围守护词表**：`account_profiles`、`account_profile_avatar_gc`、`/api/v1/account/profile`、`AccountCenterLayout`、`backend/internal/accountprofile`、`frontend/src/account`；例外仅 synthetic fixture 路径并记证据。
- **清洁度**：禁止调试输出、临时 TODO、死代码、PII、产品 profile 泄漏。

## 2. 名词与编排

### 2.1 名词层

**现状**：`avatar_store.go`（**interface-based `AvatarObjectStore`，key/prefix 仍为 string 类型**）；`AvatarObjectKey`／`ParseAvatarObjectKey`；`avatarstore/local.go`；`avatarimage`（**强制 PNG 归一化**）；读路径 `avatar_application.go` DecodeConfig；`avatarbackup` v1＋`PointerLister→[]Customer`；`cmd/avatar-manifest`；`httpapi/customer_avatar*`；ops：`v1-ops-package.py`（硬编码 v1 metadata＋customer-only key）、`backup-compose.sh`、`restore-compose.sh`、selftest、smoke-runner＋**冻结** `v1-hardening-ops-case-catalog.yaml`／`v1_ops_results.py`。（D1 的 typed Key 方向不受影响；中性化主要工作是参数类型化＋Inventory 归类，而非新建接口。）

**变化**：`avatarmedia` typed port＋共享 decode；customer 保留 normalize；Go＋Python dual key；v2 writer＋v1 内部投影；ops allowlist+派生；**本 feature wipe 脚本**；PointerSource；不改 hardening catalog。

**接口示例**

```text
DecodeConfirm(raw) → {bytes: original, mediaType: jpeg|png|webp} | validation_failed
CustomerNormalize(confirmed) → png≤512  # customer only
ParseKey / BuildCustomerKey / BuildAccountProfileKey
ObjectStore.PutImmutable(ctx, Key, Content, Meta)
PointerSource.ListCurrent(...)
GenerateV2(customerSource, inventory) → v2
ProjectV1ForVerify(...) → v1 shape  # internal only
metadata.manifest_schema_version ∈ {v1,v2} && == manifest.format
```

### 2.2 编排层

分支门禁 → ADR(D12) → characterization/golden → 中性化(decode+typed store+local) → dual key 双边 → manifest/PointerSource/v1 投影 → ops 双格式 → **wipe 脚本 A16/A17** → runbook（含脚本×镜像顺序）→ 回归。

### 2.3 挂载点（含卸载）

1. `backend/internal/avatarmedia`（新）  
2. customer composition／avatarimage 调用共享 decode（改）  
3. `avatarbackup`／`cmd/avatar-manifest`（改）  
4. `scripts/lib/v1-ops-package.py`（改）  
5. `backup-compose.sh`／`restore-compose.sh`（改／消费）  
6. `scripts/test-avatar-media-v1-restore-wipe.sh` + selftest 扩展（新，本条主 harness）  
7. `README` 备份恢复＋配对表（改）  
8. items.yaml 指针（改）  
9. **可选** `scripts/v1_ops_results.py` allowlist（改，D15）  
10. **明确不挂载**：不修改 `v1-hardening-ops-case-catalog.yaml` 作为 A17 前置  

卸载：撤接线／删 wipe 脚本／ops 回 v1-only（理论）；customer normalize 行为由 golden 保护。

### 2.4 / 2.5

推进见 checklist STEP-000…008。结构结论：微重构进 `avatarmedia`；customer normalize 不搬进共享层。

## 3. 验收契约

| ID | 触发 | 期望 | 证据 |
|---|---|---|---|
| A1 | customer+httpapi 测试 | 零漂移 | CMD-001 |
| A2 | customer key／v1 manifest vs golden | 字节相同 | golden |
| A3 | account-profile key 往返 | subject_id=account_id | Go+Py |
| A4 | 非法 key | fail closed | Go+Py |
| A5 | Inventory 两类 key | 稳定列举 | unit |
| A6 | `avatar-manifest generate` | 仅 v2 customer-only | CLI |
| A7 | v1 verify | 用内部投影比对成功；不写回 v1 文件 | fixture |
| A8 | v1 视图见 account-profile 物理 key | 排除；未知 key 失败 | unit |
| A9 | 缺字段／额外／重复 key／坏 generated_at | fail closed | Go+Py |
| A10 | 同一 golden → Go 与 Python | 接受集一致 | CMD-002 |
| A11 | Inventory 遇新鲜 `.avatar-tmp` | integrity（NewLocal 后种） | unit |
| A12 | backup 产出 v2 包 validate | 通过；metadata 为 v2 | CMD-002/003 |
| A12b | v2 verify 对象级完整性：current pointer 缺对象／元数据不一致／主体不匹配／inventory 多或少对象 | 全部 fail closed（v1 视图同规则） | CLI+fixture |
| A13 | 历史 v1 包；伪造／非 allowlist format | v1 过、伪造拒 | CMD-002 |
| A14 | 坏包走 package-validate／restore 入口 | 目标未改 | CMD-003 + wipe 预检 |
| A15 | wipe 脚本：目标多种 DB 行 + 物理 account-profile 对象后 restore v1 包 | counts=包；物理 account-profile 路径不存在；不比对 v2 generate≡v1 manifest 摘要 | CMD-004 → `restore_fixture_log` |
| A16 | 范围 grep（分支+未跟踪） | 无产品 profile 泄漏 | CMD-005 |
| A17 | README／runbook | v1/v2、正向脚本≤镜像、回滚保留新 binary、配对表 | diff |
| A18 | 共享 decode 不强制 PNG；customer normalize 仍 PNG | 单测+characterization | CMD-001 |

### Coverage Matrix

| 成功标准 | 场景 | checks |
|---|---|---|
| customer 零漂移 | A1 A2 A11 A18 | CHK-010 CHK-018 |
| dual key 双边 | A3 A4 A5 A10 | CHK-002 CHK-005 |
| v1/v2 + ops | A6–A9 A12 A12b A13 | CHK-003 CHK-004 CHK-006 CHK-007 CHK-014 CHK-019 |
| restore 安全网 | A14 A15 | CHK-008 CHK-009 |
| 范围／文档／ADR／分支 | A16 A17 | CHK-012 CHK-013 CHK-015 CHK-016 CHK-017 |

### DoD

全部核心 CMD 绿；ADR＋分支门禁完成；无产品 profile；checks 可勾选。

## 4. 架构关系

ADR-001／002／004(D12)、compound、roadmap §4.4；account-profile 不得 import customer；告知 profile-center：`DATABASE_COUNT_TABLES` 引入新表须同步（本条不改清单）。
