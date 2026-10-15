import { Cpu } from 'lucide-react'
import type { components } from '../../api/schema'
import type { PickerOption } from './Picker.tsx'

type Catalog = components['schemas']['CreativeAgentCatalog']

// 助手的模型选项，画布面板和首页一句话开始共用；Skill 见 AgentSkills。
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
// 默认值与画布内助手一致：目录里第一个可用的模型。
export function defaultAgentModel(catalog: Catalog): string {
  return catalog.models.find((item) => item.available)?.model_key ?? ''
}
