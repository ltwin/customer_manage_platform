import { useState } from 'react'
import { useFocusTrap } from '../../components/useFocusTrap'
import OrganizationPicker from './OrganizationPicker.tsx'
import { emptyOrganization, type Editor, type Catalog } from './libraryState.ts'
import { errorMessage } from './queue.ts'
export default function LibraryEditor({
  editor,
  catalog,
  disabled,
  onCancel,
  onSave,
  onDone,
}: {
  editor: Editor
  catalog: Catalog
  disabled: boolean
  onCancel: () => void
  onSave: (path: string, payload: unknown) => Promise<boolean>
  onDone: () => void
}) {
  const [name, setName] = useState(editor.name ?? '')
  const [description, setDescription] = useState(editor.description ?? '')
  const [parent, setParent] = useState(editor.parent ?? '')
  const [position, setPosition] = useState(String(editor.position ?? 0))
  const [color, setColor] = useState(editor.color ?? '#BDCEC9')
  const [retention, setRetention] = useState(
    String(catalog.settings?.retention_days ?? ''),
  )
  const [organization, setOrganization] = useState(emptyOrganization)
  const [mode, setMode] = useState('add')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [composing, setComposing] = useState(false)
  const ref = useFocusTrap<HTMLDivElement>(true, onCancel, !saving)
  const title =
    editor.kind === 'move'
      ? '移动分组'
      : editor.kind === 'metadata'
        ? '编辑资产信息'
        : editor.kind === 'organize'
          ? '批量整理'
          : editor.kind === 'settings'
            ? '回收站保留设置'
            : `${editor.id ? '编辑' : '新建'}${editor.kind === 'group' ? '分组' : editor.kind === 'tag' ? '标签' : '分类'}`
  async function save() {
    if (saving || disabled || composing) return
    if (
      ['group', 'tag', 'category', 'metadata'].includes(editor.kind) &&
      !name.trim()
    ) {
      setError('请填写名称')
      return
    }
    let path = '',
      payload: unknown
    const version = {
      expected_revision: editor.revision,
      hierarchy_revision: editor.hierarchy,
    }
    switch (editor.kind) {
      case 'group':
        path = editor.id ? `/asset-groups/${editor.id}/rename` : '/asset-groups'
        payload = editor.id
          ? { ...version, name }
          : {
              name,
              parent_id: parent || null,
              hierarchy_revision: editor.hierarchy,
            }
        break
      case 'move':
        if (!Number.isInteger(Number(position)) || Number(position) < 0) {
          setError('顺序必须为零或正整数')
          return
        }
        path = `/asset-groups/${editor.id}/move`
        payload = {
          ...version,
          parent_id: parent || null,
          position: Number(position),
        }
        break
      case 'tag':
        path = editor.id ? `/tags/${editor.id}/edit` : '/tags'
        payload = { ...version, name, color, category_id: parent || null }
        break
      case 'category':
        path = editor.id
          ? `/tag-categories/${editor.id}/edit`
          : '/tag-categories'
        payload = { ...version, name }
        break
      case 'metadata':
        path = `/assets/${editor.id}/metadata`
        payload = {
          expected_revision: editor.revision,
          title: name,
          description,
        }
        break
      case 'organize':
        path = '/assets/batch-organize'
        payload = {
          assets: editor.selected?.map((a) => ({
            asset_id: a.id,
            expected_revision: a.revision,
          })),
          [`${mode}_group_ids`]: organization.groupIDs,
          [`${mode}_tag_ids`]: organization.tagIDs,
        }
        break
      case 'settings':
        path = '/library-settings'
        payload = {
          expected_revision: editor.revision,
          retention_days: retention ? Number(retention) : null,
        }
        break
    }
    setSaving(true)
    setError('')
    try {
      if (await onSave(path, payload)) onDone()
      else setError('保存未完成。可关闭此窗口，在顶部处理错误或恢复原操作。')
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setSaving(false)
    }
  }
  return (
    <div className="overlay open">
      <div
        className="dialog cl-editor"
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby="libraryEditorTitle"
        tabIndex={-1}
      >
        <h2 id="libraryEditorTitle">{title}</h2>
        <form
          noValidate
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
        >
          {['group', 'tag', 'category', 'metadata'].includes(editor.kind) && (
            <label>
              名称
              <input
                className="input"
                value={name}
                maxLength={editor.kind === 'metadata' ? 200 : 80}
                aria-invalid={!!error}
                onChange={(e) => setName(e.target.value)}
                disabled={saving}
              />
            </label>
          )}
          {editor.kind === 'metadata' && (
            <label>
              描述
              <textarea
                className="input resize-none"
                style={{ resize: 'none' }}
                value={description}
                maxLength={2000}
                onChange={(e) => setDescription(e.target.value)}
                disabled={saving}
              />
            </label>
          )}
          {(editor.kind === 'move' ||
            (editor.kind === 'group' && !editor.id)) && (
            <label>
              上级分组
              <select
                className="input"
                aria-label="上级分组"
                value={parent}
                onChange={(e) => setParent(e.target.value)}
              >
                <option value="">顶层</option>
                {catalog.groups
                  .filter((g) => g.id !== editor.id)
                  .map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
              </select>
            </label>
          )}
          {editor.kind === 'move' && (
            <label>
              同级顺序（从 0 开始）
              <input
                className="input"
                type="number"
                min={0}
                step={1}
                value={position}
                onChange={(e) => setPosition(e.target.value)}
              />
            </label>
          )}
          {editor.kind === 'tag' && (
            <>
              <label>
                颜色
                <input
                  type="color"
                  value={color}
                  onChange={(e) => setColor(e.target.value)}
                />
              </label>
              <label>
                标签分类
                <select
                  aria-label="标签分类"
                  className="input"
                  value={parent}
                  onChange={(e) => setParent(e.target.value)}
                >
                  <option value="">未分类</option>
                  {catalog.categories.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </label>
            </>
          )}
          {editor.kind === 'organize' && (
            <>
              <label>
                整理方式
                <select
                  className="input"
                  value={mode}
                  onChange={(e) => setMode(e.target.value)}
                >
                  <option value="add">添加到所选分组 / 标签</option>
                  <option value="remove">移出所选分组 / 标签</option>
                </select>
              </label>
              <OrganizationPicker
                groups={catalog.groups}
                tags={catalog.tags}
                categories={catalog.categories}
                value={organization}
                onChange={setOrganization}
                disabled={saving}
                allowNew={false}
              />
            </>
          )}
          {editor.kind === 'settings' && (
            <>
              <label>
                默认保留时长
                <select
                  className="input"
                  value={retention}
                  onChange={(e) => setRetention(e.target.value)}
                >
                  <option value="">一直保留</option>
                  <option value="7">7 天</option>
                  <option value="30">30 天</option>
                  <option value="90">90 天</option>
                </select>
              </label>
              <p>
                仅影响之后移入回收站的资产。自动到期清理尚未启用，目前需手动彻底删除。
              </p>
            </>
          )}
          {error && (
            <p className="cl-error" role="alert">
              {error}
            </p>
          )}
          <div className="dialog-actions">
            <button
              className="btn"
              type="button"
              disabled={saving}
              onClick={onCancel}
            >
              取消
            </button>
            <button
              className="btn btn-primary"
              type="submit"
              disabled={disabled || saving || composing}
            >
              保存
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
