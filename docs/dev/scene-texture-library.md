# 内置贴图库（scene-texture-library）

平台自带的一批 CC0 真实照片贴图材质（C5c2）：木地板、红砖、大理石、草地等 24 套，10 个品类，2K 分辨率。它们是材质目录里 `kind = textured` 的条目，场景槽位只写 `{preset: "red_brick@1", params: {}}`，编辑器、渲染和助手都按 ID 取用，不依赖账号里的图片。

契约以 Epic `scene-editor-3d` 的 `data: 内置贴图库` 和 `api: 读取内置贴图` 为准；本文只讲怎么运维和扩充。

## 数据在哪

| 内容 | 位置 | 说明 |
|---|---|---|
| 清单（条目、贴图引用、来源、文件大小和下载地址） | `backend/internal/creativescene/materials.json` | 权威主数据，随代码发布；`make generate` 复制到前端 `materials.gen.json` |
| 贴图文件 | 对象存储 `scene-textures/<sha256>.jpg` | 内容寻址：文件名就是内容的 sha256，多个条目可共用一个文件 |
| 缩略图（256 × 256 材质球 WebP） | `backend/internal/creativescene/builtin_thumbnails/<id 把 @ 换成 _>.webp` | 后端嵌入二进制给助手看；`make generate` 复制到前端 `src/creative-canvas/assets/builtin/` |

存储驱动跟头像、策划素材共用 `AVATAR_STORAGE_DRIVER`：

- `oss`：同一个 bucket 的 `scene-textures/` 前缀，现有 RAM 策略（整个 bucket）已覆盖。
- `local`：`SCENE_TEXTURE_LOCAL_ROOT`，缺省为 `AVATAR_LOCAL_ROOT` 的同级目录 `scene-textures`。compose 部署挂卷 `scene_texture_data:/var/lib/crm/scene-textures`。

贴图文件不属于任何账号：没有数据库表，不进媒体 GC 和账号配额，也不进备份——随时可以从来源站重建（见下文「导入」）。

## 导入与检查

```bash
# 本机开发（在 backend/ 下，读 .env 的存储配置）
go run ./cmd/creativectl textures import
go run ./cmd/creativectl textures check

# compose 部署（镜像里的二进制）
docker compose run --rm --no-deps --entrypoint /usr/local/bin/creativectl app textures import
```

- `import`：逐个看清单里的文件，对象已在就跳过；不在就从来源站下载（四路并发、每个文件重试三次），核对大小和 sha256 一致才写入。可以反复运行，只补缺的。输出一行 JSON：`imported`、`skipped` 和 `failed`（每项给文件、条目和原因）；有失败时退出码 1，按原因处理后重跑即可。
- `check`：只检查不下载。全部就绪时退出码 0，`status: "ready"`；有缺失时退出码 3，`missing` 列出文件和条目。
- 首次导入约 177 MB、72 个文件。来源站单连接较慢，本机实测顺序下载要 19 分钟，四路并发后明显缩短。

上线前 `scripts/production-preflight.sh` 会在 compose 模式下跑一次 `textures check`：缺文件时报 `stage=scene-textures key=SCENE_TEXTURES remediation=run-creativectl-textures-import`，存储读不了时 `remediation=fix-scene-texture-storage`。`local` 驱动还要求挂了 `scene_texture_data` 卷。二进制部署（`--mode binary`）预检只提示 `scene-textures/check=run-creativectl-textures-check-attention`，由运维自己运行 `check`。

服务启动后也会在后台检查一遍，缺文件时记错误日志（列出缺失的条目），不阻止启动。

### 没导入时的表现

读取接口 `GET /api/v1/creative/scene-textures/{file}` 对清单里有、存储里没有的文件返回 503 `creative_texture_unavailable`（`details.file` 是文件名）。

- 编辑器：用到这些材质的物件按材质参数（缺省的白色）显示，槽位属性栏提示「内置贴图加载失败，暂按材质参数显示」。
- `render_view`、带 `lit` 的导出：失败，错误信息说明是哪一个内置材质，例如「内置贴图「红砖」加载失败」。

## 新增条目

已发布的条目不改：换图、改缺省值或尺寸都发布新版本（`@2`），旧版本留着，已有场景不变。新增一套材质：

1. 选来源：只用 Poly Haven 或 ambientCG 的 CC0 资产，2K JPG。记下站点、资产名和作者。
2. 下载贴图，算每个文件的 sha256 和字节数。ARM 文件（AO、粗糙度、金属度合在 r、g、b 三个通道）直接用：`ao` 读 `r`，`roughness` 读 `g`，`metalness` 读 `b`。
3. 在 `materials.json` 的 `materials` 里追加条目，字段见契约的条目表：`category` 必须是 `categories` 里已有的品类；`size_m` 取来源站标注的真实宽度；`defaults` 写 `{"color": "#ffffff", "roughness": 1, "metalness": 1}`，让贴图原样生效（参数与贴图相乘）。首批 24 套都带全五种贴图。
4. 跑 `go test ./internal/creativescene/`：目录加载会校验文件名、引用、`defaults` 的键和范围、来源和许可。`materials_internal_test.go` 里锁定了已发布条目的外观，新增条目要把它加进去。
5. 本机导入（`creativectl textures import`），然后生成缩略图（下一节）。
6. `make generate`，提交 `materials.json`、前端副本和缩略图。上线前在目标环境跑一次 `import`。

## 重新生成缩略图

缩略图用编辑器自己的材质球渲染，材质构建方式变了（光照、色调映射、贴图采样）之后也要重新生成。

```bash
# 先启动本 worktree 的 Vite，再在 frontend/ 下：
node scripts/render-texture-thumbnails.mjs --files <目录>            # 全部
node scripts/render-texture-thumbnails.mjs --files <目录> --only red_brick@1
```

- `<目录>` 放贴图文件：下载下来的 `<sha256>.jpg`，或者本地驱动导入后的存储根目录（`scene-textures/<sha256>.jpg/content`）都认。脚本经 Playwright 拦截读取接口、从这个目录供图，不需要后端。
- 输出写到 `backend/internal/creativescene/builtin_thumbnails/`，再 `make generate` 复制到前端；`make generate-check` 会检查两边一致。
- 无头 Chromium 走 Metal，约 80 秒一张，全量 24 张约半小时。生成后人工看一遍（拼一张总览图最快），确认真实尺寸、法线方向和 ARM 通道都对。
- 每个条目都必须有缩略图，也不能有对不上条目的缩略图：后端加载时检查，缺了或多了直接启动失败。

## 来源与许可

所有贴图都是 CC0（公有领域），来自 [Poly Haven](https://polyhaven.com) 和 [ambientCG](https://ambientcg.com)。CC0 不要求署名，我们仍在条目的 `source` 里记下站点、资产名和作者，资产库的查看浮窗也会显示，作为致谢。来源站撤下某个文件不影响已导入的对象；重建环境时下载失败的文件按「没导入」处理，`import` 的输出和启动日志里都能看到。

对象存储里不再被任何条目引用的文件不会自动清理，数量很少，需要时手工删除。
