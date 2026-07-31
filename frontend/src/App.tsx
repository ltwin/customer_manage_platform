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
import SettingsPage from './pages/SettingsPage'
import WelcomePage from './pages/WelcomePage'
import LoginPage from './pages/auth/LoginPage'
import RegisterPage from './pages/auth/RegisterPage'
import VerifyEmailPage from './pages/auth/VerifyEmailPage'

type AuthStatus = ReturnType<typeof getAuthSnapshot>['status']

function RequireAuth({ children, status }: { children: ReactElement; status: AuthStatus }) {
  const location = useLocation()
  if (status !== 'authenticated') {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
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
  const isVerificationRoute = location.pathname === '/verify-email'

  useEffect(() => {
    if (!isVerificationRoute && auth.status === 'restoring') void restoreSession()
  }, [auth.status, isVerificationRoute])

  if (!isVerificationRoute && auth.status === 'restoring') {
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
        <Route path="/packages" element={<PackagesPage />} />
        <Route path="/reminders" element={<RemindersPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
