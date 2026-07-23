import { Link, useNavigate } from 'react-router-dom'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, listCustomers } from '../api/client'
import type { CustomerListStatus } from '../api/client'
import { channelLabels, channelOptions } from './customerLabels'
import type { CustomerChannel } from './customerLabels'
import QuickNote from '../components/customers/QuickNote'
import CustomerAvatar from '../components/customers/CustomerAvatar'
import CustomerResult from '../components/customers/CustomerResult'
import {
  customerStatusBadge,
  shortCustomerDate,
  type CustomerListItem,
} from '../components/customers/customerResultModel'
import StateNotice from '../components/StateNotice'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from '../components/pageReadState'

const statusFilters: Array<[CustomerListStatus & string, string]> = [
  ['active', '经营中'],
  ['archived', '已归档'],
  ['all', '全部'],
]

export default function CustomersPage() {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [channel, setChannel] = useState<'all' | CustomerChannel>('all')
  const [status, setStatus] = useState<CustomerListStatus & string>('active')
  const [readState, setReadState] = useState<PageReadState<{
    items: CustomerListItem[]
    total: number
  }>>({ kind: 'loading', message: '正在加载客户' })
  const [reloadTick, setReloadTick] = useState(0)
  const loadedParamsRef = useRef('')

  const params = useMemo(() => ({
    q: query.trim(),
    channel: channel === 'all' ? '' : channel,
    status,
  }), [channel, query, status])

  const goLogin = useCallback(() => {
    navigate('/login', { replace: true })
  }, [navigate])

  useEffect(() => {
    let active = true
    const paramsKey = JSON.stringify(params)
    const preserveReady = loadedParamsRef.current === paramsKey
    loadedParamsRef.current = paramsKey
    setReadState((current) => beginPageRead(current, '正在加载客户', preserveReady))
    listCustomers({ q: params.q, channel: params.channel, status: params.status, page: 1, pageSize: 20 })
      .then((result) => {
        if (!active) return
        const filterContext = params.q ? `搜索“${params.q}”` : '当前筛选'
        setReadState(completePageRead(
          { items: result.items, total: result.total },
          result.items.length === 0,
          `${filterContext}下暂无客户`,
        ))
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          setReadState({ kind: 'unauthorized' })
          goLogin()
          return
        }
        setReadState((current) => failPageRead(
          current,
          err instanceof Error ? err.message : '客户列表加载失败',
          () => setReloadTick((tick) => tick + 1),
        ))
      })
    return () => {
      active = false
    }
  }, [goLogin, params, reloadTick])

  const presentation = pageReadPresentation(readState)
  const data = readyPageData(readState)
  const items = data?.items ?? []
  const total = data?.total ?? 0

  return (
    <>
      <header className="topbar">
        <div>
          <h1>客户</h1>
          <div className="sub">共 <span className="num">{total}</span> 位 · 私域渠道统一管理</div>
        </div>
        <div className="topbar-actions">
          <Link className="btn btn-primary" to="/customers/new">＋ 30 秒建档</Link>
        </div>
      </header>

      <main className="content">
        <div className="toolbar">
          <label className="search-box">
            <span className="search-icon" aria-hidden="true">⌕</span>
            <input
              className="input"
              type="search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="搜索昵称 / 手机号 / 平台账号…"
            />
          </label>
          <div className="chips">
            {statusFilters.map(([key, label]) => (
              <button
                key={key}
                className={`chip${status === key ? ' active' : ''}`}
                type="button"
                onClick={() => setStatus(key)}
              >
                {label}
              </button>
            ))}
          </div>
          <div className="chips">
            <button
              className={`chip${channel === 'all' ? ' active' : ''}`}
              type="button"
              onClick={() => setChannel('all')}
            >
              全部渠道
            </button>
            {channelOptions.map(([key, label]) => (
              <button
                key={key}
                className={`chip${channel === key ? ' active' : ''}`}
                type="button"
                onClick={() => setChannel(key)}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        {presentation.notice && <StateNotice {...presentation.notice} />}
        {presentation.showReadyData && <div className="table-wrap customers-desktop-results">
          <table className="data">
            <thead>
              <tr>
                <th>客户</th>
                <th>来源</th>
                <th>约单</th>
                <th>最近拍摄</th>
                <th>状态</th>
                <th>快捷操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((customer) => {
                const [badgeClass, badgeLabel] = customerStatusBadge(customer.status)
                return (
                  <tr key={customer.id} onClick={() => navigate(`/customers/${customer.id}`)}>
                    <td>
                      <div className="cell-name">
                        <CustomerAvatar
							customerId={customer.id ?? ''}
							displayName={customer.display_name}
							avatarRevision={customer.avatar_revision}
							avatarUrl={customer.avatar_url}
							decorative
						/>
                        <div>
                          <div className="nm">{customer.display_name}</div>
                          <div className="rn num">{shortCustomerDate(customer.created_at)}</div>
                        </div>
                      </div>
                    </td>
                    <td><span className="badge badge-muted">{channelLabels[customer.channel]}</span></td>
                    <td><span className="num">{customer.orders_count}</span> 单</td>
                    <td className="num muted-text">{customer.last_shot_at ?? '暂无'}</td>
                    <td><span className={badgeClass}>{badgeLabel}</span></td>
                    <td>
                      {customer.status !== 'merged' && (
                        <QuickNote
                          customerId={customer.id ?? ''}
                          onSaved={() => setReloadTick((tick) => tick + 1)}
                          onUnauthorized={goLogin}
                        />
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>}
        {presentation.showReadyData && (
          <div className="customer-mobile-results">
            {items.map((customer) => (
              <CustomerResult
                key={customer.id}
                customer={customer}
                onOpen={() => navigate(`/customers/${customer.id}`)}
                onQuickNoteSaved={() => setReloadTick((tick) => tick + 1)}
                onUnauthorized={goLogin}
              />
            ))}
          </div>
        )}
        {presentation.showReadyData && <div className="result-meta">显示 {items.length} / {total} 位客户</div>}
      </main>
    </>
  )
}
