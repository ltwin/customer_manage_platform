// 各宿主的接入文字。地址来自服务端（列表接口的 mcp_url），这里不写死域名。
// 菜单名称按各宿主 2026-10 的界面写；宿主改了界面，只改这里的文字。

export type HostID = 'claude_code' | 'codex' | 'claude' | 'chatgpt'

export type CopyText = { text: string; label: string }

export type HostGuide = {
  id: HostID
  name: string
  sub: string
  mark: string
  title: string
  // agent：整段发给助手，让它自己执行
  agent?: CopyText & { lead: string }
  // steps：只能自己动手的步骤；其中的 MCP 地址显示成代码样式
  steps?: string[]
  tip?: string
  fallback?: { title: string; text: string; copy: CopyText; switchTo?: HostID }
}

export const connectorName = '创意空间'

function adminText(where: string, kind: string, mcpUrl: string): string {
  return [
    `你好，我想在${where}里用创意空间（我的创作工具）。请帮忙在组织设置里添加一个${kind}：`,
    `名称：${connectorName}`,
    `地址：${mcpUrl}`,
    '认证：OAuth（每个成员各自登录授权自己的账号，彼此看不到数据）',
    '添加后我自己完成授权就行，谢谢！',
  ].join('\n')
}

export function hostGuides(mcpUrl: string): HostGuide[] {
  return [
    {
      id: 'claude_code',
      name: 'Claude Code',
      sub: '终端',
      mark: '✳',
      title: '让 Claude Code 添加连接',
      agent: {
        lead: '复制这段话，发给 Claude Code，它会自己执行：',
        text: `帮我连接创意空间：执行 claude mcp add --transport http --scope user creative ${mcpUrl}，然后提醒我输入 /mcp，选「creative」，在浏览器里完成授权。`,
        label: '复制发给 Claude Code',
      },
      tip: '--scope user 让这台电脑上所有项目都能用；Claude 桌面端的 Code 模式也会用这份配置。',
      fallback: {
        title: '已经在 Claude 里添加了连接器？',
        text: '登录同一个 claude.ai 账号的 Claude Code 会自动看到它，不用再添加。在 Claude Code 里输入 /mcp，选「创意空间」，按提示授权即可。',
        copy: { text: '/mcp', label: '复制 /mcp' },
      },
    },
    {
      id: 'codex',
      name: 'Codex',
      sub: '终端',
      mark: '›',
      title: '让 Codex 添加连接',
      agent: {
        lead: '复制这段话，发给 Codex，它会自己执行：',
        text: `帮我连接创意空间：\n1. 执行 codex mcp add creative --url ${mcpUrl}\n2. 执行 codex mcp login creative，在打开的浏览器里授权\n3. 完成后提醒我新开一个会话。`,
        label: '复制发给 Codex',
      },
      tip: '配置写在 ~/.codex/config.toml，这台电脑上的所有会话都能用。',
    },
    {
      id: 'claude',
      name: 'Claude',
      sub: '桌面端与网页',
      mark: 'C',
      title: '在 Claude 里添加连接器',
      steps: [
        '打开 Claude 的「设置 → 连接器」，点「添加自定义连接器」。',
        `名称填「${connectorName}」，地址填 ${mcpUrl}。`,
        '点「连接」，在打开的授权页确认是你的账号，点「允许」。',
        '在对话框的「＋ → 连接器」里打开「创意空间」。',
      ],
      tip: '桌面端、网页和登录同一账号的 Claude Code 共用这个连接器，加一次就行。',
      fallback: {
        title: '看不到「添加自定义连接器」？',
        text: 'Team 和 Enterprise 由组织管理员添加。把下面这段发给管理员；等不及的话，可以先用 Claude Code 的本机配置。',
        copy: { text: adminText(' Claude ', '自定义连接器', mcpUrl), label: '复制发给管理员' },
        switchTo: 'claude_code',
      },
    },
    {
      id: 'chatgpt',
      name: 'ChatGPT',
      sub: '网页与桌面端',
      mark: 'G',
      title: '在 ChatGPT 里添加应用',
      steps: [
        '打开 ChatGPT 的「设置 → 应用 → 高级设置」，打开「开发者模式」。',
        `点「创建应用」：名称填「${connectorName}」，MCP 服务器地址填 ${mcpUrl}，认证选 OAuth。`,
        '保存后会打开创意空间的授权页，确认是你的账号，点「允许」。',
        '新对话里点「＋」选择「创意空间」，就可以开始了。',
      ],
      fallback: {
        title: '工作区里看不到「开发者模式」？',
        text: 'Business 和 Enterprise 工作区要由管理员开启开发者模式，或者由管理员添加这个应用。把下面这段发给管理员；也可以先用 Codex。',
        copy: { text: adminText(' ChatGPT 工作区', '自定义应用（MCP）', mcpUrl), label: '复制发给管理员' },
        switchTo: 'codex',
      },
    },
  ]
}

// 第一次连上后可以试的话。只列已经开放的能力。
export const starters = ['看看我最近的画布', '读一下最近那张画布，说说用了哪些参考图', '在资产库里找海边的素材']
