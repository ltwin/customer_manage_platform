import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeft, CalendarPlus, GitMerge, Pencil, Archive, ArchiveRestore } from 'lucide-react'
import clsx from 'clsx'
import { ApiError, deleteCustomerAvatar, fetchCustomer, putCustomerAvatar, updateCustomer } from '../api/client'
import type { CustomerDetail } from '../api/client'
import { channelLabels } from './customerLabels'
import { formatCustomerTotalSpend } from './customerDetailMoney'
import CustomerProfileForm from '../components/customers/CustomerProfileForm'
import IdentitySection from '../components/customers/IdentitySection'
import NotesPanel from '../components/customers/NotesPanel'
import MergeDialog from '../components/customers/MergeDialog'
import CustomerRemindersPanel from '../components/CustomerRemindersPanel'
import OrderWorkspace from '../components/orders/OrderWorkspace'
import { listReminders } from '../api/client'
import ScheduleSlotDialog from '../components/schedule/ScheduleSlotDialog'
import { scheduleDialogShouldOpen } from '../components/schedule/flow'
import { readPendingSchedule } from '../components/schedule/journal'
import { accountToday } from '../components/schedule/timezone'
import { useShell } from '../components/shellContext'
import CustomerAvatar from '../components/customers/CustomerAvatar'
import StateNotice from '../components/StateNotice'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  terminalPageReadError,
  type PageReadState,
} from '../components/pageReadState'

const statusLabels: Record<string, string> = {
  active: '活跃',
  archived: '已归档',
  merged: '已合并',
}

type CustomerTabKey = 'notes' | 'reminders' | 'orders'

const customerTabKeys: CustomerTabKey[] = ['notes', 'reminders', 'orders']

export default function CustomerDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { notify, timezone } = useShell()
  const [readState, setReadState] = useState<PageReadState<CustomerDetail>>({
    kind: 'loading',
    message: '正在加载客户档案',
  })
  const [detailReloadTick, setDetailReloadTick] = useState(0)
  const loadedCustomerIDRef = useRef('')
  const [editing, setEditing] = useState(false)
  const [merging, setMerging] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<CustomerTabKey>(() => searchParams.get('tab') === 'orders' ? 'orders' : 'notes')
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [scheduledDate, setScheduledDate] = useState<string | null>(null)
  const [reminderCount, setReminderCount] = useState(0)
  const [reminderTick, setReminderTick] = useState(0)
	const avatarInputRef = useRef<HTMLInputElement>(null)
	const [avatarBusy, setAvatarBusy] = useState(false)

  const goLogin = useCallback(() => {
    navigate('/login', { replace: true })
  }, [navigate])

  const reload = useCallback(() => {
    if (!id) return
    setReadState((current) => beginPageRead(current, '正在刷新客户档案', true))
    fetchCustomer(id)
      .then((result) => setReadState(completePageRead(result, false, '')))
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          setReadState({ kind: 'unauthorized' })
          goLogin()
          return
        }
        setReadState((current) => failPageRead(
          current,
          err instanceof Error ? err.message : '档案刷新失败',
          () => setDetailReloadTick((value) => value + 1),
        ))
      })
  }, [goLogin, id])

  useEffect(() => {
    if (!id) return
    let active = true
    const preserveReady = loadedCustomerIDRef.current === id
    loadedCustomerIDRef.current = id
    setReadState((current) => beginPageRead(current, '正在加载客户档案', preserveReady))
    fetchCustomer(id)
      .then((result) => {
        if (active) setReadState(completePageRead(result, false, ''))
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          setReadState({ kind: 'unauthorized' })
          goLogin()
          return
        }
        if (err instanceof ApiError && err.status === 404) {
          setReadState(terminalPageReadError('没有找到这份客户档案'))
          return
        }
        setReadState((current) => failPageRead(
          current,
          err instanceof Error ? err.message : '客户档案加载失败',
          () => setDetailReloadTick((value) => value + 1),
        ))
      })
    return () => {
      active = false
    }
  }, [detailReloadTick, goLogin, id])

  useEffect(() => {
    if (searchParams.get('tab') === 'orders') setActiveTab('orders')
  }, [searchParams])

  useEffect(() => {
    if (!id) return
    let active = true
    listReminders({ customerId: id, page: 1, pageSize: 1 })
      .then((res) => {
        if (active) setReminderCount(res.total)
      })
      .catch(() => {
        if (active) setReminderCount(0)
      })
    return () => {
      active = false
    }
  }, [id, reminderTick])

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

	async function uploadAvatar(file: File | undefined) {
		if (!customer || !file) return
		setAvatarBusy(true)
		setActionError(null)
		try {
			await putCustomerAvatar(customer.id ?? id, file, customer.avatar_revision)
			reload()
			notify(customer.avatar_url ? '客户头像已替换' : '客户头像已设置')
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) {
				goLogin()
				return
			}
			if (err instanceof ApiError && err.code === 'avatar_revision_conflict') reload()
			setActionError(err instanceof Error ? err.message : '头像上传失败')
		} finally {
			setAvatarBusy(false)
			if (avatarInputRef.current) avatarInputRef.current.value = ''
		}
	}

	async function removeAvatar() {
		if (!customer) return
		setAvatarBusy(true)
		setActionError(null)
		try {
			await deleteCustomerAvatar(customer.id ?? id, customer.avatar_revision)
			reload()
			notify('客户头像已移除')
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) {
				goLogin()
				return
			}
			if (err instanceof ApiError && err.code === 'avatar_revision_conflict') reload()
			setActionError(err instanceof Error ? err.message : '头像移除失败')
		} finally {
			setAvatarBusy(false)
		}
	}

  // 左右方向键在 tablist 内切换，是 role="tablist" 的键盘契约
  function onTabKeyDown(event: React.KeyboardEvent<HTMLButtonElement>) {
    const offset = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
    if (offset === 0) return
    event.preventDefault()
    const index = customerTabKeys.indexOf(activeTab)
    const next = customerTabKeys[(index + offset + customerTabKeys.length) % customerTabKeys.length]
    if (!next) return
    setActiveTab(next)
    document.getElementById(`customerTab-${next}`)?.focus()
  }

  const presentation = pageReadPresentation(readState)
  const customer = readyPageData(readState)

  if (!presentation.showReadyData || !customer) {
    return (
      <>
        <header className="topbar">
          <div>
            <div className="crumb"><Link to="/customers">客户</Link> / 档案</div>
            <h1>客户档案</h1>
          </div>
        </header>
        <main className="content">
          {presentation.notice && <StateNotice {...presentation.notice} />}
        </main>
      </>
    )
  }

  const isMerged = customer.status === 'merged'
  const isArchived = customer.status === 'archived'
  const tabItems: { key: CustomerTabKey; label: string }[] = [
    { key: 'notes', label: `备注 · ${customer.notes.length}` },
    { key: 'reminders', label: `提醒 · ${reminderCount}` },
    { key: 'orders', label: `约单 · ${customer.stats.orders_count}` },
  ]

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
              <button className="btn" type="button" onClick={() => setEditing(true)}>
                <Pencil aria-hidden="true" strokeWidth={2} />
                编辑档案
              </button>
              <button className="btn" type="button" onClick={() => { void toggleArchive() }}>
                {isArchived
                  ? <ArchiveRestore aria-hidden="true" strokeWidth={2} />
                  : <Archive aria-hidden="true" strokeWidth={2} />}
                {isArchived ? '恢复经营' : '归档'}
              </button>
              {customer.status === 'active' && (
                <>
                  <button className="btn btn-primary" type="button" disabled={!timezone} onClick={() => setScheduleOpen(true)}>
                    <CalendarPlus aria-hidden="true" strokeWidth={2.2} />
                    新建拍摄档期
                  </button>
                  <button className="btn" type="button" onClick={() => setMerging(true)}>
                    <GitMerge aria-hidden="true" strokeWidth={2} />
                    合并重复档案
                  </button>
                </>
              )}
            </>
          )}
          <Link className="btn" to="/customers">
            <ArrowLeft aria-hidden="true" strokeWidth={2} />
            返回列表
          </Link>
        </div>
      </header>

      <main className="content">
        {presentation.notice && <StateNotice {...presentation.notice} />}
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
                  <CustomerAvatar
					customerId={customer.id ?? id}
					displayName={customer.display_name}
					avatarRevision={customer.avatar_revision}
					avatarUrl={customer.avatar_url}
					size="lg"
				/>
                  <div>
                    <h2>{customer.display_name}</h2>
                    <div className="sub">
                      <span className={customer.status === 'active' ? 'badge badge-success' : 'badge badge-muted'}>
                        {statusLabels[customer.status] ?? customer.status}
                      </span>　建档 {shortDate(customer.created_at)}
                    </div>
                  </div>
                </div>
				<div className="avatar-actions">
					{!isMerged && (
						<>
							<input
								ref={avatarInputRef}
								type="file"
								accept="image/jpeg,image/png,image/webp"
								hidden
								onChange={(event) => { void uploadAvatar(event.target.files?.[0]) }}
							/>
							<button className="btn btn-sm" type="button" disabled={avatarBusy} onClick={() => avatarInputRef.current?.click()}>
								{avatarBusy ? '处理中' : customer.avatar_url ? '替换头像' : '设置头像'}
							</button>
						</>
					)}
					{customer.avatar_url && (
						<button className="btn btn-sm" type="button" disabled={avatarBusy} onClick={() => { void removeAvatar() }}>
							{isMerged ? '移除头像（隐私清理）' : '移除头像'}
						</button>
					)}
				</div>
                <div className="value-strip">
                  <div className="vs"><div className="n">{formatCustomerTotalSpend(customer.stats.total_order_amount)}</div><div className="l">累计消费</div></div>
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
            <div className="tabs" role="tablist" aria-label="客户资料分区">
              {tabItems.map(({ key, label }) => (
                <button
                  key={key}
                  id={`customerTab-${key}`}
                  className={clsx('tab', activeTab === key && 'active')}
                  type="button"
                  role="tab"
                  aria-selected={activeTab === key}
                  aria-controls={`customerPanel-${key}`}
                  tabIndex={activeTab === key ? 0 : -1}
                  onKeyDown={onTabKeyDown}
                  onClick={() => setActiveTab(key)}
                >
                  {label}
                </button>
              ))}
            </div>
            {activeTab === 'notes' && (
              <div className="tab-panel active" id="customerPanel-notes" role="tabpanel" aria-labelledby="customerTab-notes" tabIndex={0}>
                <NotesPanel customer={customer} onChanged={reload} onUnauthorized={goLogin} />
              </div>
            )}
            {activeTab === 'reminders' && (
              <div className="tab-panel active" id="customerPanel-reminders" role="tabpanel" aria-labelledby="customerTab-reminders" tabIndex={0}>
                <CustomerRemindersPanel
                  customerId={customer.id ?? id}
                  onUnauthorized={goLogin}
                  onChanged={() => setReminderTick((n) => n + 1)}
                />
              </div>
            )}
            {activeTab === 'orders' && (
              <div className="tab-panel active" id="customerPanel-orders" role="tabpanel" aria-labelledby="customerTab-orders" tabIndex={0}>
              <OrderWorkspace
                customer={customer}
                onChanged={reload}
                focusOrderId={searchParams.get('order') ?? undefined}
                scheduleDraftId={searchParams.get('schedule_draft') ?? undefined}
                scheduleMode={searchParams.get('mode') ?? undefined}
              />
              </div>
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
          avatar_revision: customer.avatar_revision,
          avatar_url: customer.avatar_url,
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
