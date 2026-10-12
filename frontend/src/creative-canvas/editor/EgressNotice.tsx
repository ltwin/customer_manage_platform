// 首次外发说明弹窗（V6-029）：画布助手和首页一句话开始共用。
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import ConfirmDialog from '../../components/ConfirmDialog'
import { noticeSeen, rememberNotice } from './agentState.ts'

const lines = [
  <>
    这条消息会发送给平台签约的模型服务商，用来生成助手的回复。详见
    {/* 新标签页打开，草稿和弹窗都还在。 */}
    <a href="/privacy" target="_blank" rel="noreferrer">
      隐私说明
    </a>
    。
  </>,
  '会发送：你写的文字和选择的 Skill；你附带或 @ 引用的图片（压缩后的预览图）；助手完成任务时读取的当前画布内容，包括节点标题、文字、生成提示词和参数。',
  '资产库里没有被引用的素材不会发送。每次只带这条消息涉及的内容；说明有变化时会再告诉你。',
]

// 有的浏览器环境一碰 localStorage 就抛错，当作没有本机存储。
function local(): Storage | null {
  try {
    return localStorage
  } catch {
    return null
  }
}

// confirm() 在看过当前版本的说明时直接放行；否则弹窗，等摄影师选择。
// 取消不写记录，草稿由调用方原样保留，下次发送还会弹。
export function useEgressNotice(account: string) {
  const [open, setOpen] = useState(false)
  const waiting = useRef<((ok: boolean) => void) | null>(null)
  const settle = useCallback((ok: boolean) => {
    const resolve = waiting.current
    waiting.current = null
    setOpen(false)
    resolve?.(ok)
  }, [])
  useEffect(
    () => () => {
      waiting.current?.(false)
      waiting.current = null
    },
    [],
  )
  const confirm = useCallback((): Promise<boolean> => {
    if (noticeSeen(local(), account)) return Promise.resolve(true)
    waiting.current?.(false)
    return new Promise((resolve) => {
      waiting.current = resolve
      setOpen(true)
    })
  }, [account])
  const dialog = open
    ? createPortal(
        <ConfirmDialog
          title="发送前说明"
          body={lines}
          confirmLabel="知道了，发送"
          onConfirm={() => {
            rememberNotice(local(), account)
            settle(true)
          }}
          onCancel={() => settle(false)}
        />,
        document.querySelector('.cc-workspace') ?? document.body,
      )
    : null
  return { confirm, dialog }
}
