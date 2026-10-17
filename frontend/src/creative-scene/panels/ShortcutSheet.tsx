import { X } from 'lucide-react'
import { bindings, gestures, keysLabel, type Group } from '../input/keymap.ts'

const order: Group[] = ['导航', '选择', '变换', '编辑', '视图', '通用']

/** Everything here is read from the keymap, so it matches what keys do. */
export function ShortcutSheet({ mac, onClose }: { mac: boolean; onClose: () => void }) {
  return (
    <div className="cc-scene-sheet" role="dialog" aria-label="快捷键速查">
      <header>
        <h3>快捷键速查</h3>
        <button className="btn" aria-label="关闭" onClick={onClose}>
          <X size={16} />
        </button>
      </header>
      <div className="cc-scene-sheet-body">
        {order.map((group) => {
          const keys = bindings.filter((b) => b.group === group && !b.contexts.includes('modal'))
          const modalKeys = bindings.filter((b) => b.group === group && b.contexts.includes('modal'))
          const moves = gestures.filter((g) => g.group === group)
          return (
            <section key={group}>
              <h4>{group}</h4>
              <dl>
                {moves.map((g) => (
                  <div key={g.keys}>
                    <dt>{g.keys}</dt>
                    <dd>{g.label}</dd>
                  </div>
                ))}
                {keys.map((b) => (
                  <div key={b.action + b.codes[0]}>
                    <dt><kbd>{keysLabel(b, mac)}</kbd></dt>
                    <dd>{b.label}</dd>
                  </div>
                ))}
                {modalKeys.map((b) => (
                  <div key={'m' + b.action}>
                    <dt><kbd>{keysLabel(b, mac)}</kbd></dt>
                    <dd>{b.label}（变换中）</dd>
                  </div>
                ))}
              </dl>
            </section>
          )
        })}
      </div>
    </div>
  )
}
