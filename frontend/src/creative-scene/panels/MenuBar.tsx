import type { MenuKind } from '../state/sceneStore.ts'

const menus: { menu: MenuKind; label: string }[] = [
  { menu: 'add', label: '添加' },
  { menu: 'object', label: '物件' },
  { menu: 'view', label: '视图' },
]

export function MenuBar({ onOpen, disabled }: { onOpen: (menu: MenuKind, x: number, y: number) => void; disabled: (menu: MenuKind) => boolean }) {
  return (
    <nav className="cc-scene-menubar" aria-label="场景菜单">
      {menus.map(({ menu, label }) => (
        <button
          key={menu}
          disabled={disabled(menu)}
          onClick={(e) => {
            const r = e.currentTarget.getBoundingClientRect()
            onOpen(menu, r.left, r.bottom + 4)
          }}
        >
          {label}
        </button>
      ))}
    </nav>
  )
}
