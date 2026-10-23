## creative/external-agents: ADD 接入外部助手

创作者可以让常用的外部助手（Claude Code、Codex CLI、Claude 桌面端与网页、ChatGPT）连上自己的创意空间账号。授权只在本站的确认页完成，不需要把密码交给外部助手。开关 `MCP_ENABLED` 关闭时，这项能力整体不存在，内置助手不受影响。

### Scenario: 从接入页接入
- GIVEN 创作者已登录，外部助手已开放
- WHEN 创作者在「创意空间 → 外部助手」选 Claude Code 或 Codex CLI，把给出的那段话发给它
- THEN 宿主打开本站的授权确认页，页面写明发起连接的客户端、授权后它能做什么、读到的内容会发给它使用的 AI 模型服务；点「允许」后浏览器回到宿主，宿主能列出 6 个只读工具

### Scenario: 没登录时授权
- GIVEN 创作者在浏览器里没有登录
- WHEN 宿主打开授权确认页
- THEN 先进入登录页，登录后回到同一个确认页，不需要宿主重新发起

### Scenario: 拒绝授权
- GIVEN 创作者在确认页
- WHEN 点「拒绝」
- THEN 不建立连接，宿主收到拒绝

### Scenario: 发起方不可信
- GIVEN 授权请求的客户端无法识别，或回调地址不在它登记的范围内
- WHEN 浏览器打开授权请求
- THEN 只在本站显示错误，不跳转到任何外部地址

### Scenario: 续期
- GIVEN 外部助手的访问令牌已过期
- WHEN 宿主用 refresh token 换新令牌
- THEN 换到新令牌，不需要创作者再次授权；同一个旧 refresh token 再被使用时，整个连接被断开，宿主需要重新授权

### Scenario: 没有新建连接器权限
- GIVEN 创作者在接入页选 Claude 桌面端或 ChatGPT
- WHEN 查看回退方法
- THEN 页面给出一段可以发给管理员的文字；Claude 桌面端另给「改用 Claude Code」的路径

### Scenario: 外部助手未开放
- GIVEN 部署没有开启外部助手
- WHEN 创作者打开接入页，或宿主访问 MCP 地址和授权地址
- THEN 接入页显示「外部助手暂未开放」，MCP 与授权地址都不存在

## creative/external-agents: ADD 外部助手读取创意空间

已连接的外部助手能读取授权账号的画布、素材和资产库，与内置助手读到的内容和拒绝原因一致。它看不到客户、订单、日程等经营数据，也看不到其他账号的任何内容。

### Scenario: 自检
- GIVEN 外部助手已连接
- WHEN 它调用 `get_capabilities`
- THEN 得到账号显示名、连接署名、可用的生成模型、本月剩余额度，以及创作者有没有打开创意空间页面

### Scenario: 看最近的画布
- GIVEN 账号里有画布
- WHEN 创作者说「看看我最近的画布」
- THEN 外部助手用 `list_canvases` 列出画布，每张带可点击的画布链接；用 `read_canvas` 读到节点；用 `read_asset` 看到引用的图片；用 `search_assets`、`read_library` 查找资产库

### Scenario: 结果在终端里也能看懂
- GIVEN 外部助手在 Claude Code 或 Codex CLI 里调用任一工具
- WHEN 宿主只显示结果的前几行
- THEN 第一行是能单独看懂的结论，画布链接是 Markdown 链接，结果里没有卡片字段

### Scenario: 别的账号的内容
- GIVEN 外部助手拿到了另一个账号的画布 ID 或内容修订 ID
- WHEN 用它调用读工具
- THEN 结果是 `not_found`，看不到任何内容

### Scenario: 令牌不通用
- GIVEN 一个网页端登录令牌和一个外部助手令牌
- WHEN 用网页端令牌访问 MCP 地址，或用外部助手令牌调用网页端接口
- THEN 都被拒绝（401）

## creative/external-agents: ADD 外部助手的连接与断开

创作者能看到哪些外部助手连着自己的账号，随时断开。外部助手最近 5 分钟内调用过工具时，画布页顶栏显示它的名字。

### Scenario: 画布页显示已连接
- GIVEN 外部助手在最近 5 分钟内调用过工具
- WHEN 创作者打开任一画布
- THEN 顶栏显示「<署名> 已连接」，点开能看到最近调用时间和「管理连接」

### Scenario: 一段时间不调用
- GIVEN 顶栏显示某个外部助手已连接
- WHEN 它超过 5 分钟没有调用工具
- THEN 顶栏的指示消失，接入页的连接仍在，显示最近一次调用的时间

### Scenario: 断开
- GIVEN 接入页的「已连接」里有一个外部助手
- WHEN 创作者点「断开」并确认
- THEN 那一行显示「已断开」；该外部助手的下一次请求被拒绝，需要重新授权；画布页顶栏不再显示它
