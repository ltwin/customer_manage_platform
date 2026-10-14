import { useState } from 'react'
import { useFocusTrap } from '../../components/useFocusTrap'
import { MediaFigure } from '../editor/MediaView.tsx'
import { readableFileName } from '../editor/media.ts'
import { fileTitle } from '../editor/fileImport.ts'
import type { Duplicate } from './importDuplicates.ts'

export type DuplicateChoice = 'add' | 'replace' | 'skip'

/** 导入的文件与资产库里的素材内容相同：问一次是新增一份还是替换已有的那份。 */
export default function DuplicateDialog({
  item,
  remaining,
  grouped,
  busy,
  error,
  onChoose,
}: {
  item: Duplicate
  /** 排在这一项后面、还没处理的重复文件数。 */
  remaining: number
  /** 这次导入是否放进某个分组。 */
  grouped: boolean
  busy: boolean
  error: string
  onChoose: (choice: DuplicateChoice, rest: boolean) => void
}) {
  const [rest, setRest] = useState(false)
  const ref = useFocusTrap<HTMLDivElement>(
    true,
    () => onChoose('skip', rest),
    !busy,
  )
  const { asset, file } = item
  const trashed = !!asset.deleted_at
  const name = readableFileName(file.name)
  const title = fileTitle(file.name)
  return (
    <div className="overlay open">
      <div
        ref={ref}
        className="dialog al-duplicate-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="al-duplicate-title"
        tabIndex={-1}
      >
        <h2 id="al-duplicate-title">
          {trashed ? '回收站里有相同内容' : '资产库里已有相同内容'}
        </h2>
        <p>
          「{name}」与{trashed ? '回收站里' : '资产库里'}的「{asset.title}
          」内容完全相同。
        </p>
        <figure className="al-duplicate-existing">
          {asset.content && !asset.unavailable ? (
            <MediaFigure
              content={asset.content}
              kind={asset.kind}
              title={asset.title}
            />
          ) : (
            <div className="al-duplicate-missing">预览不可用</div>
          )}
          <figcaption>{asset.title}</figcaption>
        </figure>
        <ul className="al-duplicate-help">
          <li>新增：再存一份，两份互不影响。</li>
          <li>
            替换：{trashed ? '从回收站恢复原素材，' : ''}标题改为「{title}」
            {grouped ? '并加入当前分组' : ''}
            ，评分、标签和原有分组保留，不再重复上传。
          </li>
        </ul>
        {remaining > 0 && (
          <label className="al-duplicate-rest">
            <input
              type="checkbox"
              checked={rest}
              disabled={busy}
              onChange={(e) => setRest(e.currentTarget.checked)}
            />
            其余 {remaining} 个重复文件也这样处理
          </label>
        )}
        {error && <p role="alert">{error}</p>}
        <div className="dialog-actions">
          <button
            type="button"
            className="btn"
            disabled={busy}
            onClick={() => onChoose('skip', rest)}
          >
            跳过
          </button>
          <button
            type="button"
            className="btn"
            disabled={busy}
            onClick={() => onChoose('add', rest)}
          >
            新增
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy}
            data-autofocus
            onClick={() => onChoose('replace', rest)}
          >
            {busy ? '正在替换…' : error ? '重试替换' : '替换'}
          </button>
        </div>
      </div>
    </div>
  )
}
