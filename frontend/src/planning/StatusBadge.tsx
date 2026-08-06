import type { ShootPlanStatus } from './api'
import { shootPlanStatusLabel } from './presentation'

export default function StatusBadge({ status }: { status: ShootPlanStatus }) {
  const tone = status === 'ready'
    ? 'badge-success'
    : status === 'in_progress'
      ? 'badge-accent'
      : status === 'draft'
        ? 'badge-warning'
        : 'badge-muted'
  return <span className={`badge ${tone}`}>{shootPlanStatusLabel(status)}</span>
}
