# data-export 浏览器截图元数据

本文件补充两张设置页截图的捕获口径。截图使用隔离 PostgreSQL、虚构账号与显式禁用 Telegram 的本地环境，不含 owner 真实数据、token 或导出 payload。

## 桌面截图

- 文件：`data-export-settings-desktop.png`
- 捕获时的目标 CSS viewport：1280px 桌面场景
- `devicePixelRatio`：捕获时未持久化，无法从 PNG 独立恢复
- `innerWidth`：捕获时未持久化，无法从 PNG 独立恢复
- `scrollWidth`：捕获时未持久化，无法从 PNG 独立恢复
- PNG 原始像素尺寸：1969×1109（由 `sips` 复核）
- 证据边界：该图可证明卡片、PII 保管/删除提示、头像图片未包含与引用不可跨部署恢复文案的视觉呈现；不能仅凭物理像素宽度独立证明 1280px CSS viewport。浏览器扩展、宿主 runtime、页面缩放或捕获裁切都可能使 PNG 物理像素与 CSS viewport 不一致。

## 375px 场景截图

- 文件：`data-export-settings-375.png`
- 捕获时配置的 CSS viewport：375×812
- `devicePixelRatio`：捕获时未持久化，无法从 PNG 独立恢复
- `innerWidth`：375
- body/document `scrollWidth`：360
- 导出卡 bounding box：left=12、right=348、width=336
- PNG 原始像素尺寸：360×1101（由 `sips` 复核）
- 证据边界：运行时测量显示卡片没有造成横向溢出，但 PNG 原始宽度是 360px，不能把该物理宽度直接写成 375px viewport 证明。精确 375px viewport、devicePixelRatio、键盘、401、500/Blob reject 与快速重复触发由 QA 阶段重新记录。

## 复核结论

- 两张 PNG 均为有效截图，内容与虚构 fixture、当前 UI 文案一致。
- 原实现报告中把“1280/375 场景”直接等同于 PNG 物理像素宽度的表述不够严谨，现已降格为浏览器观察证据。
- QA 重拍时必须在同一证据记录中同时保存：viewport 配置、`devicePixelRatio`、`innerWidth`、body/document `scrollWidth`、关键 card bounding box 与 PNG 原始像素尺寸。

## 2026-07-22 QA 重拍

QA 使用 fresh-reset 的独立 PostgreSQL 容器、synthetic 单账号、显式本地数据库连接、Telegram disabled 与默认 Settings。截图不含 owner 真实数据、token、账号 ID 或导出 payload；浏览器页面的 Telegram 状态为“尚未绑定”。旧 driver 遗留服务因连接隔离不成立而被判为无效证据，未进入以下结论。

### 桌面 QA

- 文件：`data-export-qa-desktop-1280.jpg`
- CSS viewport：1280×900。
- `devicePixelRatio`：1。
- `window.innerWidth` / `innerHeight`：1280 / 900。
- document/body `scrollWidth`：1265 / 1265；均不大于 innerWidth，无横向溢出。
- 导出 card bounding box：left=260、right=836、width=576、top=98.71875、bottom=284.4453125。
- JPEG 原始像素尺寸：1265×1032；文件格式与 `.jpg` 后缀一致。

### 375px QA

- 文件：`data-export-qa-mobile-375.jpg`
- CSS viewport：375×812。
- `devicePixelRatio`：1。
- `window.innerWidth` / `innerHeight`：375 / 812。
- document/body `scrollWidth`：360 / 360；均不大于 innerWidth，无横向溢出。
- 导出 card bounding box：left=12、right=348、width=336、top=86.71875、bottom=315.8359375。
- 按钮 bounding box：left=33、right=327、width=294、top=260.3359375、bottom=296.8359375。
- JPEG 原始像素尺寸：360×1101；文件格式与 `.jpg` 后缀一致。

### QA 行为补充

- native button 经键盘 Enter 实际触发下载。
- 慢查询时按钮显示“正在准备导出…”并 disabled；第二次物理点击没有产生第二个 export query。
- Settings 500 不隐藏 card；导出 500 与 response body 中断均呈现可重试错误且不产生下载。
- 401 实际导航到 `/login`。
- 成功下载的 synthetic JSON 在重新解析结构、counts 与文件名后已删除；测试结束后已清理临时服务、容器与 viewport override。
