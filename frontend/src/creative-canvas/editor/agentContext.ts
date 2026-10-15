import type { components } from '../../api/schema'

export type ContextUsage = components['schemas']['CreativeAgentContextUsage']
type Model = components['schemas']['CreativeAgentModel']
type Segment = components['schemas']['CreativeAgentInstructionSegment']
export type Skill = components['schemas']['CreativeAgentSkill']

// 输入框里以 / 开头的命令。本期只有 compact；内置 Skill 也从斜杠调出。
export interface AgentCommand {
  name: 'compact'
  description: string
}

export const agentCommands: AgentCommand[] = [
  { name: 'compact', description: '整理较早的对话，减少上下文占用' },
]

const commandPattern = /^\/(compact)(?:\s+([\s\S]*))?$/

// 输入框里是一条完整命令时解析出来；不认识的 /xxx 当普通文字发送。
export function parseCommand(
  draft: string,
): { name: AgentCommand['name']; args: string } | null {
  const match = commandPattern.exec(draft.trim())
  if (!match) return null
  return {
    name: match[1] as AgentCommand['name'],
    args: (match[2] ?? '').trim(),
  }
}

// 只输入了 / 和命令名的一部分时，列出可选的命令。
export function commandSuggestions(draft: string): AgentCommand[] {
  const match = /^\/([a-z]*)$/.exec(draft)
  if (!match) return []
  return agentCommands.filter((command) => command.name.startsWith(match[1]))
}

// 回车时把唯一匹配但没写全的命令补全，而不是当文字发出去。
export function completeCommand(draft: string): string | null {
  const options = commandSuggestions(draft)
  if (options.length !== 1 || draft === `/${options[0].name}`) return null
  return `/${options[0].name} `
}

// 首版只开放内置 Skill（平台目录）；账号自建的随创建入口一起开。
export function builtinSkills(skills: Skill[]): Skill[] {
  return skills.filter((item) => item.origin === 'platform' && item.available)
}

export function skillMatches(skill: Skill, query: string): boolean {
  const q = query.trim().toLowerCase()
  return (
    !q ||
    [skill.display_name, skill.slug, skill.description].some((value) =>
      value.toLowerCase().includes(q),
    )
  )
}

export type SlashItem =
  | ({ kind: 'command' } & AgentCommand)
  | { kind: 'skill'; skill: Skill }

// 斜杠菜单：命令在前，内置 Skill 在后；Skill 按名称、slug 或说明匹配。
export function slashSuggestions(draft: string, skills: Skill[]): SlashItem[] {
  const match = /^\/(\S*)$/.exec(draft)
  if (!match) return []
  return [
    ...commandSuggestions(draft).map(
      (command): SlashItem => ({ kind: 'command', ...command }),
    ),
    ...skills
      .filter((skill) => skillMatches(skill, match[1]))
      .map((skill): SlashItem => ({ kind: 'skill', skill })),
  ]
}

// 打全 slug 再敲空格，就当选了这个 Skill，后面的字留在输入框里。
export function typedSkill(
  draft: string,
  skills: Skill[],
): { skill: Skill; text: string } | null {
  const match = /^\/(\S+)\s([\s\S]*)$/.exec(draft)
  const skill = match && skills.find((item) => item.slug === match[1])
  return skill ? { skill, text: match[2].trimStart() } : null
}

export function commandSegment(command: {
  name: AgentCommand['name']
  args: string
}): Segment {
  return {
    type: 'command',
    command: command.name,
    ...(command.args ? { text: command.args } : {}),
  }
}

// 几处来源（会话首页、各运行的事件）里最新测量的那一份。
export function latestUsage(
  candidates: (ContextUsage | null | undefined)[],
): ContextUsage | null {
  let latest: ContextUsage | null = null
  for (const usage of candidates) {
    if (
      usage &&
      (!latest ||
        Date.parse(usage.measured_at) > Date.parse(latest.measured_at))
    )
      latest = usage
  }
  return latest
}

export const partLabels: Record<ContextUsage['parts'][number]['key'], string> =
  {
    system: '系统指令',
    tools: '工具定义',
    skill: 'Skill',
    summary: '交接摘要',
    messages: '对话消息',
    tool_results: '工具结果',
    images: '图片',
  }

// 中文界面按「万」读：1.2 万，不足一万照写。
export function tokenLabel(tokens: number): string {
  if (tokens >= 10_000) return `${(tokens / 10_000).toFixed(1)} 万`
  return tokens.toLocaleString('zh-CN')
}

export interface RingPart {
  key: ContextUsage['parts'][number]['key']
  label: string
  tokens: number
  percent: number
}

export interface RingView {
  // 从未发过请求：只画空环，不给预估值（owner 2026-10-14）。
  empty: boolean
  window: number
  used: number
  percent: number
  // 总数也是估算：压缩之后还没测量，或输入框选的模型与上次请求不同。
  estimated: boolean
  level: 'calm' | 'warm' | 'hot'
  compactAt: number
  remaining: number
  // 距自动压缩还剩多少；不自动压缩时为 null，已过线时为 0。
  untilCompact: number | null
  parts: RingPart[]
}

// 小环与浮层要显示的一切。窗口按输入框当前选的模型计算。
export function ringView(
  usage: ContextUsage | null,
  model: Pick<Model, 'model_key' | 'context_window_tokens'> | undefined,
  compactPercent: number,
): RingView {
  const window = model?.context_window_tokens ?? usage?.window_tokens ?? 0
  const compactAt =
    compactPercent > 0 ? Math.floor((window * compactPercent) / 100) : 0
  if (!usage) {
    return {
      empty: true,
      window,
      used: 0,
      percent: 0,
      estimated: false,
      level: 'calm',
      compactAt,
      remaining: window,
      untilCompact: compactAt || null,
      parts: [],
    }
  }
  const used = usage.used_tokens
  const share = (tokens: number) =>
    window > 0 ? Math.min(100, Math.round((tokens * 100) / window)) : 0
  const percent = share(used)
  return {
    empty: false,
    window,
    used,
    percent,
    estimated:
      usage.estimated || (!!model && model.model_key !== usage.model_key),
    level:
      compactPercent > 0 && percent >= compactPercent
        ? 'hot'
        : percent >= 60
          ? 'warm'
          : 'calm',
    compactAt,
    remaining: Math.max(0, window - used),
    untilCompact: compactAt ? Math.max(0, compactAt - used) : null,
    parts: usage.parts.map((part) => ({
      key: part.key,
      label: partLabels[part.key],
      tokens: part.tokens,
      percent: share(part.tokens),
    })),
  }
}
