import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import DataExportCard from '../components/DataExportCard.tsx'
import { logoutSession } from '../auth/session.ts'
import { useAccountCenter } from './AccountCenterContext.tsx'
import {
	BOUNDARY_SECTION,
	IDENTITY_SECTION,
	SECURITY_SECTION,
} from './security/securityCopy.ts'

export default function AccountSecurityPage() {
	const { account } = useAccountCenter()
	const navigate = useNavigate()
	const [loggingOut, setLoggingOut] = useState(false)

	async function onLogout() {
		if (loggingOut) return
		setLoggingOut(true)
		try {
			await logoutSession()
		} finally {
			navigate('/login', { replace: true })
			setLoggingOut(false)
		}
	}

	return (
		<main className="content settings-stack account-security">
			<section className="card form-stack" aria-labelledby="accountIdentityTitle">
				<div>
					<h2 id="accountIdentityTitle">{IDENTITY_SECTION.title}</h2>
					<p className="sub">{IDENTITY_SECTION.subtitle}</p>
				</div>
				<dl className="account-readonly-meta">
					<dt>{IDENTITY_SECTION.emailLabel}</dt>
					<dd>{account.email}</dd>
					<dt>{IDENTITY_SECTION.verifiedLabel}</dt>
					<dd>
						<span className="badge badge-success">{IDENTITY_SECTION.verifiedValue}</span>
					</dd>
				</dl>
			</section>

			<section className="card account-security-card" aria-labelledby="accountSecurityTitle">
				<div>
					<h2 id="accountSecurityTitle">{SECURITY_SECTION.title}</h2>
					<p className="sub">{SECURITY_SECTION.subtitle}</p>
				</div>
				<div className="topbar-actions">
					<Link className="btn" to="/account/security/password">
						{SECURITY_SECTION.changePassword}
					</Link>
					<button
						className="btn btn-danger-ghost"
						type="button"
						disabled={loggingOut}
						onClick={() => void onLogout()}
					>
						{loggingOut ? SECURITY_SECTION.loggingOut : SECURITY_SECTION.logout}
					</button>
				</div>
			</section>

			<DataExportCard onUnauthorized={() => navigate('/login', { replace: true })} />

			<section className="card form-stack account-security-boundary" aria-labelledby="accountBoundaryTitle">
				<div>
					<h2 id="accountBoundaryTitle">{BOUNDARY_SECTION.title}</h2>
					<p className="sub">{BOUNDARY_SECTION.body}</p>
				</div>
			</section>
		</main>
	)
}
