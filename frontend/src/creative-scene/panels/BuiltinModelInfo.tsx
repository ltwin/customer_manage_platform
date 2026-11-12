import { useStore } from 'zustand'
import { modelCache } from '../models/cache.ts'
import { builtinModels, type BuiltinModel } from '../models/catalog.ts'
import { builtinKey } from '../models/key.ts'

// A built-in model in the properties panel (data: 内置模型库): its name, where
// it came from and its licence, in place of the parameters and code a code
// model has; and the other entries of its category to swap it for, which
// bring their own materials (design「内置模型」). When its file cannot be
// read (not imported yet: 503), it says so beside the grey block (AC-12).

export function BuiltinModelInfo({ entry, readOnly, onSwap }: { entry: BuiltinModel; readOnly: boolean; onSwap: (to: BuiltinModel) => void }) {
  const others = builtinModels.filter((m) => m.id !== entry.id && m.category === entry.category)
  const failed = useStore(modelCache.failed, (s) => !!s[builtinKey(entry.id)])
  return (
    <fieldset className="cc-scene-model-params">
      <legend>内置模型</legend>
      <dl className="cc-scene-import-source">
        <dt>名称</dt>
        <dd>{entry.label}</dd>
        <dt>来源</dt>
        <dd>
          <a href={entry.source.url} target="_blank" rel="noopener noreferrer">
            {entry.source.author} · {entry.source.asset}
          </a>
        </dd>
        <dt>授权</dt>
        <dd>{entry.source.license}</dd>
      </dl>
      {failed && <small role="alert">文件暂时读不到：视图里先用灰色方块占位，导出会失败，稍后再试</small>}
      {others.map((m) => (
        <button key={m.id} type="button" className="btn" disabled={readOnly} title="连同材质一起换：改过的材质会换成它自己的" onClick={() => onSwap(m)}>
          换成{m.label}
        </button>
      ))}
    </fieldset>
  )
}
