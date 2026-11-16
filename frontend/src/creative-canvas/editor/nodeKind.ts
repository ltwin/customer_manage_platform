// 画布节点类型的名称，画布卡片和节点树共用；图标见 KindIcon.tsx。
export const nodeKind = (typeKey: string): string =>
  typeKey.startsWith('core.') ? typeKey.slice(5) : typeKey

export const kindLabel: Record<string, string> = {
  text: '文字',
  link: '文字',
  image: '图片',
  video: '视频',
  audio: '音频',
  character: '角色卡',
  group: '布局分组',
  scene: '场景',
}

export const untitled = (typeKey: string) =>
  typeKey === 'core.group'
    ? '未命名分组'
    : `未命名${kindLabel[nodeKind(typeKey)] ?? '节点'}`
