import { channelLabels } from '../../pages/customerLabels'
import CustomerAvatar from './CustomerAvatar'
import QuickNote from './QuickNote'
import {
  customerStatusBadge,
  shortCustomerDate,
  type CustomerListItem,
} from './customerResultModel'

type Props = {
  customer: CustomerListItem
  onOpen: () => void
  onQuickNoteSaved: () => void
  onUnauthorized: () => void
}

export default function CustomerResult({
  customer,
  onOpen,
  onQuickNoteSaved,
  onUnauthorized,
}: Props) {
  const [badgeClass, badgeLabel] = customerStatusBadge(customer.status)

  return (
    <article className="customer-result-card">
      <button
        className="customer-result-open"
        type="button"
        onClick={onOpen}
        aria-label={`打开客户 ${customer.display_name} 的详情`}
      >
        <CustomerAvatar
          customerId={customer.id ?? ''}
          displayName={customer.display_name}
          avatarRevision={customer.avatar_revision}
          avatarUrl={customer.avatar_url}
          decorative
        />
        <span className="customer-result-identity">
          <strong>{customer.display_name}</strong>
          <span className="num">建档 {shortCustomerDate(customer.created_at)}</span>
        </span>
        <span aria-hidden="true">›</span>
      </button>

      <dl className="customer-result-facts">
        <div>
          <dt>来源</dt>
          <dd><span className="badge badge-muted">{channelLabels[customer.channel]}</span></dd>
        </div>
        <div>
          <dt>约单</dt>
          <dd><span className="num">{customer.orders_count}</span> 单</dd>
        </div>
        <div>
          <dt>最近拍摄</dt>
          <dd className="num">{customer.last_shot_at ?? '暂无'}</dd>
        </div>
        <div>
          <dt>状态</dt>
          <dd><span className={badgeClass}>{badgeLabel}</span></dd>
        </div>
      </dl>

      {customer.status !== 'merged' && (
        <div className="customer-result-action">
          <QuickNote
            customerId={customer.id ?? ''}
            onSaved={onQuickNoteSaved}
            onUnauthorized={onUnauthorized}
          />
        </div>
      )}
    </article>
  )
}
