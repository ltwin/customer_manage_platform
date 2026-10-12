import { Cpu, MessageCircle, Wand2 } from 'lucide-react'
import type { components } from '../../api/schema'
import type { PickerOption } from './Picker.tsx'

type Catalog = components['schemas']['CreativeAgentCatalog']

// 助手的模型与 Skill 选项，画布面板和首页一句话开始共用。
export function agentModelOptions(catalog: Catalog | null): PickerOption[] {
  return (
    catalog?.models.map((item) => ({
      value: item.model_key,
      label: item.display_name,
      icon: <Cpu size={15} />,
      meta: item.capability.image_input ? '识图' : undefined,
      description: item.available
        ? undefined
        : item.unavailable_reason || '暂不可用',
      disabled: !item.available,
    })) ?? []
  )
}
export function agentSkillOptions(catalog: Catalog | null): PickerOption[] {
  return [
    {
      value: '',
      label: '普通对话',
      description: '自由讨论创作想法，助手按需读写画布',
      icon: <MessageCircle size={15} />,
    },
    ...(catalog?.skills
      .filter((item) => item.available)
      .map((item) => ({
        value: item.skill_version_id,
        label: item.display_name,
        description: item.description,
        icon: <Wand2 size={15} />,
      })) ?? []),
  ]
}
// 默认值与画布内助手一致：目录里第一个可用的模型。
export function defaultAgentModel(catalog: Catalog): string {
  return catalog.models.find((item) => item.available)?.model_key ?? ''
}
