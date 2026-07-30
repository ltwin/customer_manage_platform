import clsx from 'clsx'
import type { LucideIcon } from 'lucide-react'

export type EmptyStateProps = {
  icon: LucideIcon
  title: string
  hint?: string
  inline?: boolean
}

export default function EmptyState({ icon: Icon, title, hint, inline }: EmptyStateProps) {
  return (
    <div className={clsx('empty', inline && 'inline-empty')}>
      <Icon aria-hidden="true" strokeWidth={1.5} />
      <div className="empty-title">{title}</div>
      {hint && <div className="empty-hint">{hint}</div>}
    </div>
  )
}
