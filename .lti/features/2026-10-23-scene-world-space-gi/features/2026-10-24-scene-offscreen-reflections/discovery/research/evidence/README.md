# S1 的证据

S1（`scene-offscreen-reflections`）各项验收的数据和图。超过 30 MB 的部分不进 git，只提交对照需要的文件，并在这里写明怎么重新生成（proposal「证据」）。

## 环境

- **设备**：Mac16,8（Apple M4 Pro）。
- **浏览器**：headless Chromium，e2e 的 `metal` 启动参数（ANGLE Metal）。
- **服务**：在 `frontend/` 下运行 `npx vite --port 5191 --strictPort`，下面的命令都用 `CREATIVE_SCENE_URL=http://localhost:5191`。

## `before/`：改动前（任务 1.2）

代码：develop `4d4b761f`（S1 的基点，只多了任务 1.1 和 e2e 的改动，都不影响画面）。

| 文件 | 内容 | 用于 |
|---|---|---|
| `gi-baseline.json` | 7 个参考场景在实时 GI 下的 `lit` 导出（2K）：整图与区域亮度、导出耗时、视口帧时间；三套后端：WebGPU、WebGL2（兼容模式）、WebGPU 按 compatibility 设备处理（`forceCompatibility`） | AC-S1-3、AC-S1-6：改动后在 `traceOff`、兼容模式和设备不支持时逐像素相同 |
| `mirror-board-shot-webgpu-lit.jpg` | `mirror-board` 改动前的导出（JPEG 90） | AC-S1-1 的并排图 |
| `wgsl.json` | 15 个材质（原理化和 14 个预设）在工作台和实时 GI 下各自生成的全部 WGSL 模块的哈希 | AC-S1-3：宽度函数改动前后相同 |
| `*.png`、`wgsl/` | 全套导出图（21 张）和 WGSL 原文（301 个模块），约 53 MB | 只在本机，见 `.gitignore` |

整图亮度与 S0 的 `gi-baseline` 相同（`backlit-room` 148.0、`thin-wall` 202.2、`mirror-board` 137.2、`noon` 164.1、`room` 154.6、`materials` 152.6、`cornell` 82.3），三套后端之间相差不超过 0.1。也就是说，S0 之后合入 develop 的「合并 part」没有改变这些场景的画面。

同一份代码上，WGSL 的哈希在 3 次运行间完全相同。

**重新生成**（在 `4d4b761f` 上，或任何还没改宽度函数的提交上）：

```sh
cd frontend
SCENE_E2E_ONLY="gi-baseline" SCENE_E2E_GI_BASELINE=<目录> SCENE_E2E_GI_COMPAT=1 \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # 约 4 分钟
SCENE_E2E_ONLY="wgsl:" SCENE_E2E_WGSL=<目录> SCENE_E2E_WGSL_WRITE=1 \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # 约 20 秒
```

`wgsl` 用例和 `gi-baseline` 的 `SCENE_E2E_GI_COMPAT` 是任务 1.2 加的，`4d4b761f` 上还没有。要在那里重新生成，先把本分支的 `frontend/scripts/creative-scene.e2e.mjs` 拷过去。

## `after/`：改动后

| 文件 | 内容 | 用于 |
|---|---|---|
| `mirror-board-shot-webgpu-lit.jpg`、`*-off.jpg` | `mirror-board` 的材质光照导出，带这一层与关掉这一层（带这一层的图在 A11、A12 之后各重新生成一次；`-off` 重新生成后逐字节没变） | AC-S1-1 的并排图（对照 `before/`） |
| `mirror-board-pitch-*.png` | 摄像机逐步下压（12°–42°），小球离开画面时地面里的倒影；`-off` 为关掉这一层（A12 之后重新生成） | AC-S1-1 |
| `trace-frames.json` | 视口 1536 × 1024 下每帧的耗时，带这一层与不带，7 个参考场景和大厅，各三轮的中位数 | AC-S1-7、S1-U4（A12 之后重测。这一轮大厅多 10 ms，超过 8 ms；同一天修复前后各测四次，见任务 8.5） |
| `trace-hall.json` | 大厅（S0 的压力场景）：打开、从头构建与缓存命中的就绪、拖动、增删、两次导出加两次关掉这一层的对照导出。JS 堆三个读数：只开视口、导出时的峰值、导出的底座释放数组之后，第一个和第三个先做 GC 再读 | AC-S1-8、S1-U2、S1-U3（任务 8.2，A9 和 A5 之后；任务 8.6 之后重测，这一轮带这一层的两次导出最多差 3 级，见 8.6） |
| `hall-exports-diff.png` | 修复镜中斑点之前（A11 之前）：大厅两次带这一层的导出相差的像素，白色差超过 8 级，灰色差 1–8 级。A11 之后两次导出逐字节相同（`trace-hall.json` 的 `exports`） | S1-U5 |
| `trace-proxy.json` | 大厅：原始层导出两次，再把预算压到原始放不下、代理放得下，导出一次。两份差异统计：全图和镜子区域（2048 × 1365 的图里 x 1000、y 120 起 680 × 400）。A11、A12 之后各重测一次 | AC-S1-9（任务 8.3） |
| `hall-proxy-mirror.png` | 上面的镜子区域并排：原始、代理、差异 ×4 | AC-S1-9，交 owner 判断 |
| `hall-original-twice-mirror.png` | 修复之前（A11 之前）同一区域，原始层两次导出不一致的一轮：两次结果和差异 ×4，斑点位置不同。e2e 每轮也会写出同名文件，A11 之后差异为零 | S1-U5 |
| `hall-mirror-fix.png` | 大厅镜子区域，上为修复前、下为修复后（A11：镜面的反射不做空间降噪） | 任务 8.4，交 owner 看图 |
| `ball-near-mirror-fix.png` | 红球贴近 `mirror-board` 的镜子（镜子是缩放成薄片的方块），两个视角各一行：左为关掉这一层，中为修复前，右为修复后（A12：步进只信屏幕上看得见的）。上一行是 owner 报告的情况：修复前镜中球的下半截被换成了球后面的地板 | 任务 8.5，交 owner 看图 |
| `grazing-floor.png` | `mirror-board` 的地面换成光滑（糙度 0.08）的 `tile@1`、`wood_plank@1`，摄像机贴近地面（高 0.25 m、0.08 m、0.15 m，下压 0.5°–2°），左为关掉这一层，右为带这一层（半分辨率）。看法线缓冲里的凹凸会不会让掠射角下的反射穿到地面以下、补追打中地面自己：带这一层时地面上没有斑点或变暗，开关前后变暗超过 8 级的像素集中在镜中方块和球的倒影上，木板接缝处只有亚像素的差别 | 任务 8.6（代码审计的待验证项） |

**重新生成**（本分支）：

```sh
cd frontend
SCENE_E2E_ONLY="trace: AC-S1-1" SCENE_E2E_TRACE=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # mirror-board-*，约 20 秒
SCENE_E2E_ONLY="trace: a ball close to the mirror" SCENE_E2E_TRACE=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # ball-near-mirror-*，约 20 秒；修复前的一列在提交 82097bb1 上生成，再用这些图拼成 ball-near-mirror-fix.png
SCENE_E2E_ONLY="trace diag:" SCENE_E2E_TRACE_DIAG=lit,albedo,object,distance SCENE_E2E_SHOTS=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # 诊断：每种着色模式一张视口截图和镜子区域，约 1.5 分钟一种
SCENE_E2E_ONLY="trace diag: a glossy tiled floor" SCENE_E2E_TRACE_GRAZING=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # grazing-*.png 六张，约 15 秒；左右拼起、缩到一半成 grazing-floor.png
SCENE_E2E_ONLY="trace perf: AC-S1-7" SCENE_E2E_TRACE_PERF=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # 约 10 分钟
SCENE_E2E_ONLY="trace perf: AC-S1-8" SCENE_E2E_TRACE_PERF=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # 约 12 分钟，另写出四张导出图 hall-export-*.png（每张约 3 MB，不进 git）
SCENE_E2E_ONLY="trace perf: AC-S1-9" SCENE_E2E_TRACE_PERF=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs      # 约 4 分钟，另写出三张整图 hall-proxy-{original,original-again,proxy}.png（每张约 3 MB，不进 git）
```
