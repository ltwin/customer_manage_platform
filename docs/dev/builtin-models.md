# 内置模型库（builtin-models）

平台自带、所有账号都能放进场景的模型，目前规划的是一男一女两个素体（Quaternius Universal Base Characters，CC0）。场景物件写 `model: {builtin: "body_female@1"}`，不属于任何账号，不能修改。

契约以 Epic `scene-pose-system` 的 `data: 内置模型库` 和 `api: 读取内置模型文件` 为准；本文只讲怎么运维和扩充。

## 数据在哪

| 内容 | 位置 | 说明 |
|---|---|---|
| 清单（条目、manifest、骨骼映射、放置时写入的材质、来源） | `backend/internal/creativescene/models.json` | 权威主数据，随代码发布；`make generate` 复制到前端 `src/creative-scene/builtinModels.gen.json` |
| GLB 文件 | 对象存储 `scene-models/<sha256>.glb` | 内容寻址：文件名就是内容的 sha256 |
| 贴图 | 材质目录里 `listed: false` 的内置贴图材质 | 不在本清单里，条目的 `materials` 引用它们；导入见 [scene-texture-library.md](scene-texture-library.md) |
| 缩略图（256 × 256 WebP） | `backend/internal/creativescene/builtin_thumbnails/models/<id 把 @ 换成 _>.webp` | 后端嵌入给助手看；`make generate` 随内置材质缩略图一起复制到前端 |

存储驱动与内置贴图相同，跟头像共用 `AVATAR_STORAGE_DRIVER`：

- `oss`：同一个 bucket 的 `scene-models/` 前缀。
- `local`：`SCENE_MODEL_LOCAL_ROOT`，缺省为 `AVATAR_LOCAL_ROOT` 的同级目录 `scene-models`。

## 导入与检查

来源站没有直链，GLB 由转换脚本从手工下载的来源包生成，再从目录导入：

```bash
# 在 backend/ 下，读 .env 的存储配置
go run ./cmd/creativectl models import --dir <转换脚本的输出目录>
go run ./cmd/creativectl models check
```

- `import --dir`：逐个看清单里的 GLB，对象已在就跳过；不在就从目录读 `<sha256>.glb`，依次核对大小、sha256、GLB 校验（与导入模型相同的静态子集和蒙皮子集），再把算出的 manifest（含骨架）和清单比对，全部一致才写入。可以反复运行。输出一行 JSON：`imported`、`skipped` 和 `failed`（每项给文件、条目和原因）；有失败时退出码 1。
- `check`：只检查不读目录。全部就绪时退出码 0；有缺失时退出码 3，`missing` 列出文件和条目。
- 素体的贴图另用 `creativectl textures import --dir <贴图目录>` 导入。

服务启动后会在后台检查一遍，缺文件时记错误日志（列出缺失的条目），不阻止启动。读取接口 `GET /api/v1/creative/scene-models/{file}` 对清单里有、存储里没有的文件返回 503 `creative_model_file_unavailable`，编辑器按「模型加载失败」处理。

## 新增条目

已发布的条目不改：换文件、改映射或材质都发布新版本（`@2`），旧版本留着，已有场景不变。条目字段见契约的条目表。

服务启动时校验清单，不合法直接启动失败：

- 外形与取值：`id` 为「名称@版本」，`category` 目前只有 `humanoid`，`mount` 为四种之一，GLB 文件名是 sha256，大小不超过 200 MiB。
- `materials` 的键都是 manifest 的槽位，预设都在材质目录里，参数在范围内。
- 人形模型必须有 `rig.humanoid`，`rig` 按 manifest 的骨架校验（规则同导入接口的 `rig`）。
- 来源写明站点、资产、作者和 https 地址，许可为 CC0。
- 每个条目都有缩略图，也没有对不上条目的缩略图。

改完跑 `go test ./internal/creativescene/`，再 `make generate`，提交清单、前端副本和缩略图。上线前在目标环境跑一次 `models import` 和 `textures import`。
