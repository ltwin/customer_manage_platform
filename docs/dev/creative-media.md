# 创意画布媒体（FND-05）开发说明

领域包 `backend/internal/creativemedia`：上传会话（`creative_uploads`）、分片（`creative_upload_parts`）、对象（`creative_blobs` + `creative_content_objects`）、候选（`creative_upload_candidates`）、read pin、配额。迁移 `0041_creative_media` 同时把 `creative_contents/creative_assets.kind` 扩到 image/video/audio；down 在已有媒体数据时拒绝。

## 流程与进程

| 阶段 | 进程 | 说明 |
|---|---|---|
| CreateUpload | API | 校验目标（库目录/标签或节点 data_revision）、预留配额、写不可变来源声明（`rights` 可省略，缺省 `photographer_owned/ownership_attested`）、同事务入队 `media.upload_init` |
| workInit | worker | claim 租约 → `initializing` → 适配器 InitMultipart → `uploading`，multipart_id 落库后才允许签分片 |
| AuthorizeParts | API | 仅 `uploading/none` 且未到期；local 返回 HMAC token 的 API PUT，OSS 返回预签名 UploadPart |
| CompleteUpload | API | 202；`completing` 后入队 `media.upload_complete` |
| workComplete | worker | 服务端 ListParts 核对数量/总字节 → CompleteMultipart 固定版本 → `verifying` 并入队 `media.upload_verify` |
| workVerify | worker | 流式读取固定版本到私有临时文件，mimetype 嗅探 + 图片解码/ffprobe，预分配 blob 身份与正式 key 后写正式对象（图片 >1600px 另生成 display 渲染件），再以 `publish_operation_id` 执行发布命令 |
| publish | worker | 库根 → upload → 候选/画布 → 内容锁序；目标仍有效则创建资产或绑定节点（可撤销 change），已变化则写 pending 候选；同事务清空 `blob_id`、写 `handed_off_at`、结算配额、置 `ready` |

执行权由 `execution_epoch + lease_until` 控制：取消/失败/到期都递增 epoch，旧 worker 的每次写都校验 epoch，迟到结果不会发布。`SweepExpired` 只标记到期并释放预留，不删除对象；物理清理归 FND-10。

## 进程配置

本地开发先执行 `go run ./cmd/creative-worker -migrate` 建 River 的 `creative_jobs` schema，再常驻 `go run ./cmd/creative-worker`；server 启动时检查队列就绪，未迁移则记警告并让上传返回 503「媒体处理队列未就绪」，不会在入队事务里报 500。

`creative-worker` 启动（非 `--migrate/--check`）时调用完整 `config.Load()`，因此除 `DATABASE_URL` 外还需要与 API 相同的 `AUTH_TOKEN_SECRET`（派生票据/分片签名密钥）、`PUBLIC_BASE_URL`、存储驱动与 `CREATIVE_*` 配置；部署时给两个进程同一份 EnvironmentFile。verify 任务超时 20 分钟、complete 5 分钟，其余沿队列默认 2 分钟，每个阶段的执行租约与其超时相同（测试固定该不变量）；发布阶段被中断的会话在下次任务认领时以 `promote_interrupted` 落为 failed，写入的对象留在 `deleting` 的 blob 行等待对账。

## 读取

`IssueTicket` 在事务内用 `RequireUsable` 核验当前用途后签发 10 分钟无状态票据；`Open` 再次核验、锁 blob、写 read pin（120s）并打开精确版本。HTTP 层实现单段 Range/206/416/HEAD/ETag，`Cache-Control: private, no-store`。撤销 `creative_usage_grants` 后票据与流立即失效。

## 测试

- `go test ./internal/creativemedia/`：真实 PostgreSQL + local 适配器 + 内嵌 HTTP 分片端点，样本在 `testdata/`（JPEG/PNG/WebP、MP4 H.264/AAC、WebM VP8/Opus、MP3、WAV；WebM 由 Chromium MediaRecorder 录制）。
- `CREATIVE_EDITOR_URL=... CREATIVE_EDITOR_SCRIPT=creative-media.e2e.mjs go test ./internal/platform/httpapi/ -run TestCreativeEditorBrowser`：真实浏览器导入/拖入/替换/撤销/预览/下载/播放/存库/重开。
- OSS：`internal/creativemedia/oss.go` 需在授权 bucket 上补 conformance（PRE-05），当前未验收。
