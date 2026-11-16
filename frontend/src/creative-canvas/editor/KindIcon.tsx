import {
  AudioLines,
  Box as BoxIcon,
  Contact,
  FileText,
  Film,
  Folder,
  Image as ImageIcon,
  Link2,
  Type,
} from 'lucide-react'

// 画布节点类型的图标，画布卡片和节点树共用。
export function KindIcon({
  typeKey,
  size = 14,
}: {
  typeKey: string
  size?: number
}) {
  switch (typeKey) {
    case 'core.group':
      return <Folder size={size} />
    case 'internal.document':
      return <FileText size={size} />
    case 'core.scene':
      return <BoxIcon size={size} />
    case 'core.text':
    case 'core.link':
      return <Type size={size} />
    case 'core.character':
      return <Contact size={size} />
    case 'core.image':
      return <ImageIcon size={size} />
    case 'core.video':
      return <Film size={size} />
    case 'core.audio':
      return <AudioLines size={size} />
    default:
      return <Link2 size={size} />
  }
}
