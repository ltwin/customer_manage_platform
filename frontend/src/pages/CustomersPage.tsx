import { Link, useNavigate } from 'react-router-dom'
import { useEffect, useMemo, useState } from 'react'
import { ApiError, listCustomers } from '../api/client'
import type { CustomerListResponse } from '../api/client'
import { channelLabels, channelOptions } from './customerLabels'
import type { CustomerChannel } from './customerLabels'

type CustomerListItem = CustomerListResponse['items'][number]

export default function CustomersPage() {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [channel, setChannel] = useState<'all' | CustomerChannel>('all')
  const [items, setItems] = useState<CustomerListItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const params = useMemo(() => ({
    q: query.trim(),
    channel: channel === 'all' ? '' : channel,
  }), [channel, query])

  useEffect(() => {
    let active = true
    setLoading(true)
    setError(null)
    listCustomers({ q: params.q, channel: params.channel, page: 1, pageSize: 20 })
      .then((result) => {
        if (!active) return
        setItems(result.items)
        setTotal(result.total)
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        setError(err instanceof Error ? err.message : '客户列表加载失败')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [navigate, params.channel, params.q])

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
            <button
              className={`chip${channel === 'all' ? ' active' : ''}`}
              type="button"
              onClick={() => setChannel('all')}
            >
              全部
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

        {error && <div className="form-error">{error}</div>}
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>客户</th>
                <th>来源</th>
                <th>约单</th>
                <th>最近拍摄</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr><td colSpan={5}><div className="empty inline-empty">加载中</div></td></tr>
              ) : items.length === 0 ? (
                <tr><td colSpan={5}><div className="empty inline-empty">暂无客户</div></td></tr>
              ) : items.map((customer) => (
                <tr key={customer.id} onClick={() => navigate(`/customers/${customer.id}`)}>
                  <td>
                    <div className="cell-name">
                      <div className="avatar">{customer.display_name[0]}</div>
                      <div>
                        <div className="nm">{customer.display_name}</div>
                        <div className="rn num">{shortDate(customer.created_at)}</div>
                      </div>
                    </div>
                  </td>
                  <td><span className="badge badge-muted">{channelLabels[customer.channel]}</span></td>
                  <td><span className="num">{customer.orders_count}</span> 单</td>
                  <td className="num muted-text">{customer.last_shot_at ?? '暂无'}</td>
                  <td>{customer.status === 'active' ? <span className="badge badge-success">活跃</span> : <span className="badge badge-muted">{customer.status}</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="result-meta">显示 {items.length} / {total} 位客户</div>
      </main>
    </>
  )
}

function shortDate(value: string) {
  return value.slice(0, 10)
}
