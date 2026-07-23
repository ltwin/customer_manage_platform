import type { PageReadNotice } from './pageReadState'

export type StateNoticeProps = PageReadNotice

export default function StateNotice(props: StateNoticeProps) {
  if (props.kind === 'loading') {
    return <div className="state-notice state-notice-loading" role="status" aria-busy="true">{props.message}</div>
  }

  if (props.kind === 'empty') {
    return <div className="state-notice state-notice-empty">{props.message}</div>
  }

  return (
    <div
      className={`state-notice state-notice-${props.kind}`}
      role="alert"
    >
      <span>{props.message}</span>
      {(props.kind === 'refresh-error' || props.retryable) && (
        <button className="btn btn-sm" type="button" onClick={props.onRetry}>重试</button>
      )}
    </div>
  )
}
