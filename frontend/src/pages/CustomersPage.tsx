import { useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { DICT } from '../crm/prototypeData'
import type { Channel, Platform } from '../crm/prototypeData'
import { byId, customerOrders, lastShotDate, shortDate, visibleBirthday } from '../crm/model'
import { usePrototypeStore } from '../crm/prototypeStoreContext'
import { useShell } from '../components/shellContext'

const channels: Array<['all' | Channel, string]> = [['all', '全部'], ...Object.entries(DICT.channel) as Array<[Channel, string]>]

export default function CustomersPage() {
  const store = usePrototypeStore()
  const { notify } = useShell()
  const navigate = useNavigate()
  const location = useLocation()
  const [query, setQuery] = useState('')
  const [activeChannel, setActiveChannel] = useState<'all' | Channel>('all')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [displayName, setDisplayName] = useState('')
  const [platform, setPlatform] = useState<Platform>('wechat')
  const [handle, setHandle] = useState('')
  const [channel, setChannel] = useState<Channel>('xiaohongshu')
  const [submitted, setSubmitted] = useState(false)

  useEffect(() => {
    if (location.pathname.endsWith('/new') || location.hash === '#new') setDialogOpen(true)
  }, [location.hash, location.pathname])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return store.customers.filter((customer) => {
      const channelHit = activeChannel === 'all' || customer.channel === activeChannel
      const haystack = [
        customer.display_name,
        customer.real_name,
        customer.phone,
        ...customer.identities.map((identity) => identity.handle),
      ].join(' ').toLowerCase()
      return channelHit && (!q || haystack.includes(q))
    })
  }, [activeChannel, query, store.customers])

  function closeDialog() {
    setDialogOpen(false)
    if (location.pathname.endsWith('/new')) navigate('/customers', { replace: true })
  }

  function saveCustomer() {
    setSubmitted(true)
    if (!displayName.trim() || !handle.trim()) return
    const id = store.addCustomer({
      displayName: displayName.trim(),
      platform,
      handle: handle.trim(),
      channel,
    })
    notify(`已建档「${displayName.trim()}」`)
    closeDialog()
    setDisplayName('')
    setHandle('')
    setSubmitted(false)
    navigate(`/customers/${id}`)
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>客户</h1>
          <div className="sub">共 <span className="num">{store.customers.length}</span> 位 · 私域渠道统一管理</div>
        </div>
        <div className="topbar-actions">
          <button className="btn btn-primary" type="button" onClick={() => setDialogOpen(true)}>＋ 30 秒建档</button>
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
            {channels.map(([key, label]) => (
              <button
                key={key}
                className={`chip${activeChannel === key ? ' active' : ''}`}
                type="button"
                onClick={() => setActiveChannel(key)}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>客户</th>
                <th>私域账号</th>
                <th>来源</th>
                <th>约单</th>
                <th>最近拍摄</th>
                <th>生日</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {filtered.length === 0 ? (
                <tr><td colSpan={7}><div className="empty inline-empty">没有匹配的客户，试试换个关键词</div></td></tr>
              ) : filtered.map((customer) => {
                const orders = customerOrders(store.orders, customer.id)
                const referrer = byId(store.customers, customer.referrer_customer_id)
                return (
                  <tr key={customer.id} onClick={() => navigate(`/customers/${customer.id}`)}>
                    <td>
                      <div className="cell-name">
                        <div className={`avatar ${customer.avatar}`}>{customer.display_name[0]}</div>
                        <div>
                          <div className="nm">{customer.display_name}</div>
                          <div className="rn">{customer.real_name ? `${customer.real_name} · ` : ''}{customer.phone ? <span className="num">{customer.phone}</span> : '未留手机号'}</div>
                        </div>
                      </div>
                    </td>
                    <td>
                      <div className="identity-chips">
                        {customer.identities.map((identity) => (
                          <span className="id-chip" key={`${identity.platform}-${identity.handle}`}>{DICT.platform[identity.platform]} · {identity.handle}</span>
                        ))}
                      </div>
                    </td>
                    <td>
                      <span className="badge badge-muted">{DICT.channel[customer.channel]}</span>
                      {referrer && <div className="tiny-meta">由 {referrer.display_name} 介绍</div>}
                    </td>
                    <td><span className="num">{orders.length}</span> 单</td>
                    <td className="num muted-text">{shortDate(lastShotDate(store.orders, customer.id))}</td>
                    <td className="num muted-text">{visibleBirthday(customer.birthday)}</td>
                    <td>{customer.status === 'archived' ? <span className="badge badge-muted">已归档</span> : <span className="badge badge-success">活跃</span>}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        <div className="result-meta">显示 {filtered.length} / {store.customers.length} 位客户</div>
      </main>

      <div className={`overlay${dialogOpen ? ' open' : ''}`} onClick={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="newCustomerTitle">
          <h2 id="newCustomerTitle">30 秒建档</h2>
          <div className="dialog-sub">只需昵称 + 一个联系账号，其余信息可在档案页随时补全</div>

          <div className={`field${submitted && !displayName.trim() ? ' show-err' : ''}`}>
            <label htmlFor="newDisplayName">昵称 *</label>
            <input id="newDisplayName" className={`input${submitted && !displayName.trim() ? ' invalid' : ''}`} value={displayName} onChange={(event) => setDisplayName(event.target.value)} placeholder="客户常用称呼，如：阿茶" autoFocus />
            <div className="err">昵称不能为空</div>
          </div>

          <div className="field-row">
            <div className="field">
              <label htmlFor="newPlatform">平台</label>
              <select id="newPlatform" className="input" value={platform} onChange={(event) => setPlatform(event.target.value as Platform)}>
                {Object.entries(DICT.platform).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
              </select>
            </div>
            <div className={`field${submitted && !handle.trim() ? ' show-err' : ''}`}>
              <label htmlFor="newHandle">账号 / ID *</label>
              <input id="newHandle" className={`input${submitted && !handle.trim() ? ' invalid' : ''}`} value={handle} onChange={(event) => setHandle(event.target.value)} placeholder="如：tea_moli" />
              <div className="err">账号不能为空</div>
            </div>
          </div>

          <div className="field">
            <label htmlFor="newChannel">来源渠道</label>
            <select id="newChannel" className="input" value={channel} onChange={(event) => setChannel(event.target.value as Channel)}>
              {Object.entries(DICT.channel).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
            </select>
            <div className="hint">选「转介绍」后可在正式档案能力里关联介绍人，自动记入对方人脉链</div>
          </div>

          <div className="dialog-actions">
            <button className="btn" type="button" onClick={closeDialog}>取消</button>
            <button className="btn btn-primary" type="button" onClick={saveCustomer}>保存并打开档案</button>
          </div>
        </div>
      </div>

      <Link className="sr-only" to="/customers/new">打开建档弹窗</Link>
    </>
  )
}
