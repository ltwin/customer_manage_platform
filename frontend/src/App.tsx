import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useSyncExternalStore } from 'react'
import type { ReactElement } from 'react'
import LoginPage from './pages/LoginPage'
import AppShell from './components/AppShell'
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
import { getToken, getTokenSnapshot, subscribeToken } from './auth/token'

// 守卫路由骨架：后续域 feature 的受保护页面都挂在 RequireAuth 之下（design 2.2 扩展点）
function RequireAuth({ children }: { children: ReactElement }) {
  const location = useLocation()
	useSyncExternalStore(subscribeToken, getTokenSnapshot)
  if (!getToken()) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  return children
}

// 根路径按登录态分流：未登录看欢迎页，已登录直接进经营台
function RootEntry() {
  useSyncExternalStore(subscribeToken, getTokenSnapshot)
  if (getToken()) {
    return <Navigate to="/dashboard" replace />
  }
  return <WelcomePage />
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/" element={<RootEntry />} />
      <Route
        element={
          <RequireAuth>
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
