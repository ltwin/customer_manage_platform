# 拍摄策划备份、恢复与发布证据

本文是拍摄策划生产形态备份与恢复的 operator runbook。脚本只支持本机 Unix Docker endpoint、
compose 托管 PostgreSQL、唯一的 app/postgres 容器以及唯一的 `pgdata`、`avatar_data`、
`planning_media_data` named volume。远程 Engine、外部 PostgreSQL、对象存储和生产切换需要独立方案与授权。

## 包格式与工具配对

历史 schema-v1 包严格包含五个 regular file：

```text
database.sql
avatar-volume.tgz
avatar-manifest.json
metadata.json
SHA256SUMS
```

拍摄策划 schema-v2 包严格包含七个 regular file：

```text
database.sql
avatar-volume.tgz
avatar-manifest.json
planning-media-volume.tgz
planning-media-manifest.json
metadata.json
SHA256SUMS
```

新 reader 接受 v1 和 v2；旧 reader 必须拒绝 v2。产生过 v2 包或启用过 planning media 后，回退应用也必须保留
与该包配对、理解 schema-v2 的 restore tool/image。禁止 schema down，也禁止用 v1 包声称完整备份已经包含的
planning media。

operator 提供的 deployment identity 是无凭证的 strict JSON，字段集合固定为：

```json
{
  "environment_id": "production-shaped-non-production",
  "deployment_id": "deployment-generation",
  "compose_project": "crm-project",
  "storage_layout_version": "planning-media-v1"
}
```

identity 必须保存为不可被非 owner 修改的 regular file。备份把 identity 和 app/postgres/restore-tool image digest、
compose SHA-256、PostgreSQL major 绑定进 metadata；恢复要求与目标完全匹配。

## 创建 schema-v2 备份

输出必须是尚不存在的新目录，父目录应位于受控、加密并有 retention 的介质：

```bash
./scripts/planning-backup-compose.sh \
  --env-file /etc/crm/env \
  --compose-file ./docker-compose.yml \
  --docker-context local-production \
  --project-name crm-prod \
  --deployment-identity /etc/crm/planning-deployment-identity.json \
  --output /srv/crm-backups/2026-08-18T020000Z
```

脚本在同一 operation lock/fence 下冻结写入，复用 v1 数据库和头像 exact package，再生成 planning-media tar 与
manifest，strict validate 后以 no-replace 方式发布。任何失败都不发布半包；原 app 为 running 时才恢复运行状态。

## 恢复前检查与 exact restore

恢复会替换 PostgreSQL、头像卷和 planning-media 卷。`--target-generation` 必须是目标 identity 当前
`deployment_id`，`--confirm-project` 必须逐字等于 compose project：

```bash
./scripts/planning-restore-compose.sh \
  --env-file /etc/crm/env \
  --compose-file ./docker-compose.yml \
  --docker-context local-production \
  --project-name crm-prod \
  --input /srv/crm-backups/2026-08-18T020000Z \
  --confirm-project crm-prod \
  --deployment-identity /etc/crm/planning-deployment-identity.json \
  --target-generation deployment-generation
```

destructive mutation 前，preflight 必须全部通过：

- strict package schema、成员、文件类型、SHA-256、database dump、avatar/planning manifest 和 tar 安全；
- deployment identity、compose、app/postgres/restore-tool digest、PostgreSQL major 和 generation fence；
- 私有 staging、数据库卷、头像卷和 planning-media 卷的最小可用空间；
- 只读采集的目标数据库逐表 counts、只读头像 manifest 与 planning-media inventory；
- v1 包面对非空 planning-media target 时拒绝；v2 允许替换当前已损坏的 target，但不能放宽 source package 校验。

通过后阶段顺序固定为：

```text
preflight -> stop_writes -> replace -> migrate -> validate -> reopen
```

`replace` 必须先完成 PostgreSQL、avatar 与 planning-media 三类资源；schema-v1 同样运行 forward migration。
`validate` 是 migration 后唯一权威成功判据，必须重新核对 package 中的数据库逐表 counts、avatar exact manifest，
并对 schema-v2 核对 planning-media exact manifest（schema-v1 则核对目标 planning-media 仍为空）。replace 中的
防御性检查不能代替这组三资源最终 oracle，也不能据此提前 reopen。

进入 replace 后任一 DB、volume、migration 或 exact oracle 失败，终局必须是 `failed_restore_stopped`，app/writers
保持 Engine exited。不要手工跳到 reopen，也不要从混合时间线状态继续；保留日志，修复原因后从完整 preflight
重新运行。只有原 app 正常 running 且所有 oracle 通过时才重新开放。

## Production-shaped rehearsal

真实 rehearsal 只可在 owner 批准的隔离、非生产、production-shaped 部署执行。普通 restore wrapper 不生成
rehearsal evidence。先准备权限为 `0700` 的私有 evidence 目录、deployment identity，以及一个 owner 审核过的
executable hook。hook SHA-256 和允许执行的 UTC 时间窗必须写入 strict config：

```json
{
  "schema": "planning-restore-rehearsal-config-v1",
  "environment_class": "production_shaped_non_production",
  "owner_approval_ref": "approval-system:reference",
  "approval_starts_at": "2026-08-18T00:00:00Z",
  "approval_expires_at": "2026-08-18T04:00:00Z",
  "deployment_identity": {
    "path": "/absolute/path/deployment-identity.json",
    "sha256": "<64 lowercase hex>"
  },
  "hook": {
    "path": "/absolute/path/approved-rehearsal-hook",
    "sha256": "<64 lowercase hex>"
  },
  "expected_image_digests": {
    "app": "sha256:<64 lowercase hex>",
    "postgres": "sha256:<64 lowercase hex>",
    "restore_tool": "sha256:<64 lowercase hex>"
  }
}
```

wrapper 先把批准的 deployment identity 复制到私有、fsync 完成的只读快照，并生成只指向该快照的冻结
execution config，再以 `--config <frozen-config> --result <temporary-result>` 调用已按批准 hash 复制的 hook。
hook 结束后原 identity 路径与快照的 regular-file identity 和 bytes/hash 都必须保持不变。hook 必须在结果路径生成 strict
`planning-restore-rehearsal-v1` JSON，证明固定六阶段、DB/avatar/planning-media 三个 passed oracle、一个
preflight-before-mutation rejection 和一个 destructive `failed_restore_stopped` case。单纯 exit 0、synthetic
environment、错误 digest、空 oracle 或错误阶段顺序全部拒绝。

```bash
./scripts/rehearse-planning-backup-restore.sh \
  --config /secure/planning-rehearsal-config.json \
  --evidence /secure/planning-rehearsal-evidence
```

只有验证通过的结果才以 `0600`、no-replace 方式发布为 `planning_restore_rehearsal.json`。脚本不执行 release、
deploy、promotion 或 cutover。

## Release readiness

readiness JSON 与下列 sibling artifact 必须位于同一私有 evidence 目录：

```text
planning_v1_dependency_conformance.json
planning_v1_e2e_results.json
planning_v1_failure_matrix.json
planning_v1_h1_h2_negative_matrix.json
planning_v1_prototype_responsive_a11y_matrix.json
planning_v1_evidence_index.json
planning_restore_rehearsal.json
planning_restore_package/
planning_v1_additive_schema_readiness.json
planning_v1_marker_reader_readiness.json
planning_v1_old_processes_exited.json
planning_v1_trusted_readiness_inventory.json
planning_v1_planningctl_readiness.json
planning_v1_archive_cas_readback.json
planning_v1_share_reminder_writers.json
planning_v1_schema_v2_retention.json
planning_v1_release_readiness.json
```

`planning_restore_package/` 必须是 rehearsal 实际使用的完整 schema-v2 package，不是摘要或另写的 inventory。
verifier 会 strict validate 七个成员，重算覆盖全部文件名与 bytes 的 package SHA-256，并把 metadata 中的
app/postgres/restore-tool digest 与 `planning_restore_rehearsal.json` 逐项绑定。

stage evidence 不允许从私有 evidence 目录任意指定 sibling。`planning_v1_evidence_index.json` 的 `json_path`
必须是无 `..` 的 repository-relative path，并精确解析到 canonical `approval-report.md` 经
`planning-evidence-dispatch-gate.py` 返回的 roadmap `evidence/` 文件；绝对路径、目录逃逸、index 内自述 approval、
path/hash/gate-version 任一不一致都拒绝。当前 `stage-1-evidence-go` 或 `stage-2-evidence-go` 仍为 `pending` 时，
即使 synthetic bundle 内全部写成 passed，release verifier 也必须非零退出。

`planning_v1_planningctl_readiness.json` 必须保存 release-only `planningctl archive-capability readiness` 的原生
JSON 输出，不得手写替代。verifier 按 Go `readinessOutput` exact fields 重算内层 digest，并把
environment/deployment、schema hash、trusted inventory bytes hash、生成/过期时间、current marker/revision、target、
live builds、wiring 与 control-plane provenance 绑定到其他 sibling。随后
`planning_v1_archive_cas_readback.json.readiness_digest` 必须等于该 digest，证明 CAS/readback 使用的是同一 readiness
frame；生成 readiness 不授权执行 `promote`，实际 promotion 仍需独立 owner 授权。

verifier 重新计算所有 sibling SHA-256，并绑定当前 Git HEAD、roadmap bytes、`api/openapi.yaml`、migration head、
完整 conformance/E2E/failure/H1-H2/17 场景 responsive coverage、canonical stage evidence approval、真实 rehearsal
package、planningctl readiness digest、archive adjacent CAS/readback 和 rollback v2 restore-tool retention。
`required_checks` 的每一项必须按固定 ID 顺序
携带对应 sibling SHA-256；只写 `status: passed` 不构成证据：

```bash
./scripts/verify-planning-v1-release-readiness.sh \
  --evidence /secure/planning-evidence/planning_v1_release_readiness.json \
  --json
```

exit 0 只表示证据满足发布条件，不授权或执行 remote push、merge、publish、release、deploy、promotion、
production restore 或 cutover。stage evidence pending、manual accessibility 检查未完成、failure matrix 有 deferred、
artifact bytes 漂移或 production-shaped rehearsal 未获授权时必须保持 blocked。

## 生产侧保留证据

脚本无法替代 ECS/runtime mount、owner/permission、容量、write/fsync/dir-sync、凭证管理、PostgreSQL TLS、
网络边界、异地加密备份、对象存储持久性和 restore-tool retention 的云侧证据。生产动作前由 owner/operator
单独留存这些证明，并确认备份中的客户数据和媒体符合 retention、销毁与恢复后的隐私处理要求。
