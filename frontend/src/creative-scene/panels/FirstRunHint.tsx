import { useState } from 'react'
import { X } from 'lucide-react'

const seenKey = 'creative-scene:first-run-seen'

function seen(): boolean {
  try {
    return window.localStorage.getItem(seenKey) === '1'
  } catch {
    return false
  }
}

/** A one-time card; if storage is unavailable it simply shows every time. */
export function FirstRunHint({ onShortcuts }: { onShortcuts: () => void }) {
  const [open, setOpen] = useState(() => !seen())
  if (!open) return null
  const close = () => {
    setOpen(false)
    try {
      window.localStorage.setItem(seenKey, '1')
    } catch {
      // Shown again next time; harmless.
    }
  }
  return (
    <aside className="cc-scene-hint-card" aria-label="上手提示">
      <button className="cc-scene-hint-close" aria-label="关闭提示" onClick={close}>
        <X size={14} />
      </button>
      <h4>这里的操作和 Blender 一样</h4>
      <ul>
        <li>中键拖动环绕视角，Shift + 中键平移；没有中键时用 Alt + 左键。触控板可在「视图」菜单切换。</li>
        <li>Shift + A 添加物件；选中后按 G 移动、R 旋转、S 缩放，再按 X / Y / Z 锁轴，输入数字精确调整。</li>
        <li>修改会自动保存，Cmd/Ctrl + Z 只撤销你自己的操作。</li>
      </ul>
      <button className="btn" onClick={onShortcuts}>
        查看全部快捷键
      </button>
    </aside>
  )
}
