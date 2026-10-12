# 素材主色与画幅

资产库 V6-069 使用 `oklab-k5-v1` 主色算法。上传和生成导入在图片完整解码、方向修正后计算主色，和原始 blob 的 ready 发布同事务保存。颜色是派生事实，不是媒体保留根；0061 已建表，本次无需迁移。

## 计算与筛选

长边最多采样 64 像素，在 OKLab 空间做最多 30 轮 k-means。黑、白、红、绿、蓝五个初始中心固定；空簇按固定像素顺序选取剩余误差最大的点。输出最多 5 色，纯色图不复制相同色填满。透明像素忽略，半透明像素以 alpha 为权重，全透明图记 `failed/no_visible_pixels`；不会阻塞素材入库。算法采用 [OKLab 原作者的公开实现矩阵](https://bottosson.github.io/posts/oklab/#converting-from-linear-srgb-to-oklab)。

颜色占比至少 10% 才参与筛选。色度 < 0.04 为中性色，只匹配中性色且明度差 ≤ 0.12；有彩色仅匹配有彩色、环形色相差 ≤ 25°、明度差 ≤ 0.2。阈值由 `creativepalette` 定义，匹配仅在 `creativelibrary.appearanceCondition` 的 SQL 中实现。真实 PostgreSQL 集成测试覆盖 12 张照片和阈值边界，并交叉核对 items、total_count 与 facets。12 张原型真实照片走查后保留已批准阈值；灰阶照片不命中暖色，粉色花卉、黄色草坡分别命中粉、黄。

画幅从原始 blob 已有宽高计算，不新增存储：长短边比 ≤ 1.1 为方形，其余宽大于高为横幅，反之为竖幅。当前只支持有尺寸的图片；视频现有宽高为编码尺寸，含旋转信息时不能代表显示方向，因此暂不参与画幅筛选。检查器显示视频「编码尺寸」，不附带横幅/竖幅结论。后续见 [视频显示方向待办](../../.codestable/work/issue-asset-video-orientation.md)。颜色也仅支持图片。

`GET /creative/assets` 支持 `color=#rrggbb`、`shape=landscape|square|portrait`，参与游标绑定、总数及首屏 facets。所有内容派生条件都经过既有 `readableContent` 授权守卫，详情及列表的 `palette` 只从已通过展示授权的原始 blob 批量加载；不可读内容返回空色板。

## 回填与运维

`creative-worker` 启动时遍历账号分页，给缺少当前算法记录的 ready 图片原始 blob 排 `media.palette_backfill`，使用 maintenance 队列。每批最多 50 张，JPEG 原始图优先读显示图，否则读原图；PNG/WebP 始终读原图，因为现有显示图是 JPEG，会丢失 alpha；原始与被读取的显示 blob 都先登记读取锁。尝试上限 10 分钟，锁有效期覆盖尝试，结束时释放已登记的锁，进程崩溃后由到期失效兜底。

网络超时、短读、数据库故障不写主色结果，交给现有队列有限次重试。存储返回 `versionedfs.ErrNotFound` 时记 `failed/object_missing`；完整对象的确定性解码失败或异常尺寸同样记 `failed`，不自动重试，其他图片及后续批次继续处理。修复缺失对象后可用 rebuild 重新计算。整批结果、命令回执、后续批次入队同事务提交；重复投递不多处理一批，不重复排后续批次。进入删除流程的 blob 跳过。状态不参与保留关系。

若重试耗尽，缺失记录保留为缺失；重启 worker 会补扫。滚动部署中旧实例晚于启动扫描写入的图片，在下次 worker 启动或运维重建时补齐。

在部署环境注入 `DATABASE_URL` 后运行：

```sh
cd backend
go run ./cmd/creative-palette rebuild --account <账号ID> --algorithm oklab-k5-v1
```

命令只删除指定账号、指定受支持算法的派生记录并原子排队，不读取对象存储；实际计算由使用同一数据库与媒体存储配置的 worker 执行。不支持的算法在清理前拒绝；入队失败整体回滚；删除操作不触及别的账号或其他算法版本；后续扫描仍会按当前算法补齐该账号的缺失项，包括将旧算法的图片结果升级为当前版本（表按账号与 blob 唯一）。命令不会启动 worker。重建期间相应色板暂时为空，其他素材功能不受影响。

回滚只回滚应用，不执行 0061 down；主色记录留存，旧应用忽略。未来换算法应修改版本常量并保留版本化测试，新版本扫描会覆盖原版本派生记录；未实现任意算法选择。

## 验证入口

- `go test ./internal/creativepalette ./internal/creativemedia ./cmd/creative-palette`：算法、上传/生成原子发布、授权、画幅边界、回填临时失败与确定性失败、105 张分批恢复、重复执行、重建隔离、读取锁、启动分页。
- `CREATIVE_PALETTE_BENCHMARK=1 go test ./internal/creativemedia -run TestPaletteSearchScale -count=1 -v`：5001 个独立内容修订和原始 blob 的首屏+facets 采样。
- `CREATIVE_QA_CHECK=step6 node frontend/scripts/creative-assets.e2e.mjs`：桌面/手机筛选、URL、检查器主色点击；设置 `CREATIVE_PROTOTYPE_URL` 可和已启动的 V6 原型逐项比对计算样式。
