# v4 原型验证 · 2026-09-07

## 已运行

- `node check.mjs`：通过。核对图片清单、7 种容器宽度下的原图比例、卡片不重叠与空列表布局。
- `node --check studio.js`：通过。
- `node qa.mjs`：12 组浏览器检查通过，`pageErrors: []`。覆盖桌面 1440×1050、1280×900；手机 375×812 与 320×640。
- `npx --yes @google/design.md lint DESIGN.md`：0 errors / 0 warnings。

## 浏览器证据

1. 阻塞图片网络请求，确认首屏只有附近图片被赋 src、远处图片未发起请求。拍摄占位截图后放行，比较全部卡片 bounding rect，加载前后完全一致；比例误差 <0.002。
2. 滚动到底部后 12 张图片均加载，图片失败注入后原位重试，位置不变。
3. 搜索、无结果、清空、中文组合输入结束前不提交检索；刷新保持查询和结果。
4. 大图查看、上下张、笔记保存、未保存关闭确认、Escape、关闭后焦点归还。截图发现详情图被固有高度撑出容器，已改为确定尺寸的 flex 图框并新增图片边界断言；新增连续 12 次 Tab 检查后补齐共享弹层焦点循环，修复后完整回归通过。
5. 批量加入项目、刷新保留、重复加入禁用；新建集合、空集合与名称错误；心选加入/移出。
6. 图片/文字/链接导入、错误文件提示、原子保存、刷新保留；多标签页旧版本不能覆盖新保存数据。
7. 断网时本地心选仍可保存；移动端详情操作可达，减少动态模式下占位不移动；320px 无横向溢出。
8. 浏览器计算值确认玻璃栏使用 `blur(24px) saturate(1.35)` 与透明表面。

截图与机器报告默认在 `/tmp/creative-v4-qa/`，分别为占位、桌面素材、大图详情、集合、图片错误、手机素材/详情/集合及 `report.json`，可通过 README 命令重现。

最终视觉预览随原型保留：[桌面](evidence/desktop.png)、[手机](evidence/mobile.png)、[浏览器报告](evidence/report.json)。截图等待淡入完成后再拍摄，避免把图片正在淡入的瞬间误认为最终视觉。

## 静态扫描的真实边界

运行 premium `audit_project.py` strict，扫描返回 **37 个检测报告项（exit 1），不能标为严格扫描通过**：35 个 `affordance.actionless-button` 与 2 个 `form.textarea-resize-missing`。

已核对扫描器源代码：按钮规则只识别 HTML 内的 `onclick` / Vue `@click` 或表单提交，不追踪独立 `studio.js` 的 `addEventListener` 与委托绑定；textarea 规则只看标签内 `resize-none` / 内联样式，不追踪本原型 `studio.css` 的统一 `textarea { resize: none }`。这些报告项由已存在的实际事件绑定与全局 CSS 解释，不通过增加空 onclick、改成假提交、排除 HTML 或放宽规则来伪造绿灯。浏览器 CDP 检查当时页面的 84 个按钮均有 click 绑定/委托或实际表单提交路径，unbound=[]；两处 textarea 的 computed resize 均为 none。真实动作由上面的浏览器主链验证，静态报告保存在 `/tmp/creative-v4-premium-audit.json`。

## 不覆盖

本次不修改生产前后端，因此未运行 Go、生产前端或 ops 全量测试。没有真实 API、OSS、模型、跨设备同步、账号权限或真人试点结论。界面是设计走查载体，本地存储成功不代表云端业务成功。

## 集合语义更新验证

- `node check.mjs` 新增系统集合守卫、最爱/项目不参与归类、多集合/最后一处关系、删除及重命名不改素材的检查，全部通过。
- `node collections.qa.mjs` 新增 5 组浏览器场景，全部通过：三项导航与固定入口；旧 view=saved 和心选数据兼容；最爱/项目使用后仍未归类；普通集合归类与移出；重命名、取消/确认删除、刷新、其他集合/项目/最爱保留；失效链接与手机管理入口。pageErrors=[]。
- 原有 `qa.mjs` 的 12 组主链重新通过，包含新的“集合 → 我的最爱”路径；静态扫描仍为已说明的分离事件/CSS 检测局限，最新统计为 35 项，未宣称 strict 通过。
- 集合截图与报告保存在 evidence/collections-desktop.png、evidence/collections-mobile.png、evidence/collections-report.json；用独立浏览器数据验证，不改正在浏览原型的账号数据。

## 项目工作台补全验证

- `projects.qa.mjs` 9 组浏览器检查通过，pageErrors=[]、apiRequests=[]：独立项目列表/概览；意图与条件编辑/未保存/刷新；项目参考说明不改全局笔记；参考快照与手写分镜；六类策划与道具核对；只读画布及禁用未来能力；现场完成/跳过/撤销及共享备忘；新项目空白；手机布局和编辑焦点；项目导入进入全库且仍未归类。
- `check.mjs` 增加新项目无示例内容、已有文档不被覆盖、分镜参考为快照的检查，通过。原有 `qa.mjs` 12 组及 `collections.qa.mjs` 5 组回归通过，项目引用测试显式切换到参考页后验证。
- 道具复选操作等待本地写入完成后再验证刷新；现场核对后保持备忘展开与当前控件焦点。画布仅表达布局、未提供拖拽/连线；AI/导出等禁用按钮没有实际模型、业务或支付调用。
- 最新静态扫描 37 项仍为前文解释的分离事件/全局 textarea 样式检测局限；未声明 strict 通过。浏览器核心主链当时 84 个按钮均有实际绑定、委托或提交路径。
- 项目截图和报告见 evidence/project-overview.png、evidence/project-shots.png、evidence/project-canvas.png、evidence/project-live.png、evidence/project-mobile.png、evidence/project-report.json；可用 README 中的命令重现。

## 分镜操作补齐验证

- `shots.qa.mjs` 9 组检查通过：图片草稿取消不写入；手写分镜从库配图与刷新；上传替换时原子加入全库/项目且保留原素材、文字及现场状态；清空图片保留资产；非法/损坏文件可恢复；选图搜索、空结果及手机布局/焦点；多页存储冲突保留草稿且无部分写入；取消/确认移除与历史保留及跨视图同步；最后一个分镜移除后的持久空状态。pageErrors=[]、apiRequests=[]。
- 重新通过 `check.mjs`、项目 9 组、素材 12 组、集合 5 组回归，JavaScript 语法检查通过。新改动仅在静态原型中，未运行生产前后端全量门禁。
- 实际查看桌面和 375px 手机截图，配图保持比例，手机弹窗内滚动、素材双列，无页面/弹窗横向溢出。证据：evidence/shot-image-desktop.png、shot-image-mobile.png、shot-remove.png、shots-report.json。
- 本次未重跑 premium 静态扫描；前述 37 项是上次的历史结果。本次素材浏览器回归发现 86 个静态按钮，未绑定为 0；此统计不代表覆盖所有动态模板。

## 按类型收集素材验证

- `collector.qa.mjs` 10 组浏览器检查通过：逐图字段仅创建图片；图片描述详情/搜索；多行含网址正文只创建一份文字；链接 URL 校验/说明/来源且不抓网页；切换类型取消/确认及刷新；队列增删去重不丢其他说明；超限/损坏图片、整批失败无部分写入及重试；全局拖图不覆盖文字草稿；项目上下文引用；三类手机表单保存可达、焦点圈定和无横向溢出。pageErrors=[]、externalRequests=[]。
- 素材 12 组、项目 9 组、集合 5 组、分镜 9 组回归及 `check.mjs` 通过。旧混合导入用例已按新契约拆为三次创建；素材总数、持久化、关系断言仍保留。语法检查与 git diff --check 通过。
- 实看桌面图片/文字表单与手机图片/链接截图；手机保存操作固定底部，图片保持原比例。走查发现校验错误在修正字段后仍展示，已补输入时清除过期错误并重跑专项通过。截图/报告见 evidence/collect-*、collector-report.json。
- 本次未运行生产门禁，未重跑 premium 静态扫描。素材回归静态按钮计数 89、未绑定为 0，三处静态 textarea 的 computed resize 均为 none；不代表动态模板穷尽审计。

## 素材回收站验证

- `recycle.check.mjs` 通过：输入不变性、移入隐藏、恢复仍存在关系/跳过已删除目标、分镜快照保留、旧数据默认 30 天、不自动清理、精确到期边界与缩短期限清理。
- `recycle.qa.mjs` 7 组通过：单项取消/确认和关系隐藏；入口/默认时长/刷新/恢复；图片文字批量移入、全选与恢复最爱；取消/确认彻底删除且保留分镜；不自动清理及缩短期限影响预览/取消/保存；打开页面按设置清理到期；手机布局、确认焦点圈定和清空。pageErrors=[]、apiRequests=[]。时间推进只修改独立浏览器测试数据，未改正在体验原型的素材。
- 素材 12、项目 9、分镜 9、集合 5、按类型收集 10 组回归，以及 `check.mjs` 通过；语法检查与 diff 空白检查通过。纯本地原型未运行生产前后端门禁。
- 实看桌面/手机回收站，手机页面内滚动，无横向溢出；证据为 evidence/recycle-desktop.png、recycle-mobile.png、recycle-confirm.png、recycle-report.json。恢复完成后先解除 inert 再将焦点放到回收站数量；定时清理不会覆盖未保存的保留时长选择。
- 本轮未重新执行 premium 静态扫描；素材回归看到 98 个静态按钮，未绑定为 0，三处 textarea computed resize 为 none。不把该检查当作动态模板全覆盖。

## 卡片菜单与详情底栏验证

- `card-menu.qa.mjs` 7 组通过：右下角菜单不触发详情/不改变卡片几何；方向键/Home/End/Escape/Tab 与焦点返回；外部点击和页面滚动关闭；集合/项目快捷加入；最爱状态更新；回收确认取消保留素材；详情垃圾桶无可见文字、有名称和图标，四按钮同排；375px/320px 菜单不越界且避开底栏，详情无横向溢出。pageErrors=[]。
- 原素材 12 组、回收站 7 组及 `check.mjs` 回归通过；语法与 diff 空白检查通过。本轮仅修改原型 UI，未跑生产门禁。
- 实看桌面菜单、手机菜单和手机详情底栏。菜单截图等待目标图片显现后采集；证据为 evidence/card-menu-{desktop,mobile}.png、detail-actions-{desktop,mobile}.png、card-menu-report.json。
- 未重跑 premium 静态扫描；主链当时发现 117 个静态按钮，未绑定为 0，textarea computed resize 均 none，不代表动态菜单的穷尽静态审计。

## 标签管理与即时选择验证

- `tags.check.mjs` 通过：旧标签迁移幂等/名称判重/稳定 ID、改名改色和分组、回收站同步、删除标签/分组不删素材、all/any 匹配。
- `tags.qa.mjs` 11 组通过：旧关系持久化，创建分组/带色标签/重复保护，素材收集选择与就地新建原子保存，取消草稿，组合筛选和 URL，已有素材标签编辑，改名颜色不破坏筛选，分组改名删除，回收站恢复不复活删除标签，存储冲突保留输入且无部分写入，手机布局/管理编辑键盘焦点。pageErrors=[]、apiRequests=[]。
- Owner 纠正选择方式后，`tag-live.qa.mjs` 6 组通过：无需应用即更新卡片/数量；多选和取消仍保持面板；Escape 收起保留条件/刷新/清除；创建中就地选取/新建/Enter 不误提交/取消零写入；详情即时保存；375px/320px 不产生二级弹窗且无横向溢出。pageErrors=[]、apiRequests=[]。
- 原素材 12、收集 10、项目 9、集合 5、分镜 9、回收站 7、卡片菜单 7 组回归通过；其中素材/收集在改成非模态选择后再次通过，其余在标签模型接入后通过。几何/模型检查及语法、diff 空白检查通过。未运行生产门禁或实现后端缓存。
- 实看桌面管理、实时筛选、手机管理与就地选择；手机计数原在底部易受导航遮挡，已移到面板头部并重新采集验证。证据：evidence/tag-manager-*、live-filter-*、inline-tags-*、tags-report.json、tag-live-report.json。
- 未重跑 premium 静态扫描；最新素材主链记录 137 个静态按钮、未绑定 0，textarea resize 均 none，不将它当作动态选择器穷尽审计。

## 类型与标签统一工具栏验证

- `browse-toolbar.qa.mjs` 5 组通过：无固定分类/无空标签行，类型标签显示同一工具栏、管理入口归数量栏；真实 kind 筛选及 URL/刷新；旧题材分类忽略、旧文字分类映射、新 URL 移除 category；标签实时筛选与类型组合、空结果重置；821px 评论视口及375/320手机排序/管理入口可达，无横向溢出。视频项通过原生 option.disabled 属性验证禁用。pageErrors=[]。
- 原素材 12、即时标签 6、创建收集 10、项目 9 组及 `check.mjs` 回归通过，语法与 diff 空白检查通过；仅原型修改，未运行生产全量门禁。
- 实看 821px、375px 静态及实时选择截图；正式证据等待图像解码和淡入结束后采集。截图为 evidence/toolbar-{desktop,821,375,320}.png、toolbar-live-{821,375}.png，报告 browse-toolbar-report.json。
- 未重跑 premium 静态扫描；素材主链当时 130 个静态按钮未绑定为0，textarea resize 为 none，不代表动态菜单全覆盖。

## 自绘下拉与焦点验证

- `glass-select.qa.mjs` 6 组通过：共享玻璃 listbox/图标/勾选/视频禁用及几何稳定；指针选择和再次收起后 computed outlineStyle=none 且不匹配 :focus-visible；键盘上下/Home/End 跳过禁用、Enter 提交、Escape 取消，返回后 2px 轮廓/12px 圆角；排序实际顺序与刷新；外部点击/滚动/Tab/其他菜单关闭；375px/320px 视口与底栏边界。pageErrors=[]。
- 工具栏 5 组通过（改用可见自绘控件操作，不强行操控隐藏原生 select），素材 12 组回归通过，语法和 diff 空白检查通过。未跑生产门禁。
- 实看 821px 展开、选择后和手机展开截图。证据：evidence/type-menu-821.png、pointer-after-select-821.png、keyboard-focus-821.png、sort-menu-821.png、type-menu-mobile.png、glass-select-report.json。选择后截图等待淡入/悬停过渡结束采集。
- 未重跑 premium 静态扫描；素材主链记录 132 个静态按钮未绑定为0，textarea resize 均 none，不代表动态选项穷尽审计。

## 集合卡片菜单验证

- `collection-menu.qa.mjs` 6 组通过：仅普通集合有菜单且无嵌套按钮；点击不入集合、键盘和焦点；外层改名留在概览且刷新保留；取消/确认删除、同 URL 列表刷新、保留素材/最爱/项目、删除后焦点；菜单打开与系统入口；375px/320px 边缘和底栏避让。pageErrors=[]。
- 集合原 5 组通过；素材菜单 7 组回归通过。菜单测试暴露滚动定位事件延迟导致菜单闪退，已改为检查触发点位移，无需等待补丁即可通过。语法与 diff 空白检查通过；本次纯原型不跑生产门禁。
- 实看 821px 与手机菜单截图，证据为 evidence/collection-menu-821.png、collection-menu-mobile.png、collection-menu-report.json。本轮未重跑 premium 静态扫描。
