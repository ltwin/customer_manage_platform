# 对象存储（本地卷 / 阿里云 OSS）

本节记录头像与拍摄策划接入时的基线：客户与账号头像（`avatars/` 命名空间）、拍摄策划素材（`planning/` 命名空间）。两者都走不可变对象端口（`avatarmedia.ObjectStore` / `immutablefs.ObjectStore`），下载一律经 API 回环流式转发（无公开直链）；本期 OSS 接入不改变任何 API 契约与前端行为。

## 驱动开关

`AVATAR_STORAGE_DRIVER`（env，默认 `local`）统管两类对象存储：

| 取值 | 头像 | 策划素材 | 配置要求 |
|---|---|---|---|
| `local` | `AVATAR_LOCAL_ROOT` 目录 | `PLANNING_MEDIA_LOCAL_ROOT`（缺省 = AvatarLocalRoot 同级 `planning-media`） | 挂载探针（production） |
| `oss` | 共享 bucket `avatars/` 前缀 | 同 bucket `planning/` 前缀 | `OSS_REGION`、`OSS_BUCKET` 必填；`OSS_ENDPOINT`、`OSS_USE_CNAME` 可选 |

oss driver 下本地根目录与挂载探针全部跳过；启动时对 bucket 做一次 `GetBucketInfo` 探活，不可达即启动失败（fail fast）。

## 凭证（硬规则：只经环境注入）

SDK 凭证链顺序：`OSS_ACCESS_KEY_ID` / `OSS_ACCESS_KEY_SECRET` 环境变量 → ECS RAM Role 实例角色。生产部署在阿里云 ECS 上时，推荐给实例绑定 RAM Role，避免在服务器上落长期 AK。

endpoint 建议：ECS 与 bucket 同地域时设置内网 endpoint（如 `oss-cn-hangzhou-internal.aliyuncs.com`），流量免费且低延迟。此时 `OSS_USE_CNAME=false`。

## Bucket 初始化 runbook（一次性）

1. 创建 bucket，私有读写，与 ECS 同地域。
2. **开启版本控制**（已拍板）：对象删除留下 noncurrent 版本作为恢复兜底；GC 的 `DeleteObject` 会产生 delete marker，`ListObjectsV2` 只列 live 版本，对账逻辑不受影响。
3. **配置生命周期规则**：noncurrent 版本 N 天后删除（建议 30 天，控制存储成本；删除 marker 同样清理）。
4. RAM 最小权限策略（AK 或实例角色均适用）：

```json
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "oss:PutObject", "oss:GetObject", "oss:HeadObject",
        "oss:DeleteObject", "oss:ListObjects", "oss:GetBucketInfo"
      ],
      "Resource": ["acs:oss:*:*:<bucket>", "acs:oss:*:*:<bucket>/*"]
    }
  ]
}
```

5. 中国内地 2025-03-20 之后新开通的 bucket：数据面访问需绑定自定义域名（CNAME），代码侧对应 `OSS_ENDPOINT` 指向该域名并设置 `OSS_USE_CNAME=true`；普通 OSS endpoint 不得打开该开关。

## 对象布局与语义

- 键即 canonical 相对路径（DB 只存这类 key）：头像为 `avatars/{accountID}/.../{checksum}/{objectID}`；新 planning 对象为 `planning/{accountID}/assets/{assetID}/g{generation}/{checksum}/{rendition}`。旧六段 planning key 继续只读兼容；新 key 内嵌 checksum，保证版本控制 bucket 的同 key 多版本只能是相同内容。
- 元数据：无 `metadata.json` sidecar；`MediaType/Checksum/Size(/Width/Height)` 写入对象用户元数据（`x-oss-meta-*`），`ModifiedAt` 取对象 LastModified。Stat 是一次 HEAD（不回读全文重算 sha256，完整性由 checksum-in-key 与 SDK CRC64 兜底）。
- `PutImmutable`：先 HEAD 判存在（同 meta 幂等返回 created=false，异 meta 拒绝），再 `PutObject(ForbidOverwrite=true)`，成功后重新 HEAD 校验持久化元数据。注意：**bucket 开启版本控制后 ForbidOverwrite 不生效**，并发写同 key 时会留多版本；两类新 key 均内嵌 sha256，保证同 key 内容一致，最坏产生字节相同的 noncurrent 版本。
- List：keyset 游标（cursor = 末项 key，`key > cursor` 递增），与 local 实现契约一致（见 conformance 套件）。

## 切换步骤（local → oss）

本期不做存量数据迁移（测试数据可丢弃）。切换即：bucket 按 runbook 初始化 → `.env` 设 `AVATAR_STORAGE_DRIVER=oss` + `OSS_*` → 重启。回滚同理切回 `local`（旧数据仍在卷上，新写入 OSS 的对象不回流）。
上线前 `production-preflight.sh` 会按 OSS 分支校验 region/bucket、CNAME 组合与 AK/SK 成对性；AK/SK 都为空时只能在已绑定 ECS RAM Role 的实例上发布。

存量迁移 CLI、预签名直链、备份策略切 OSS 版本控制：均为后续条目，不在该基线范围；创意画布新版的拟议上传例外见文末独立章节。现有 `avatar-manifest` / `planning-media-manifest` 与 compose 卷备份脚本在 OSS 模式会显式拒绝，避免误把旧本地卷当作当前数据备份。

## 测试

- 接口契约套件：`immutablefs.RunConformance` 与 `avatarmedia.RunConformance`（`*_test.go`），Local 与 OSS 实现共用。
- 真实 bucket conformance：设 `OSS_TEST_BUCKET` / `OSS_TEST_REGION`（可选 `OSS_TEST_ENDPOINT`；CNAME 时再设 `OSS_TEST_USE_CNAME=true`）+ 凭证后运行
  `go test ./internal/platform/immutablefs/ -run TestOSSConformanceRealBucket -count=1` 与
  `go test ./internal/avatarmedia/ -run TestOSSStoreConformanceRealBucket -count=1`；未设置自动 skip。

## 创意画布新版媒体部署契约（待实施）

本节只适用于[创意画布基础框架](../product/creative-canvas-system/architecture.md)的后续实施；尚未配置生产 bucket、RAM、CORS 或新增签名接口。已有头像、planning 和创意空间先导继续沿用各自现行路径，不因本节被切换。

新对象限于 `creative-v2/{account_id}/staging/{upload_id}/…` 和 `creative-v2/{account_id}/blobs/{blob_id}/{sha256}/{rendition}`。旧先导使用 `creative/{account_id}/assets/…`，它不属于新版孤立对象扫描范围。新 GC 只接受已校验的新前缀与账号，先核对 DB 保留根，再操作精确 key/version；旧文件由原生命周期或显式迁移接管规则负责。

上传采用服务端签发的短期分片 PUT URL；签名绑定准确对象、uploadId、partNumber 和所需请求头，客户端不能选择任意 key，也不获得 STS/长期凭证。完成、列分片和终止由已鉴权 API 发起；完成前服务端核实分片数量/实际大小，发布前仍做全文件校验，不能把签名或 ETag 当真实内容证明。签名过期后必须重新校验账号/会话再续签，取消会话后禁止续签并终止分片；已签 URL 不保证瞬时失效，永远只能写 staging，不能发布正式内容。实现依据：[OSS Go v2 预签名支持](https://github.com/aliyun/alibabacloud-oss-go-sdk-v2/blob/master/DEVGUIDE.md)、[UploadPart](https://www.alibabacloud.com/help/en/oss/developer-reference/uploadpart)。

部署时必须完成并留存以下核对结果：

- 浏览器上传域名必须能从摄影师设备访问；服务端内网 endpoint 不能直接作为浏览器 URL。需要 CNAME 的 bucket 配置相应 HTTPS 域名，不通过关闭签名验证解决。
- CORS 仅添加实际前端 Origin，生产与开发分别配置（协议、host、端口均匹配）；允许分片 PUT 及其预检，按签名实际使用的头设置 AllowedHeaders，暴露 ETag 和必要请求标识。CORS 不提供对象授权，不能代替 API 账号校验。[预签名上传说明](https://www.alibabacloud.com/help/en/oss/user-guide/upload-files-using-presigned-urls)
- 签名角色的写权限只覆盖新 staging。服务端完成/校验角色另需列分片、终止分片、读 staging 与写正式对象权限；维护角色按新前缀执行列举/删除，版本读取与精确版本删除的权限单独核实。沿用现有环境注入或实例身份；若拆角色再显式配置 AssumeRole，不默认新增 STS 发给浏览器。按当前 OSS 操作对应的 RAM action/resource 编制最小权限清单，不把原 runbook 的全 bucket 示例直接作为新版授权方案。
- 分片会话初始化、完成、终止均在数据库事务外；会话状态与入队短事务提交。进程崩溃后的孤立分片、已完成对象与 DB 状态通过维护任务对账；请求返回未知不能靠再次上传覆盖正式对象。
- staging 到期和 noncurrent 生命周期只匹配所属前缀，并服从运行/上传保留与备份恢复窗口；恢复演练前不启用新版物理删除。验收在同一测试 bucket 放置头像、planning、旧 creative 及新版样本，证明新版扫描不会删除旧样本。

读取仍经授权 API 代理，并验证 Range/206、取消和版本 ETag；上传预签名不引入公开下载直链。发布顺序为业务迁移与 River 自身迁移完成、兼容性检查、API/worker 按配置启用；worker 不能先于所需 schema 启动。具体 SDK 补丁版、权限 JSON、CORS 配置和启动命令由模块实现补齐，本节不冒充已验证 runbook。
