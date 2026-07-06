import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import type { ReactElement } from 'react'
import LoginPage from './pages/LoginPage'
import HomePage from './pages/HomePage'
import { getToken } from './auth/token'

// 守卫路由骨架：后续域 feature 的受保护页面都挂在 RequireAuth 之下（design 2.2 扩展点）
function RequireAuth({ children }: { children: ReactElement }) {
  const location = useLocation()
  if (!getToken()) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  return children
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <HomePage />
          </RequireAuth>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
