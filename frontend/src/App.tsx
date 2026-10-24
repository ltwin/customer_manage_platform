import { lazy, Suspense, useEffect, useSyncExternalStore } from 'react'
import type { ReactElement } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import AppShell from './components/AppShell'
import { getAuthSnapshot, restoreSession, subscribeAuth } from './auth/session'
import CalendarPage from './pages/CalendarPage'
import CustomerDetailPage from './pages/CustomerDetailPage'
import CustomerNewPage from './pages/CustomerNewPage'
import CustomersPage from './pages/CustomersPage'
import DashboardPage from './pages/DashboardPage'
import OrdersPage from './pages/OrdersPage'
import PackagesPage from './pages/PackagesPage'
import RemindersPage from './pages/RemindersPage'
import WelcomePage from './pages/WelcomePage'
import LoginPage from './pages/auth/LoginPage'
import RegisterPage from './pages/auth/RegisterPage'
import VerifyEmailPage from './pages/auth/VerifyEmailPage'
import ForgotPasswordPage from './pages/auth/ForgotPasswordPage'
import ResetPasswordPage from './pages/auth/ResetPasswordPage'
import AccountCenterLayout from './account/AccountCenterLayout.tsx'
import AccountOverviewPage from './account/AccountOverviewPage.tsx'
import AccountProfilePage from './account/AccountProfilePage.tsx'
import AccountSecurityPage from './account/AccountSecurityPage.tsx'
import AccountPasswordPage from './account/AccountPasswordPage.tsx'
import AccountSettingsPage from './account/settings/AccountSettingsPage.tsx'
import { peekLoginNotice } from './account/security/loginNotice.ts'
import ShootPlansPage from './planning/ShootPlansPage'
import ShootPlanWorkspacePage from './planning/ShootPlanWorkspacePage'
import ShootPlanRunPage from './planning/ShootPlanRunPage'
import ShootPlanIngestionPage from './planning/ShootPlanIngestionPage'
import SharedPlanPage from './planning/share/SharedPlanPage'
import PrivacyPage from './pages/privacy/PrivacyPage'

import CreativeLayout, { CanvasLoading } from './creative-canvas/home/CreativeLayout.tsx'
import {
  CreativeAgentsPage,
  CreativeAssetsPage,
  CreativeCanvasPage,
  CreativeHomePage,
  CreativeProjectsPage,
} from './creative-canvas/pages.ts'

const AgentConsentPage = lazy(() => import('./pages/auth/AgentConsentPage'))

// 懒加载路由的 Suspense 各带不同 key：路由元素本身没有 key，同类型的 Suspense 会被
// React 复用成「已显示」的边界，而 React Router 把导航放在 transition 里，结果是
// 旧页面一直留在屏幕上、点了也不跳，直到新页面 chunk 下载完。独立 key 让导航立刻
// 卸下旧页面并显示 fallback。创意空间的四个页面在 CreativeLayout 里按同样的方式处理。

type AuthStatus = ReturnType<typeof getAuthSnapshot>['status']

function RequireAuth({ children, status }: { children: ReactElement; status: AuthStatus }) {
  const location = useLocation()
  if (status !== 'authenticated') {
    const notice = peekLoginNotice()
    return (
      <Navigate
        to="/login"
        replace
        state={notice ? { from: location.pathname, notice } : { from: location.pathname }}
      />
    )
  }
  return children
}

function AnonymousEntry({ children, status }: { children: ReactElement; status: AuthStatus }) {
  return status === 'authenticated' ? <Navigate to="/dashboard" replace /> : children
}

function RootEntry({ status }: { status: AuthStatus }) {
  return status === 'authenticated' ? <Navigate to="/dashboard" replace /> : <WelcomePage />
}

export default function App() {
  const location = useLocation()
  const auth = useSyncExternalStore(subscribeAuth, getAuthSnapshot)
  const isActionTokenRoute = location.pathname === '/verify-email' || location.pathname === '/reset-password'
  const isPublicShareRoute = location.pathname.startsWith('/shared/plans/')
  const skipSessionRestore = isActionTokenRoute || isPublicShareRoute || location.pathname === '/privacy'

  useEffect(() => {
    if (!skipSessionRestore && auth.status === 'restoring') void restoreSession()
  }, [auth.status, skipSessionRestore])

  if (!skipSessionRestore && auth.status === 'restoring') {
    return (
      <main className="auth-restore" role="status" aria-live="polite">
        <span className="auth-restore-spinner" aria-hidden="true" />
        <p>正在恢复安全会话…</p>
      </main>
    )
  }

  return (
    <Routes>
      <Route
        path="/login"
        element={
          <AnonymousEntry status={auth.status}>
            <LoginPage />
          </AnonymousEntry>
        }
      />
      <Route
        path="/register"
        element={
          <AnonymousEntry status={auth.status}>
            <RegisterPage />
          </AnonymousEntry>
        }
      />
      <Route path="/verify-email" element={<VerifyEmailPage />} />
      <Route
        path="/forgot-password"
        element={
          <AnonymousEntry status={auth.status}>
            <ForgotPasswordPage />
          </AnonymousEntry>
        }
      />
      <Route path="/reset-password" element={<ResetPasswordPage />} />
      <Route path="/shared/plans/:token" element={<SharedPlanPage />} />
      <Route path="/privacy" element={<PrivacyPage />} />
      <Route path="/" element={<RootEntry status={auth.status} />} />
      <Route
        element={
          <RequireAuth status={auth.status}>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route path="/dashboard" element={<DashboardPage />} />
        <Route path="/customers" element={<CustomersPage />} />
        <Route path="/customers/new" element={<CustomerNewPage />} />
        <Route path="/customers/:id" element={<CustomerDetailPage />} />
        <Route path="/orders" element={<OrdersPage />} />
        <Route element={<CreativeLayout />}>
          <Route path="/creative" element={<CreativeHomePage />} />
          <Route path="/creative/projects" element={<CreativeProjectsPage />} />
          <Route path="/creative/library" element={<CreativeAssetsPage />} />
          <Route path="/creative/agents" element={<CreativeAgentsPage />} />
        </Route>
        <Route path="/creative/canvases/:canvasID" element={<Suspense key="creative-canvas" fallback={<CanvasLoading />}><CreativeCanvasPage /></Suspense>} />
        <Route path="/calendar" element={<CalendarPage />} />
        <Route path="/shoot-plans" element={<ShootPlansPage />} />
        <Route path="/shoot-plans/:id" element={<ShootPlanWorkspacePage />} />
        <Route path="/shoot-plans/:id/ingestions/:sessionId" element={<ShootPlanIngestionPage />} />
        <Route path="/packages" element={<PackagesPage />} />
        <Route path="/reminders" element={<RemindersPage />} />
        <Route path="/settings" element={<Navigate to="/account/settings" replace />} />
        <Route path="/change-password" element={<Navigate to="/account/security/password" replace />} />
        <Route path="/account" element={<AccountCenterLayout />}>
          <Route index element={<AccountOverviewPage />} />
          <Route path="profile" element={<AccountProfilePage />} />
          <Route path="security" element={<AccountSecurityPage />} />
          <Route path="security/password" element={<AccountPasswordPage />} />
          <Route path="settings" element={<AccountSettingsPage />} />
        </Route>
      </Route>
      <Route path="/oauth/consent" element={<Suspense key="oauth-consent" fallback={<p role="status">正在打开授权页…</p>}><AgentConsentPage /></Suspense>} />
      <Route path="/shoot-plans/:id/run" element={<RequireAuth status={auth.status}><ShootPlanRunPage /></RequireAuth>} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
