import { useEffect, useSyncExternalStore } from 'react'
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
  const skipSessionRestore = isActionTokenRoute || isPublicShareRoute

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
      <Route path="/shoot-plans/:id/run" element={<RequireAuth status={auth.status}><ShootPlanRunPage /></RequireAuth>} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
