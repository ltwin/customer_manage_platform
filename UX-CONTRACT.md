# 创意空间交互归属

本文记录本轮新增界面的交互后果；产品规则来自 `.codestable/epics/creative-workspace-redesign.md` 与执行游标的 owner 决定，不重新定义经营或权限规则。

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
|---|---|---|---|---|
| Select/Listbox | 原生 select + .input | 既有 settings / index.css；本轮接受操作系统菜单 | 类型、分组、关联对象与目标空间 | 浏览器键盘与 375px |
| Form | .input + 就地表单 | Epic DEC-15/16、共享 index.css | 创建零必填；名称/关联/备忘为可选编辑 | HTTP 与浏览器保存/失败 |
| Scrollbar | frontend/src/index.css | 现有全局 scrollbar token | 自然页面滚动；素材长文内部滚动 | 浏览器 computed style |
| Toast | AppShell / useShell.notify | 共享 shellContext | 成功提示；错误保留在页内 | aria-live 与保存结果 |
| CRUD | creative/api.ts 与空间页面 | OpenAPI creative-workspace；Epic DEC-3/9/17 | 创建直接进空灵感墙；归档后保留内容；停止后只读 | 真库测试与浏览器主链 |
| Confirmation | ConfirmDialog + useFocusTrap | 共享确认组件 | 移除素材/备忘与停止试用；归档可恢复不额外确认 | 取消焦点、Tab、Escape、焦点归还 |
| Upload | WorkspacePage 的搬入队列 | Epic ITEM-4、planningmedia 图像管线 | 多行粘贴、多图选择与拖入 | 每文件失败保留、同键重试 |

账号隔离与写权限由服务端决定；禁用状态只是反馈。网络失败保留输入；批量创建重试复用同一幂等键，成功后才清空。现场只对服务端成功响应显示结果，断网不排队写入拍摄结果。离线打开仅暂记为未核验的使用观察，恢复网络后补报，不能变成已核验现场事实。

导航名称为「创意空间」；「旧策划记录」保留原路由，放在历史入口。关联仅跳转，禁止带出档期、计价或提醒。未命名空间显示名可回落到关联对象名。

语言为简体中文，界面用「摄影师 / 账号 / 客户」的项目术语。原生控件接受操作系统的交互；搜索输入须兼容中文输入法。主动作有 pending、disabled 与错误状态；拖动排序有按钮替代操作。验收证据在 `.codestable/work/epic-creative-workspace-redesign.md` 记录。
