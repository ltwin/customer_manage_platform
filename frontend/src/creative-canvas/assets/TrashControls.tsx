import { useEffect, useRef, useState } from 'react'
import { Clock } from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from '../editor/api.ts'
import ConfirmDialog from '../../components/ConfirmDialog'
import { errorMessage } from '../editor/queue.ts'
import { conflict, operation, rejected } from './operations.ts'
import type { useActions } from './useActions.ts'

type Settings = components['schemas']['CreativeLibrarySettings']
export default function TrashControls({
  account,
  refresh,
  actions,
  onDone,
}: {
  account: string
  refresh: number
  actions: ReturnType<typeof useActions>
  onDone: () => void
}) {
  const [settings, setSettings] = useState<Settings | null>(null),
    [error, setError] = useState(''),
    [retry, setRetry] = useState(0),
    [empty, setEmpty] = useState(false)
  useEffect(() => {
    const c = new AbortController()
    void api
      .read<Settings>('/library-settings', c.signal)
      .then((s) => {
        if (!c.signal.aborted) {
          setSettings(s)
          setError('')
        }
      })
      .catch((e: unknown) => {
        if (!c.signal.aborted) setError(errorMessage(e))
      })
    return () => c.abort()
  }, [refresh, retry])
  return (
    <>
      <div className="al-trash-note">
        <Clock size={14} />
        <span>
          {error
            ? '保留设置读取失败'
            : !settings
              ? '正在读取保留设置…'
              : settings.retention_days === null
                ? '不会自动清除'
                : `移入回收站 ${settings.retention_days} 天后自动清除`}
        </span>
        {error && (
          <button className="btn" onClick={() => setRetry((n) => n + 1)}>
            重试
          </button>
        )}
        {settings && (
          <label className="al-retention">
            保留期限
            <select
              aria-label="回收站保留期限"
              value={settings.retention_days ?? ''}
              disabled={actions.blocked}
              onChange={(e) => {
                const value = e.currentTarget.value
                void actions.execute({
                  run: operation(account, '/library-settings', {
                    expected_revision: settings.revision,
                    retention_days:
                      value === '7'
                        ? 7
                        : value === '30'
                          ? 30
                          : value === '90'
                            ? 90
                            : null,
                  } satisfies components['schemas']['CreativeLibrarySettingsPayload']),
                  success: () => ({
                    text: '已更新保留期限，已在回收站中的素材仍按原期限清除',
                  }),
                })
              }}
            >
              <option value="7">7 天</option>
              <option value="30">30 天</option>
              <option value="90">90 天</option>
              <option value="">不自动清除</option>
            </select>
          </label>
        )}
        <button
          className="btn"
          disabled={actions.blocked}
          onClick={() => setEmpty(true)}
        >
          清空回收站
        </button>
      </div>
      <p className="al-trash-help">
        已有素材按各自剩余时间清除；到期后由后台定期处理。
      </p>
      {empty && (
        <EmptyTrash
          account={account}
          onClose={() => setEmpty(false)}
          onDone={() => {
            setEmpty(false)
            onDone()
          }}
        />
      )}
    </>
  )
}
function EmptyTrash({
  account,
  onClose,
  onDone,
}: {
  account: string
  onClose: () => void
  onDone: () => void
}) {
  const [snapshot, setSnapshot] = useState<api.AssetPage | null>(null),
    [busy, setBusy] = useState(true),
    [unknown, setUnknown] = useState(false),
    [error, setError] = useState('')
  const send = useRef<
    (() => Promise<components['schemas']['CreativeEmptyTrashResult']>) | null
  >(null)
  const running = useRef(false)
  async function read(changed = false) {
    setBusy(true)
    setSnapshot(null)
    try {
      const page = await api.read<api.AssetPage>('/assets?view=trash&limit=1')
      setSnapshot(page)
      setError(changed ? '回收站有变化，请再确认一次' : '')
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => {
    void read()
  }, [])
  useEffect(() => {
    if (!busy && !unknown) return
    const leave = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', leave)
    return () => window.removeEventListener('beforeunload', leave)
  }, [busy, unknown])
  async function confirm() {
    if (running.current) return
    if (!snapshot) {
      void read()
      return
    }
    running.current = true
    setBusy(true)
    send.current ??= operation(account, '/library/empty-trash', {
      expected_library_revision: snapshot.library_revision,
    } satisfies components['schemas']['CreativeEmptyTrashPayload'])
    try {
      await send.current()
      setUnknown(false)
      onDone()
    } catch (e) {
      if (conflict(e)) {
        send.current = null
        setUnknown(false)
        await read(true)
      } else {
        const uncertain = !rejected(e)
        setUnknown(uncertain)
        if (!uncertain) send.current = null
        setError(
          errorMessage(e) + (uncertain ? '；结果尚未确认，请重试原操作。' : ''),
        )
      }
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  return (
    <ConfirmDialog
      title="清空回收站？"
      body={[
        <span key="count">
          {snapshot
            ? `将永久删除 ${snapshot.total_count} 项。`
            : '正在核对回收站数量。'}
          画布里已有的节点不受影响，此操作不可撤销。
        </span>,
        ...(error
          ? [
              <span role="alert" key="error">
                {error}
              </span>,
            ]
          : []),
      ]}
      danger
      busy={busy}
      confirmLabel={unknown ? '重试原操作' : snapshot ? '永久删除' : '重新读取'}
      cancelLabel={unknown ? '等待确认' : '取消'}
      onCancel={() => {
        if (!unknown && !busy) onClose()
      }}
      onConfirm={() => void confirm()}
    />
  )
}
