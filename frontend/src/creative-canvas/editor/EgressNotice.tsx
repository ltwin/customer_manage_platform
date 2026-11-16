// 发送前说明与外发同意（V6-029，egress-consent-account-level）：画布助手和首页
// 一句话开始共用。同意按账号记在服务端，换对话、换浏览器都不再询问，直到说明
// 版本升级或摄影师在「外发说明」里撤回。
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import ConfirmDialog from '../../components/ConfirmDialog'
import { egressPolicyVersion } from './agentState.ts'
import {
  accountConsent,
  agreeToNotice,
  forgetConsent,
  isPolicyChanged,
  isStaleConsent,
  withdrawConsent,
  type EgressConsentState,
} from './egressConsent.ts'

const privacyLink = (
  <>
    详见
    {/* 新标签页打开，草稿和弹窗都还在。 */}
    <a href="/privacy" target="_blank" rel="noreferrer">
      隐私说明
    </a>
    。
  </>
)
const disclosure = [
  <>
    你发给助手的消息会发送给平台签约的模型服务商，用来生成助手的回复。
    {privacyLink}
  </>,
  '会发送：你写的文字和选择的 Skill；你附带或 @ 引用的图片（压缩后的预览图）；助手完成任务时读取的当前画布内容，包括节点标题、文字、生成提示词和参数；本次对话里之前出现过的内容。',
  '后台任务（例如图片生成）完成后，助手可能自动开始回复处理结果，按用量扣积分。',
  '同意后不再逐条询问；你可以随时在助手面板菜单的「外发说明」里撤回。已经发出的内容无法召回。说明有变化时会再请你确认。',
]
const libraryLabel =
  '允许助手检索我的素材库（素材标题、描述、标签、分组及它查看的图片预览）'

type Ask = {
  library: boolean
  // 摄影师看到的状态版本：同意以它为条件，别处先改过就不写。
  revision: string
  outdated: boolean
  error: string
  busy: boolean
}

function errorText(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

// confirm() 在账号的同意有效时直接放行；否则弹出说明，等摄影师选择。取消不写
// 记录，草稿由调用方原样保留，下次发送还会弹。keep 在「刷新页面」之前调用，
// 让调用方把还没落盘的草稿存好。
export function useEgressConsent(account: string) {
  const [ask, setAsk] = useState<Ask | null>(null)
  const waiting = useRef<((ok: boolean) => void) | null>(null)
  const keeping = useRef<(() => void) | null>(null)
  const settle = useCallback((ok: boolean) => {
    const resolve = waiting.current
    waiting.current = null
    keeping.current = null
    setAsk(null)
    resolve?.(ok)
  }, [])
  useEffect(
    () => () => {
      waiting.current?.(false)
      waiting.current = null
    },
    [],
  )
  const confirm = useCallback(
    async (keep?: () => void): Promise<boolean> => {
      let state: EgressConsentState | null = null
      try {
        state = await accountConsent(account)
      } catch {
        // 读不到就当作没同意：宁可多问一次，服务端照样会核对。
      }
      if (state?.effective) return true
      waiting.current?.(false)
      return new Promise<boolean>((resolve) => {
        waiting.current = resolve
        keeping.current = keep ?? null
        setAsk({
          library: state?.consent?.library_access ?? true,
          revision: state?.revision ?? '0',
          // 服务端的说明比这一页新：不能拿旧文字去换新版本的同意。
          outdated: !!state && state.policy_version !== egressPolicyVersion,
          error: '',
          busy: false,
        })
      })
    },
    [account],
  )
  const agree = useCallback(() => {
    if (!ask || ask.busy) return
    const { library } = ask
    setAsk({ ...ask, busy: true, error: '' })
    agreeToNotice(account, library, ask.revision).then(
      () => settle(true),
      async (error: unknown) => {
        if (isPolicyChanged(error)) {
          setAsk((old) => old && { ...old, busy: false, outdated: true })
          return
        }
        if (!isStaleConsent(error)) {
          setAsk(
            (old) =>
              old && {
                ...old,
                busy: false,
                error: errorText(error, '没有记下你的同意，请重试'),
              },
          )
          return
        }
        // 别处刚改过同意：读最新的。已经按同样的选择同意了就直接放行，否则
        // 以最新状态为条件，请摄影师再点一次。
        forgetConsent(account)
        let state: EgressConsentState | null = null
        try {
          state = await accountConsent(account)
        } catch {
          // 读不到时留在弹窗里，提示重试。
        }
        if (state?.effective && state.consent?.library_access === library) {
          settle(true)
          return
        }
        setAsk(
          (old) =>
            old && {
              ...old,
              busy: false,
              revision: state?.revision ?? old.revision,
              error: state
                ? '同意状态刚在其他窗口或设备上改过，请确认后再点一次'
                : '没有读到最新的同意状态，请重试',
            },
        )
      },
    )
  }, [account, ask, settle])
  const body = ask
    ? ask.outdated
      ? ['发送前说明已经更新，请刷新页面查看新版说明后再发送。你的草稿会保留。']
      : [
          ...disclosure,
          <label key="library" className="cc-egress-library">
            <input
              type="checkbox"
              checked={ask.library}
              disabled={ask.busy}
              onChange={(event) => {
                const library = event.target.checked
                setAsk((old) => old && { ...old, library })
              }}
            />
            <span>{libraryLabel}</span>
          </label>,
          ...(ask.error
            ? [
                <span key="error" className="cc-egress-error">
                  {ask.error}
                </span>,
              ]
            : []),
        ]
    : []
  const dialog = ask
    ? createPortal(
        <ConfirmDialog
          title="发送前说明"
          body={body}
          busy={ask.busy}
          confirmLabel={ask.outdated ? '刷新页面' : '同意并发送'}
          onConfirm={() => {
            if (!ask.outdated) {
              agree()
              return
            }
            // 刷新前先让调用方把草稿存好，刷新后从本机记录找回。
            keeping.current?.()
            window.location.reload()
          }}
          onCancel={() => settle(false)}
        />,
        document.querySelector('.cc-workspace') ?? document.body,
      )
    : null
  return { confirm, dialog }
}

function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

// 「外发说明」：查看已同意的版本和时间、修改素材库选择、撤回同意。
export function EgressSettings({
  account,
  onClose,
}: {
  account: string
  onClose: () => void
}) {
  const [state, setState] = useState<EgressConsentState | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  useEffect(() => {
    let alive = true
    // 面板打开时读一次最新的，不用缓存：别的设备可能刚改过。
    forgetConsent(account)
    accountConsent(account).then(
      (value) => {
        if (alive) setState(value)
      },
      (e: unknown) => {
        if (alive) setError(errorText(e, '读取失败'))
      },
    )
    return () => {
      alive = false
    }
  }, [account])
  function run(action: () => Promise<EgressConsentState>) {
    setBusy(true)
    setError('')
    action().then(
      (value) => {
        setState(value)
        setBusy(false)
      },
      async (e: unknown) => {
        if (!isStaleConsent(e)) {
          setError(errorText(e, '没有保存，请重试'))
          setBusy(false)
          return
        }
        // 面板打开后别处撤回或改过：显示最新状态，不凭旧界面写入。
        forgetConsent(account)
        try {
          setState(await accountConsent(account))
          setError('外发同意刚在其他窗口或设备上改过，已显示最新状态')
        } catch (reread: unknown) {
          setError(errorText(reread, '读取失败'))
        }
        setBusy(false)
      },
    )
  }
  const consent = state?.consent ?? null
  return (
    <div className="cc-egress-settings" role="dialog" aria-label="外发说明">
      <div className="cc-egress-settings-head">
        <strong>外发说明</strong>
        <button type="button" className="btn" onClick={onClose}>
          关闭
        </button>
      </div>
      {disclosure.map((line, index) => (
        <p key={index}>{line}</p>
      ))}
      {!state && !error && <p>正在读取…</p>}
      {state && consent && state.effective && (
        <>
          <p className="cc-egress-status">
            已同意（版本 {consent.policy_version}，
            {formatTime(consent.granted_at)}）
          </p>
          <label className="cc-egress-library">
            <input
              type="checkbox"
              checked={consent.library_access}
              disabled={busy}
              onChange={(event) => {
                const library = event.target.checked
                const seen = state.revision
                run(() => agreeToNotice(account, library, seen))
              }}
            />
            <span>{libraryLabel}</span>
          </label>
          <button
            type="button"
            className="btn btn-danger"
            disabled={busy}
            onClick={() => setConfirming(true)}
          >
            撤回同意
          </button>
        </>
      )}
      {state && !state.effective && (
        <p className="cc-egress-status">
          {state.revoked_at
            ? `已于 ${formatTime(state.revoked_at)} 撤回。`
            : '还没有同意当前版本的说明。'}
          下次发送时会再请你确认。
        </p>
      )}
      {error && <p className="cc-egress-error">{error}</p>}
      {confirming &&
        createPortal(
          <ConfirmDialog
            title="撤回外发同意"
            body={[
              '撤回后助手不能再开始新的回复，正在进行的回复会在下一次调用模型前停下。',
              '已经发出的内容无法召回。下次发送时会重新请你确认发送前说明。',
            ]}
            confirmLabel="撤回同意"
            danger
            onConfirm={() => {
              setConfirming(false)
              run(() => withdrawConsent(account))
            }}
            onCancel={() => setConfirming(false)}
          />,
          document.querySelector('.cc-workspace') ?? document.body,
        )}
    </div>
  )
}
