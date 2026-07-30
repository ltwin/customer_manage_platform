import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useEffect, useMemo, useState } from 'react'
import { ApiError, fetchMe } from '../api/client'
import { PrototypeProvider } from '../crm/PrototypeStore'
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
  { key: 'packages', label: '套系', to: '/packages', icon: PackageIcon },
  { key: 'reminders', label: '提醒', to: '/reminders', icon: ReminderIcon },
  { key: 'settings', label: '设置', to: '/settings', icon: SettingsIcon },
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

	const context = useMemo<ShellContext>(
		() => ({
			timezone,
			timezoneError,
			timezoneLoading,
			retryTimezone() {
				setTimezoneReloadTick((current) => current + 1)
			},
			notify(message: string) {
				setToast(message)
			},
		}),
			[timezone, timezoneError, timezoneLoading],
	)

  return (
    <PrototypeProvider>
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
          <div className="nav-foot">工作室单账号 · 数据可随时导出</div>
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

      <button
        type="button"
        className="theme-toggle icon-btn"
        onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
        aria-label={theme === 'dark' ? '切换到浅色模式' : '切换到暗色模式'}
        title={theme === 'dark' ? '切换到浅色模式' : '切换到暗色模式'}
      >
        {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
      </button>

      <div id="toast" className={toast ? 'show' : ''} role="status" aria-live="polite">
        {toast}
      </div>
    </PrototypeProvider>
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

function SettingsIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9c.3.6.9 1 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
    </svg>
  )
}

function MoonIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M20.2 14.2A8.2 8.2 0 0 1 9.8 3.8a8.2 8.2 0 1 0 10.4 10.4z" />
    </svg>
  )
}

function SunIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
      <circle cx="12" cy="12" r="4.2" />
      <path d="M12 2.5v2.6M12 18.9v2.6M2.5 12h2.6M18.9 12h2.6M5.3 5.3l1.8 1.8M16.9 16.9l1.8 1.8M18.7 5.3l-1.8 1.8M7.1 16.9l-1.8 1.8" />
    </svg>
  )
}
