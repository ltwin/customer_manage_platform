import { humanBones, requiredBones } from '../rig/bones.ts'
import type { FileSkeleton } from '../rig/detect.ts'
import { jointTree, type Humanoid } from './mapping.ts'

// 手动指定人形映射（design「导入时保留骨骼并回写节点下标」5）：按身体部位分组，
// 每根标准骨骼一个下拉框，可选项是关节名，按层级缩进。改动交给对话框即时重新校验。

const sides = { left: '左', right: '右' } as const
const parts: Record<string, string> = {
  hips: '髋',
  spine: '脊柱',
  chest: '胸',
  upperChest: '上胸',
  neck: '颈',
  head: '头',
  Eye: '眼',
  jaw: '下颌',
  UpperLeg: '大腿',
  LowerLeg: '小腿',
  Foot: '脚',
  Toes: '脚趾',
  Shoulder: '肩',
  UpperArm: '上臂',
  LowerArm: '前臂',
  Hand: '手',
  Thumb: '拇指',
  Index: '食指',
  Middle: '中指',
  Ring: '无名指',
  Little: '小指',
  Metacarpal: '掌骨',
  Proximal: '近节',
  Intermediate: '中节',
  Distal: '远节',
}

/** leftIndexProximal → 左食指近节。 */
function boneLabel(b: string): string {
  const side = /^(left|right)/.exec(b)?.[1] as keyof typeof sides | undefined
  const rest = side ? b.slice(side.length) : b
  const words = side ? (rest.match(/[A-Z][a-z]+/g) ?? []) : [rest]
  return (side ? sides[side] : '') + words.map((w) => parts[w] ?? w).join('')
}

const isFinger = (b: string) => /(Thumb|Index|Middle|Ring|Little)/.test(b)
const groups: { title: string; bones: string[]; open: boolean }[] = [
  { title: '躯干和头', bones: humanBones.filter((b) => !/^(left|right)/.test(b) || b.endsWith('Eye')), open: true },
  ...(['left', 'right'] as const).flatMap((s) => [
    { title: `${sides[s]}臂`, bones: humanBones.filter((b) => b.startsWith(s) && /(Shoulder|Arm|Hand)$/.test(b)), open: true },
    { title: `${sides[s]}腿`, bones: humanBones.filter((b) => b.startsWith(s) && /(Leg|Foot|Toes)$/.test(b)), open: true },
  ]),
  ...(['left', 'right'] as const).map((s) => ({ title: `${sides[s]}手手指`, bones: humanBones.filter((b) => b.startsWith(s) && isFinger(b)), open: false })),
]
const required = new Set(requiredBones)

export default function RigMapping({ skeleton, humanoid, onChange, disabled }: { skeleton: FileSkeleton; humanoid: Humanoid; onChange: (h: Record<string, number>) => void; disabled?: boolean }) {
  const joints = jointTree(skeleton)
  const set = (b: string, value: string) => {
    const next: Record<string, number> = { ...humanoid }
    if (value === '') delete next[b]
    else next[b] = Number(value)
    onChange(next)
  }
  return (
    <div className="cc-rig-mapping" aria-label="手动指定人形映射">
      {groups.map((g) => (
        <details key={g.title} open={g.open}>
          <summary>{g.title}</summary>
          {g.bones.map((b) => (
            <label key={b} className="cc-rig-bone">
              <span>
                {boneLabel(b)}
                {required.has(b) && <span aria-label="必需"> *</span>}
                <small className="cc-muted"> {b}</small>
              </span>
              <select className="input" name={b} value={humanoid[b] ?? ''} disabled={disabled} onChange={(e) => set(b, e.target.value)}>
                <option value="">（不指定）</option>
                {joints.map((j) => (
                  <option key={j.node} value={j.node}>
                    {'　'.repeat(j.depth) + j.name}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </details>
      ))}
    </div>
  )
}
