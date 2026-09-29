import '../../src/index.css'
import { createRoot } from 'react-dom/client'
import { useState } from 'react'
import { AccountCenterProvider, useAccountCenter } from '../../src/account/AccountCenterContext.tsx'
import AccountProfilePage from '../../src/account/AccountProfilePage.tsx'
import { setAuthenticated } from '../../src/auth/session.ts'
import WorkspacePage from '../../src/creative-canvas/editor/WorkspacePage.tsx'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

setAuthenticated({ access_token: 'fixture', token_type: 'Bearer', expires_in: 600 }, {
  id: 'quality-account', email: 'quality@example.invalid', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai',
})
export function ProfileObserver() {
  const { profile } = useAccountCenter()
  return <output data-testid="profile">{profile.kind === 'ready' ? JSON.stringify(profile.data) : profile.kind}</output>
}
export function ProfileHarness() {
  const [theme, setTheme] = useState<'light' | 'dark'>('light')
  return <AccountCenterProvider theme={theme} setTheme={setTheme} notify={() => {}}>
    <ProfileObserver /><AccountProfilePage />
  </AccountCenterProvider>
}
const canvas = new URLSearchParams(location.search).has('canvas')
createRoot(document.getElementById('root')!).render(canvas
  ? <MemoryRouter initialEntries={['/canvases/test-canvas']}><Routes><Route path="/canvases/:canvasID" element={<WorkspacePage />} /></Routes></MemoryRouter>
  : <ProfileHarness />)
