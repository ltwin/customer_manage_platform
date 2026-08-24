import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useEffect, useMemo, useState } from 'react'
import { ApiError, fetchMe } from '../api/client'
import { AccountCenterProvider } from '../account/AccountCenterContext.tsx'
import AccountMenu from '../account/AccountMenu.tsx'
import type { ShellContext } from './shellContext'
import StateNotice from './StateNotice'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from './pageReadState'

const navItems = [
  { key: 'dashboard', label: '仪表盘', to: '/dashboard', icon: DashboardIcon },
  { key: 'customers', label: '客户', to: '/customers', icon: CustomersIcon },
  { key: 'orders', label: '订单', to: '/orders', icon: OrdersIcon },
  { key: 'calendar', label: '档期', to: '/calendar', icon: CalendarIcon },
  { key: 'planning', label: '策划', to: '/shoot-plans', icon: PlanningIcon },
  { key: 'packages', label: '套系', to: '/packages', icon: PackageIcon },
  { key: 'reminders', label: '提醒', to: '/reminders', icon: ReminderIcon },
]

export default function AppShell() {
	const navigate = useNavigate()
	const [theme, setTheme] = useState(() => readTheme())
	const [toast, setToast] = useState<string | null>(null)
		const [timezoneState, setTimezoneState] = useState<PageReadState<string>>({
			kind: 'loading',
			message: '正在加载账号时区',
		})
		const [timezoneReloadTick, setTimezoneReloadTick] = useState(0)

	useEffect(() => {
		let active = true
			setTimezoneState((current) => beginPageRead(current, '正在加载账号时区', true))
			fetchMe()
				.then((account) => {
					if (!active) return
					setTimezoneState(completePageRead(account.timezone, false, ''))
				})
			.catch((error: unknown) => {
					if (!active) return
					if (error instanceof ApiError && error.status === 401) {
						setTimezoneState({ kind: 'unauthorized' })
						navigate('/login', { replace: true })
						return
					}
					setTimezoneState((current) => failPageRead(
						current,
						error instanceof Error ? error.message : '账号时区加载失败',
						() => setTimezoneReloadTick((value) => value + 1),
					))
				})
		return () => {
			active = false
		}
		}, [navigate, timezoneReloadTick])

		const timezonePresentation = pageReadPresentation(timezoneState)
		const timezone = readyPageData(timezoneState)
		const timezoneError = timezonePresentation.notice?.kind === 'error' || timezonePresentation.notice?.kind === 'refresh-error'
			? timezonePresentation.notice.message
			: null
		const timezoneLoading = timezoneState.kind === 'loading'

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    saveTheme(theme)
  }, [theme])

  useEffect(() => {
    if (!toast) return
    const timer = window.setTimeout(() => setToast(null), 2400)
    return () => window.clearTimeout(timer)
  }, [toast])

	const notify = useMemo(() => {
		return (message: string) => {
			setToast(message)
		}
	}, [])

	const context = useMemo<ShellContext>(
		() => ({
			timezone,
			timezoneError,
			timezoneLoading,
			retryTimezone() {
				setTimezoneReloadTick((current) => current + 1)
			},
			notify,
		}),
			[timezone, timezoneError, timezoneLoading, notify],
	)

  return (
    <AccountCenterProvider theme={theme} setTheme={setTheme} notify={notify}>
      <div className="app-shell">
        <aside className="sidebar">
          <NavLink className="brand" to="/dashboard">
            <span className="brand-mark">影</span>
            <span>影约 CRM<small>摄影师私域客户经营</small></span>
          </NavLink>
          {navItems.map((item) => {
            const Icon = item.icon
            return (
              <NavLink key={item.key} className="nav-item" to={item.to}>
                <Icon />
                {item.label}
              </NavLink>
            )
          })}
          <div className="nav-spacer" />
          <div className="nav-account-actions" aria-label="账号菜单">
            <AccountMenu slot="desktop" />
          </div>
          <div className="nav-foot">演示单账号 · 数据可随时导出</div>
        </aside>

			        <div className="main">
				          {timezonePresentation.notice && <StateNotice {...timezonePresentation.notice} />}
				          <Outlet context={context} />
        </div>
      </div>

      <nav className="bottom-nav" aria-label="主导航">
        {navItems.map((item) => {
          const Icon = item.icon
          return (
            <NavLink key={item.key} to={item.to}>
              <Icon />
              <span>{item.label}</span>
            </NavLink>
          )
        })}
      </nav>

      <AccountMenu slot="mobile" />

      <div id="toast" className={toast ? 'show' : ''} role="status" aria-live="polite">
        {toast}
      </div>
    </AccountCenterProvider>
  )
}

function readTheme(): 'light' | 'dark' {
  try {
    return localStorage.getItem('od-crm-theme') === 'dark' ? 'dark' : 'light'
  } catch {
    return 'light'
  }
}

function saveTheme(theme: 'light' | 'dark') {
  try {
    localStorage.setItem('od-crm-theme', theme)
  } catch {
    // 预览沙箱可能禁用 localStorage，本页主题仍可正常切换。
  }
}

function DashboardIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="3" width="7" height="9" rx="1.5" />
      <rect x="14" y="3" width="7" height="5" rx="1.5" />
      <rect x="14" y="12" width="7" height="9" rx="1.5" />
      <rect x="3" y="16" width="7" height="5" rx="1.5" />
    </svg>
  )
}

function CustomersIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="9" cy="8" r="3.2" />
      <path d="M3.5 19c.7-3 2.9-4.5 5.5-4.5S13.8 16 14.5 19" />
      <circle cx="17" cy="9" r="2.4" />
      <path d="M16.2 14.7c2.3.1 3.9 1.5 4.4 3.8" />
    </svg>
  )
}

function CalendarIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3.5" y="5" width="17" height="16" rx="2" />
      <path d="M3.5 10h17M8 3v4M16 3v4" />
    </svg>
  )
}

function PlanningIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="4" y="3.5" width="16" height="17" rx="2" />
      <path d="M8 8h8M8 12h5M8 16h7" />
      <path d="M16.5 11.5l2 2-4.5 4.5H12v-2z" />
    </svg>
  )
}

function OrdersIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M7 4h10a2 2 0 0 1 2 2v14l-3-1.8L12 20l-4-1.8L5 20V6a2 2 0 0 1 2-2z" />
      <path d="M8.5 8h7M8.5 12h7M8.5 16H13" />
    </svg>
  )
}

function PackageIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3l8 4.5v9L12 21l-8-4.5v-9L12 3z" />
      <path d="M4 7.5l8 4.5 8-4.5M12 12v9" />
    </svg>
  )
}

function ReminderIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 4a6 6 0 0 0-6 6v3.2L4.5 16h15L18 13.2V10a6 6 0 0 0-6-6z" />
      <path d="M10 18a2 2 0 0 0 4 0" />
    </svg>
  )
}
