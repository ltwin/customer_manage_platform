# 内置模型库（builtin-models）

平台自带、所有账号都能放进场景的模型，目前是一男一女两个素体 `body_female@1`、`body_male@1`（Quaternius Universal Base Characters，CC0）。场景物件写 `model: {builtin: "body_female@1"}`，不属于任何账号，不能修改。

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
- `local`：`SCENE_MODEL_LOCAL_ROOT`，缺省为 `AVATAR_LOCAL_ROOT` 的同级目录 `scene-models`。compose 部署挂卷 `scene_model_data:/var/lib/crm/scene-models`。
  - 镜像里预建了 `/var/lib/crm/scene-models` 并交给 `app`（UID 1000），新建的卷首次挂载时会沿用这个属主。
  - 在这之前就创建过的卷属于 root，`creativectl` 写入时会报 `permission denied`。需要用 root 改一次属主：
    `docker compose run --rm --no-deps --user root --entrypoint chown app -R app:app /var/lib/crm/scene-models`

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

上线前 `scripts/production-preflight.sh` 会在 compose 模式下跑一次 `models check`：缺文件时报 `stage=scene-models key=SCENE_MODELS remediation=run-creativectl-models-import`，存储读不了时 `remediation=fix-scene-model-storage`。`local` 驱动还要求挂了 `scene_model_data` 卷。二进制部署（`--mode binary`）预检只提示 `scene-models/check=run-creativectl-models-check-attention`，由运维自己运行 `check`。

服务启动后会在后台检查一遍，缺文件时记错误日志（列出缺失的条目），不阻止启动。读取接口 `GET /api/v1/creative/scene-models/{file}` 对清单里有、存储里没有的文件返回 503 `creative_model_file_unavailable`，编辑器按「模型加载失败」处理。

## 素体的生成

两个素体由 `frontend/scripts/builtin-models/convert.mjs` 从来源包生成，走的是导入对话框同一套换算与写出（解析 → 原点移到底面中心 → 按 UE 命名识别人形映射并按服务端规则校验 → 写出 GLB 并回写节点下标 → 贴图按导入的规则处理），所以素体和摄影师自己导入的角色是同一种东西。

1. 从 [Quaternius](https://quaternius.com/packs/universalbasecharacters.html) 手工下载 Universal Base Characters，解压后找到 `Base Characters/Godot - UE` 目录（`.gltf` + `.bin` + PNG 贴图）。
2. 在 `frontend/` 下运行（不需要后端，脚本自己起 Vite 和无头 Chromium）：

   ```bash
   node scripts/builtin-models/convert.mjs --source <…/Base Characters/Godot - UE> --out <输出目录>
   ```

   - 来源包的贴图引用有误（写成 `xxx_png.png`），脚本找不到时去掉 `_png` 再找；材质改名为槽位 `body`、`eyes`、`brows`。
   - 输出 `<输出目录>/models/<sha256>.glb`、`textures/<sha256>.png` 和 `catalog.json`（清单条目与贴图材质条目），控制台打印每个素体的三角形数、标准骨骼数（应为 52）和换算报告。
   - 贴图材质 `ubc_*@1` 是 `listed: false`、品类 `character`：只给素体用，选材面板、资产库和助手的内置来源都不列出。金属度为 0 的槽位不带金属度贴图（glTF 里贴图乘系数，为 0 时贴图不起作用）。
3. 确认输出无误后加 `--apply` 再跑一次，写进 `models.json` 与 `materials.json`。已有的 ID 直接报错：要改就发新版本。
4. 导入贴图和 GLB（见上一节），再生成缩略图：

   ```bash
   node scripts/render-texture-thumbnails.mjs --files <输出目录> --only ubc_female_body@1   # 每个贴图材质各一次，或省略 --only 全部重画
   node scripts/render-texture-thumbnails.mjs --files <输出目录> --models                   # 模型缩略图，写到 builtin_thumbnails/models/
   ```

5. 更新 `builtin_models_internal_test.go` 与 `materials_internal_test.go` 锁定的条目，跑 `go test ./internal/creativescene/ ./internal/creativeagent/`，再 `make generate`，提交清单、前端副本和缩略图。

## 新增条目

已发布的条目不改：换文件、改映射或材质都发布新版本（`@2`），旧版本留着，已有场景不变。条目字段见契约的条目表。

服务启动时校验清单，不合法直接启动失败：

- 外形与取值：`id` 为「名称@版本」，`category` 目前只有 `humanoid`，`mount` 为四种之一，GLB 文件名是 sha256，大小不超过 200 MiB。
- `materials` 的键都是 manifest 的槽位，预设都在材质目录里，参数在范围内。
- 人形模型必须有 `rig.humanoid`，`rig` 按 manifest 的骨架校验（规则同导入接口的 `rig`）。
- 来源写明站点、资产、作者和 https 地址，许可为 CC0。
- 每个条目都有缩略图，也没有对不上条目的缩略图。

改完跑 `go test ./internal/creativescene/`，再 `make generate`，提交清单、前端副本和缩略图。上线前在目标环境跑一次 `models import` 和 `textures import`。
