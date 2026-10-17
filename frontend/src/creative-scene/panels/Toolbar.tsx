import { BoxSelect, Move, MousePointer2, Rotate3d, Scaling } from 'lucide-react'
import type { Tool } from '../state/sceneStore.ts'

const tools: { tool: Tool; label: string; hint: string; icon: typeof Move }[] = [
  { tool: 'box', label: '框选', hint: '左键拖动框选（W 切换）', icon: BoxSelect },
  { tool: 'tweak', label: '调整', hint: '拖动物件即移动（W 切换）', icon: MousePointer2 },
  { tool: 'move', label: '移动', hint: '显示移动手柄', icon: Move },
  { tool: 'rotate', label: '旋转', hint: '显示旋转手柄', icon: Rotate3d },
  { tool: 'scale', label: '缩放', hint: '显示缩放手柄', icon: Scaling },
]

export function Toolbar({ tool, onTool, disabled }: { tool: Tool; onTool: (t: Tool) => void; disabled: boolean }) {
  return (
    <div className="cc-scene-toolbar" role="toolbar" aria-label="工具">
      {tools.map(({ tool: t, label, hint, icon: Icon }) => (
        <button key={t} className={t === tool ? 'is-active' : ''} aria-pressed={t === tool} title={`${label} · ${hint}`} aria-label={label} disabled={disabled && t !== 'box'} onClick={() => onTool(t)}>
          <Icon size={18} />
        </button>
      ))}
    </div>
  )
}
