import { useEffect, useRef, useState } from 'react'
import { FileText, Link, Plus, Upload } from 'lucide-react'
import type { components } from '../../api/schema'
import ImportDialog from './ImportDialog.tsx'
import CanvasContextMenu from '../editor/CanvasContextMenu.tsx'
import UploadTray from '../editor/UploadTray.tsx'
import { useUploads } from '../editor/uploads.ts'
import { fileTitle, importPlan } from '../editor/fileImport.ts'
import { readableFileName } from '../editor/media.ts'
import type { useActions } from './useActions.ts'
import { errorMessage } from '../editor/queue.ts'
import { operation } from './operations.ts'
import { contentImport, pastedKind } from './importContent.ts'
import type { Point } from './popover.ts'

export function useAssetImport({
  account,
  group,
  blocked,
  actions,
  onDone,
  onStart,
  paste = true,
}: {
  account: string
  group: string
  blocked: boolean
  actions: ReturnType<typeof useActions>
  onStart: () => void
  onDone: () => void
  /** 画布页的 ⌘V 归画布，抽屉里不接管粘贴。 */
  paste?: boolean
}) {
  const input = useRef<HTMLInputElement>(null)
  const [menu, setMenu] = useState<Point | null>(null)
  const [draft, setDraft] = useState<{
    kind: 'text' | 'link'
    group: string
    value?: string
    error?: string
    title?: string
  } | null>(null)
  const uploads = useUploads((item) => {
    if (item.stage === 'done') onDone()
    else actions.setToast({ text: `${item.name}：${item.message}` })
  })
  const busy = uploads.busy
  useEffect(() => {
    if (!busy) return
    const leave = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', leave)
    return () => window.removeEventListener('beforeunload', leave)
  }, [busy])
  function files(list: File[]) {
    if (
      blocked ||
      actions.isBlocked() ||
      document.querySelector('[role="dialog"], [role="alertdialog"]')
    )
      return
    if (!uploads.capabilities || uploads.capabilitiesError) {
      actions.setToast({
        text: uploads.capabilitiesError || '正在读取上传能力，请稍后重试',
      })
      return
    }
    const plan = importPlan(list, uploads.capabilities)
    uploads.reject(plan.rejected)
    if (plan.rejected.length)
      actions.setToast({
        text: plan.rejected
          .map(
            ({ file, message }) => `${readableFileName(file.name)}：${message}`,
          )
          .join('；'),
      })
    if (plan.accepted.length) onStart()
    uploads.start(
      plan.accepted.map((x) => x.file),
      (file) => ({
        kind: 'asset',
        asset: { title: fileTitle(file.name), group_ids: group ? [group] : [] },
      }),
    )
  }
  function create(
    kind: 'link' | 'text',
    value: string,
    title = '',
    destination = group,
  ) {
    const payload = contentImport(kind, value, title, destination)
    onStart()
    const send = operation<components['schemas']['CreativeAssetCreateResult']>(
      account,
      '/assets',
      payload,
    )
    void actions.execute({
      run: async () => {
        setDraft((current) =>
          current ? { ...current, error: undefined } : current,
        )
        try {
          return await send()
        } catch (e) {
          setDraft({
            kind,
            group: destination,
            value,
            title,
            error: errorMessage(e),
          })
          throw e
        }
      },
      success: () => {
        setDraft(null)
        onDone()
        return { text: kind === 'link' ? '已添加链接' : '已新建文字' }
      },
    })
  }
  const pasteRef = useRef<(e: ClipboardEvent) => void>(() => {})
  pasteRef.current = (e) => {
    const target = e.target instanceof Element ? e.target : null
    if (
      blocked ||
      draft ||
      menu ||
      target?.closest(
        'input,textarea,select,[contenteditable="true"],[role="dialog"],[role="alertdialog"],[role="menu"]',
      ) ||
      document.querySelector('[role="dialog"], [role="alertdialog"]')
    )
      return
    const incoming = Array.from(e.clipboardData?.files ?? [])
    const text = e.clipboardData?.getData('text/plain') ?? ''
    if (incoming.length) {
      e.preventDefault()
      files(incoming)
    } else if (text.trim()) {
      e.preventDefault()
      const kind = pastedKind(text)
      try {
        create(kind, text)
      } catch (e) {
        actions.setToast(null)
        setDraft({
          kind,
          group,
          value: text,
          error: e instanceof Error ? e.message : '无法添加素材',
        })
      }
    }
  }
  useEffect(() => {
    if (!paste) return
    const listener = (e: ClipboardEvent) => pasteRef.current(e)
    document.addEventListener('paste', listener)
    return () => document.removeEventListener('paste', listener)
  }, [paste])
  const button = (className = '') => (
    <button
      type="button"
      className={`btn btn-primary al-import ${className}`}
      disabled={blocked}
      aria-haspopup="menu"
      aria-expanded={!!menu}
      onClick={(e) => {
        const r = e.currentTarget.getBoundingClientRect()
        setMenu({ x: r.left, y: r.bottom + 4 })
      }}
    >
      <Plus size={14} />
      导入
    </button>
  )
  return {
    files,
    busy,
    button,
    open: !!draft || !!menu,
    overlays: (
      <>
        <input
          ref={input}
          type="file"
          multiple
          hidden
          aria-label="上传素材文件"
          onChange={(e) => {
            files(Array.from(e.currentTarget.files ?? []))
            e.currentTarget.value = ''
          }}
        />
        {menu && (
          <CanvasContextMenu
            {...menu}
            label="导入素材"
            onClose={() => setMenu(null)}
            items={[
              {
                label: '上传文件',
                icon: <Upload size={14} />,
                shortcut: '或直接拖进来',
                action: () => input.current?.click(),
              },
              {
                label: '添加链接',
                icon: <Link size={14} />,
                shortcut: '⌘V 粘贴网址',
                action: () => {
                  actions.setToast(null)
                  setDraft({ kind: 'link', group })
                },
              },
              {
                label: '新建文字',
                icon: <FileText size={14} />,
                action: () => {
                  actions.setToast(null)
                  setDraft({ kind: 'text', group })
                },
              },
            ]}
          />
        )}
        {draft && (
          <ImportDialog
            kind={draft.kind}
            busy={actions.busy}
            blocked={actions.blocked}
            initialValue={draft.value ?? ''}
            initialTitle={draft.title ?? ''}
            errorMessage={draft.error ?? ''}
            onRetry={
              draft.error && actions.blocked
                ? actions.toast?.action?.run
                : undefined
            }
            onClose={() => {
              if (!actions.isBlocked()) setDraft(null)
            }}
            onSave={(value, title) =>
              create(draft.kind, value, title, draft.group)
            }
          />
        )}
        <UploadTray
          items={uploads.items}
          onRetry={uploads.retry}
          onDismiss={uploads.dismiss}
        />
      </>
    ),
  }
}
