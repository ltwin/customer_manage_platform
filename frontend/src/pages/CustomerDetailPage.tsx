import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useCallback, useEffect, useState } from 'react'
import { ApiError, fetchCustomer, updateCustomer } from '../api/client'
import type { CustomerDetail } from '../api/client'
import { channelLabels } from './customerLabels'
import CustomerProfileForm from '../components/customers/CustomerProfileForm'
import IdentitySection from '../components/customers/IdentitySection'
import NotesPanel from '../components/customers/NotesPanel'
import MergeDialog from '../components/customers/MergeDialog'
import OrderWorkspace from '../components/orders/OrderWorkspace'
import ScheduleSlotDialog from '../components/schedule/ScheduleSlotDialog'
import { scheduleDialogShouldOpen } from '../components/schedule/flow'
import { readPendingSchedule } from '../components/schedule/journal'
import { accountToday } from '../components/schedule/timezone'
import { useShell } from '../components/shellContext'

const statusLabels: Record<string, string> = {
  active: '活跃',
  archived: '已归档',
  merged: '已合并',
}

export default function CustomerDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { notify, timezone } = useShell()
  const [customer, setCustomer] = useState<CustomerDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [merging, setMerging] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<'notes' | 'reminders' | 'orders'>(() => searchParams.get('tab') === 'orders' ? 'orders' : 'notes')
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [scheduledDate, setScheduledDate] = useState<string | null>(null)

  const goLogin = useCallback(() => {
    navigate('/login', { replace: true })
  }, [navigate])

  const reload = useCallback(() => {
    if (!id) return
    fetchCustomer(id)
      .then(setCustomer)
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          goLogin()
          return
        }
        setActionError(err instanceof Error ? err.message : '档案刷新失败')
      })
  }, [goLogin, id])

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
          goLogin()
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
  }, [goLogin, id])

  useEffect(() => {
    if (searchParams.get('tab') === 'orders') setActiveTab('orders')
  }, [searchParams])

  useEffect(() => {
    try {
      const pending = readPendingSchedule()
      setScheduleOpen(scheduleDialogShouldOpen(
        pending?.phase,
        searchParams.get('schedule_draft') ?? undefined,
        searchParams.get('mode') ?? undefined,
      ))
    } catch {
      // The dialog reports malformed recovery state when explicitly opened.
    }
  }, [searchParams])

  async function toggleArchive() {
    if (!customer) return
    setActionError(null)
    const next = customer.status === 'archived' ? 'active' : 'archived'
    try {
      await updateCustomer(customer.id ?? '', { status: next })
      reload()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setActionError(err instanceof Error ? err.message : '操作失败')
    }
  }

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

  const isMerged = customer.status === 'merged'
  const isArchived = customer.status === 'archived'

  return (
    <>
      <header className="topbar">
        <div>
          <div className="crumb"><Link to="/customers">客户</Link> / 档案</div>
          <h1>{customer.display_name}</h1>
        </div>
        <div className="topbar-actions">
          {!isMerged && (
            <>
              <button className="btn" type="button" onClick={() => setEditing(true)}>编辑档案</button>
              <button className="btn" type="button" onClick={() => { void toggleArchive() }}>
                {isArchived ? '恢复经营' : '归档'}
              </button>
              {customer.status === 'active' && (
                <>
                  <button className="btn btn-primary" type="button" disabled={!timezone} onClick={() => setScheduleOpen(true)}>新建拍摄档期</button>
                  <button className="btn" type="button" onClick={() => setMerging(true)}>合并重复档案</button>
                </>
              )}
            </>
          )}
          <Link className="btn" to="/customers">返回列表</Link>
        </div>
      </header>

      <main className="content">
        {actionError && <div className="form-error">{actionError}</div>}
        {isMerged && (
          <div className="form-error">
            该客户已合并，档案只读。
            {customer.merged_into_customer_id && (
              <>
                {' '}
                <Link to={`/customers/${customer.merged_into_customer_id}`}>查看合并后的档案 →</Link>
              </>
            )}
          </div>
        )}
        <div className="detail-grid">
          <div className="detail-side">
            {editing ? (
              <CustomerProfileForm
                customer={customer}
                onSaved={() => {
                  setEditing(false)
                  reload()
                }}
                onCancel={() => setEditing(false)}
                onUnauthorized={goLogin}
              />
            ) : (
              <section className="card">
                <div className="profile-head">
                  <div className="avatar">{customer.display_name[0]}</div>
                  <div>
                    <h2>{customer.display_name}</h2>
                    <div className="sub">
                      <span className={customer.status === 'active' ? 'badge badge-success' : 'badge badge-muted'}>
                        {statusLabels[customer.status] ?? customer.status}
                      </span>　建档 {shortDate(customer.created_at)}
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
                  <span className="k">介绍人</span>
                  <span className="v">
                    {customer.referrer
                      ? customer.referrer.display_name
                      : customer.channel === 'referral'
                        ? '介绍人已失效'
                        : '无'}
                  </span>
                </div>
              </section>
            )}

            <IdentitySection customer={customer} onChanged={reload} onUnauthorized={goLogin} />
          </div>

          <section className="card">
            <div className="tabs">
              <button className={`tab${activeTab === 'notes' ? ' active' : ''}`} type="button" onClick={() => setActiveTab('notes')}>备注 · {customer.notes.length}</button>
              <button className={`tab${activeTab === 'reminders' ? ' active' : ''}`} type="button" onClick={() => setActiveTab('reminders')}>提醒 · 0</button>
              <button className={`tab${activeTab === 'orders' ? ' active' : ''}`} type="button" onClick={() => setActiveTab('orders')}>约单 · {customer.stats.orders_count}</button>
            </div>
            {activeTab === 'notes' && <NotesPanel customer={customer} onChanged={reload} onUnauthorized={goLogin} />}
            {activeTab === 'reminders' && <div className="empty inline-empty">暂无提醒</div>}
            {activeTab === 'orders' && (
              <OrderWorkspace
                customer={customer}
                onChanged={reload}
                focusOrderId={searchParams.get('order') ?? undefined}
                scheduleDraftId={searchParams.get('schedule_draft') ?? undefined}
                scheduleMode={searchParams.get('mode') ?? undefined}
              />
            )}
          </section>
        </div>
      </main>

      {merging && (
        <MergeDialog
          targetId={customer.id ?? ''}
          targetName={customer.display_name}
          onMerged={() => {
            setMerging(false)
            reload()
          }}
          onConflict={reload}
          onClose={() => setMerging(false)}
          onUnauthorized={goLogin}
        />
      )}

      <ScheduleSlotDialog
        open={scheduleOpen}
        timezone={timezone}
        initialDate={timezone ? accountToday(timezone) : ''}
        scheduleDraftId={searchParams.get('schedule_draft') ?? undefined}
        fixedType="shoot"
        fixedCustomer={{
          id: customer.id ?? id,
          display_name: customer.display_name,
          status: customer.status,
        }}
        source="customer"
        onClose={() => setScheduleOpen(false)}
        onChanged={async () => { reload() }}
        onCompleted={(date) => {
          setScheduledDate(date)
          setSearchParams({ tab: 'orders' }, { replace: true })
          notify('拍摄档期已保存')
        }}
      />

      {scheduledDate && (
        <div className="schedule-complete-bar" role="status">
          拍摄档期已保存
          <Link to={`/calendar?date=${scheduledDate}`}>查看该日档期</Link>
          <button className="icon-btn" type="button" aria-label="关闭" onClick={() => setScheduledDate(null)}>×</button>
        </div>
      )}
    </>
  )
}

function shortDate(value: string) {
  return value.slice(0, 10)
}
