import { Link, useNavigate, useParams } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { ApiError, fetchCustomer } from '../api/client'
import type { CustomerDetail } from '../api/client'
import { channelLabels, platformLabels } from './customerLabels'

export default function CustomerDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [customer, setCustomer] = useState<CustomerDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    let active = true
    setLoading(true)
    setError(null)
    fetchCustomer(id)
      .then((result) => {
        if (active) setCustomer(result)
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        setCustomer(null)
        setError(err instanceof Error ? err.message : '客户档案加载失败')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [id, navigate])

  if (loading) {
    return (
      <>
        <header className="topbar">
          <div>
            <div className="crumb"><Link to="/customers">客户</Link> / 档案</div>
            <h1>加载中</h1>
          </div>
        </header>
        <main className="content"><div className="empty">加载中</div></main>
      </>
    )
  }

  if (!customer) {
    return (
      <>
        <header className="topbar">
          <div>
            <div className="crumb"><Link to="/customers">客户</Link> / 档案</div>
            <h1>客户不存在</h1>
          </div>
        </header>
        <main className="content"><div className="empty">{error ?? '没有找到这份客户档案'}</div></main>
      </>
    )
  }

  return (
    <>
      <header className="topbar">
        <div>
          <div className="crumb"><Link to="/customers">客户</Link> / 档案</div>
          <h1>{customer.display_name}</h1>
        </div>
        <div className="topbar-actions">
          <Link className="btn" to="/customers">返回列表</Link>
        </div>
      </header>

      <main className="content">
        <div className="detail-grid">
          <div className="detail-side">
            <section className="card">
              <div className="profile-head">
                <div className="avatar">{customer.display_name[0]}</div>
                <div>
                  <h2>{customer.display_name}</h2>
                  <div className="sub">
                    <span className={customer.status === 'active' ? 'badge badge-success' : 'badge badge-muted'}>{customer.status === 'active' ? '活跃' : customer.status}</span>　建档 {shortDate(customer.created_at)}
                  </div>
                </div>
              </div>
              <div className="value-strip">
                <div className="vs"><div className="n">{customer.stats.total_order_amount}</div><div className="l">累计消费</div></div>
                <div className="vs"><div className="n">{customer.stats.orders_count}</div><div className="l">约单</div></div>
                <div className="vs"><div className="n">{customer.stats.last_shot_at ?? '暂无'}</div><div className="l">最近拍摄</div></div>
              </div>
              <div className="kv">
                <span className="k">真实姓名</span><span className="v">{customer.real_name ?? '未填写'}</span>
                <span className="k">手机号</span><span className="v num">{customer.phone ?? '未填写'}</span>
                <span className="k">生日</span><span className="v num">{customer.birthday ?? '未填写'}</span>
                <span className="k">来源渠道</span><span className="v">{channelLabels[customer.channel]}</span>
                <span className="k">介绍人</span><span className="v">{customer.referrer?.display_name ?? '无'}</span>
              </div>
            </section>

            <section className="card">
              <div className="card-title">私域账号 <span className="count">· {customer.identities.length}</span></div>
              {customer.identities.map((identity) => (
                <div className="identity-item" key={identity.id}>
                  <span className="plat">{platformLabels[identity.platform]}</span>
                  <span className="handle">{identity.handle}</span>
                  <span className="rmk">{identity.remark}</span>
                </div>
              ))}
            </section>
          </div>

          <section className="card">
            <div className="tabs">
              <button className="tab active" type="button">备注 · {customer.notes.length}</button>
              <button className="tab" type="button">提醒 · 0</button>
              <button className="tab" type="button">约单 · {customer.stats.orders_count}</button>
            </div>
            {customer.notes.length === 0 ? <div className="empty">暂无备注</div> : null}
          </section>
        </div>
      </main>
    </>
  )
}

function shortDate(value: string) {
  return value.slice(0, 10)
}
