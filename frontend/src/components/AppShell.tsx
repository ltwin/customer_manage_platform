import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useEffect, useMemo, useState } from 'react'
import {
  Bell,
  CalendarDays,
  LayoutDashboard,
  Package,
  ReceiptText,
  Users,
} from 'lucide-react'
import { ApiError, fetchMe } from '../api/client'
import { AccountCenterProvider } from '../account/AccountCenterContext.tsx'
import AccountMenu from '../account/AccountMenu.tsx'
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
  { key: 'dashboard', label: '仪表盘', to: '/dashboard', icon: LayoutDashboard },
  { key: 'customers', label: '客户', to: '/customers', icon: Users },
  { key: 'orders', label: '订单', to: '/orders', icon: ReceiptText },
  { key: 'calendar', label: '档期', to: '/calendar', icon: CalendarDays },
  { key: 'packages', label: '套系', to: '/packages', icon: Package },
  { key: 'reminders', label: '提醒', to: '/reminders', icon: Bell },
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
                <Icon aria-hidden="true" strokeWidth={1.8} />
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
              <Icon aria-hidden="true" strokeWidth={1.8} />
              <span>{item.label}</span>
            </NavLink>
          )
        })}
      </nav>

      <AccountMenu slot="mobile" />

      <div id="toast" className={toast ? 'show' : ''} role="status" aria-live="polite">
        {toast}
      </div>
    </PrototypeProvider>
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
