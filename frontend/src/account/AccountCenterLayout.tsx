import { NavLink, Outlet, useOutletContext } from 'react-router-dom'
import type { ShellContext } from '../components/shellContext.ts'
import './account-center.css'

const nav = [
	{ to: '/account', label: '总览', end: true },
	{ to: '/account/profile', label: '用户资料', end: false },
	{ to: '/account/security', label: '隐私与安全', end: false },
	{ to: '/account/settings', label: '系统设置', end: false },
]

export default function AccountCenterLayout() {
	const shell = useOutletContext<ShellContext>()

	return (
		<div className="account-center">
			<header className="topbar">
				<div>
					<h1>用户中心</h1>
					<p className="sub">资料、隐私与安全、系统设置</p>
				</div>
			</header>
			<nav className="account-center-nav" aria-label="用户中心">
				{nav.map((item) => (
					<NavLink
						key={item.to}
						to={item.to}
						end={item.end}
						className={({ isActive }) => (isActive ? 'account-center-nav-link active' : 'account-center-nav-link')}
					>
						{item.label}
					</NavLink>
				))}
			</nav>
			<div className="account-center-body">
				<Outlet context={shell} />
			</div>
		</div>
	)
}
