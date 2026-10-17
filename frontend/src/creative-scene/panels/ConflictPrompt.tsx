import type { ConflictGroup } from '../state/sync.ts'

export function ConflictPrompt({ groups, trashed, onReapply, onDiscard }: { groups: ConflictGroup[]; trashed: boolean; onReapply: (id: string) => void; onDiscard: (id: string) => void }) {
  if (!groups.length) return null
  return (
    <div className="cc-scene-conflicts" role="alert">
      {groups.map((g) => (
        <div key={g.id} className="cc-scene-conflict">
          <p>
            {g.message}
            {g.intents.length > 1 && `（连同后续 ${g.intents.length - 1} 处修改）`}
          </p>
          <div>
            {g.kind !== 'invalid' && (
              <button className="btn" disabled={trashed} onClick={() => onReapply(g.id)}>
                {g.deletes ? '仍然删除' : '重新应用我的修改'}
              </button>
            )}
            <button className="btn" onClick={() => onDiscard(g.id)}>
              放弃
            </button>
          </div>
        </div>
      ))}
    </div>
  )
}
