import { useEffect, useRef, useState } from 'react'
import { FileText, Link, Plus, Upload } from 'lucide-react'
import type { components } from '../../api/schema'
import ImportDialog from './ImportDialog.tsx'
import DuplicateDialog, { type DuplicateChoice } from './DuplicateDialog.tsx'
import CanvasContextMenu from '../editor/CanvasContextMenu.tsx'
import UploadTray from '../editor/UploadTray.tsx'
import { useUploads } from '../editor/uploads.ts'
import { fileTitle, importPlan } from '../editor/fileImport.ts'
import { readableFileName } from '../editor/media.ts'
import type { useActions } from './useActions.ts'
import { errorMessage } from '../editor/queue.ts'
import { conflict, operation } from './operations.ts'
import { contentImport, pastedKind } from './importContent.ts'
import { findDuplicates, type Duplicate } from './importDuplicates.ts'
import { read } from '../editor/api.ts'
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
  // 内容与资产库重复、等摄影师决定的文件；group 是发起导入时的位置。
  const [pending, setPending] = useState<{
    items: Duplicate[]
    group: string
    error: string
    seq: number
  } | null>(null)
  const [replacing, setReplacing] = useState(false)
  const checking = useRef(false)
  // 失败的替换按原操作身份重试：结果未知的写入不能换新身份重发。
  const failed = useRef<{
    rest: boolean
    run: () => Promise<void>
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
      checking.current ||
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
    const accepted = plan.accepted.map((x) => x.file)
    if (!accepted.length) return
    onStart()
    const destination = group
    checking.current = true
    void findDuplicates(accepted, (digests) =>
      read<components['schemas']['CreativeAssetDigestMatches']>(
        `/assets/digest-matches?digests=${digests.join(',')}`,
      ).then((r) => r.items),
    )
      // 查重只是提醒，失败时照常导入。
      .catch(() => [])
      .then((found) => {
        checking.current = false
        const held = new Set(found.map((d) => d.file))
        upload(
          accepted.filter((file) => !held.has(file)),
          destination,
        )
        if (found.length)
          setPending({ items: found, group: destination, error: '', seq: 0 })
      })
  }
  function upload(list: File[], destination: string) {
    if (!list.length) return
    uploads.start(list, (file) => ({
      kind: 'asset',
      asset: {
        title: fileTitle(file.name),
        group_ids: destination ? [destination] : [],
      },
    }))
  }
  function choose(choice: DuplicateChoice, rest: boolean) {
    if (!pending || replacing) return
    const take = rest ? pending.items : pending.items.slice(0, 1)
    const next = (left = rest ? [] : pending.items.slice(1)) => {
      failed.current = null
      setPending(
        left.length
          ? { ...pending, items: left, error: '', seq: pending.seq + 1 }
          : null,
      )
    }
    if (choice === 'skip') return next()
    if (choice === 'add') {
      upload(
        take.map((d) => d.file),
        pending.group,
      )
      return next()
    }
    let run = failed.current?.rest === rest ? failed.current.run : null
    // 同一批里内容相同的几个文件指向同一份素材，只替换一次。
    const targets = take.filter(
      (d, i) => take.findIndex((x) => x.asset.id === d.asset.id) === i,
    )
    const replaced = new Set(targets.map((d) => d.asset.id))
    if (!run) {
      const sends = targets.map((d) =>
        operation(
          account,
          `/assets/${encodeURIComponent(d.asset.id)}/import-replace`,
          {
            expected_revision: d.asset.revision,
            title: fileTitle(d.file.name),
            group_ids: pending.group ? [pending.group] : [],
          } satisfies components['schemas']['CreativeAssetImportReplacePayload'],
        ),
      )
      run = async () => {
        for (const send of sends) await send()
      }
    }
    const attempt = run
    setReplacing(true)
    void attempt()
      .then(() => {
        // 排在后面、内容相同的文件已经并进这份素材，不再逐个询问；
        // 它们手里的版本也已过期，再替换只会冲突。
        const left = rest ? [] : pending.items.slice(1)
        const remaining = left.filter((d) => !replaced.has(d.asset.id))
        const merged = left.length - remaining.length
        next(remaining)
        onDone()
        actions.setToast({
          text:
            (targets.length === 1
              ? `已替换「${targets[0].asset.title}」`
              : `已替换 ${targets.length} 个素材`) +
            (merged ? `，同批 ${merged} 个相同文件一并处理` : ''),
        })
      })
      .catch(async (e: unknown) => {
        if (!conflict(e)) {
          failed.current = { rest, run: attempt }
          setPending((p) => p && { ...p, error: errorMessage(e) })
          return
        }
        // 版本冲突说明这次写入被拒绝了：读回最新版本，下次替换用新请求。
        failed.current = null
        const fresh = await Promise.allSettled(
          targets.map((d) =>
            read<components['schemas']['CreativeLibraryAsset']>(
              `/assets/${encodeURIComponent(d.asset.id)}`,
            ),
          ),
        )
        const latest = new Map(
          fresh.flatMap((r) =>
            r.status === 'fulfilled' ? [[r.value.id, r.value] as const] : [],
          ),
        )
        setPending(
          (p) =>
            p && {
              ...p,
              items: p.items.map((d) => ({
                ...d,
                asset: latest.get(d.asset.id) ?? d.asset,
              })),
              error: '这份素材刚在别处改过，已读取最新内容，可以再替换一次',
            },
        )
      })
      .finally(() => setReplacing(false))
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
    open: !!draft || !!menu || !!pending,
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
        {pending && (
          <DuplicateDialog
            key={pending.seq}
            item={pending.items[0]}
            remaining={pending.items.length - 1}
            grouped={!!pending.group}
            busy={replacing}
            error={pending.error}
            onChoose={choose}
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
