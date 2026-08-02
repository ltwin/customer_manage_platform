import { Link, useNavigate } from 'react-router-dom'
import ChangePasswordForm from './ChangePasswordForm.tsx'
import { PASSWORD_CHANGED_NOTICE } from './security/loginNotice.ts'
import { PASSWORD_PAGE } from './security/securityCopy.ts'
import '../pages/auth/LoginPage.css'

export default function AccountPasswordPage() {
	const navigate = useNavigate()

	return (
		<main className="content">
			<section className="card form-stack auth-card-inline">
				<div>
					<h2>{PASSWORD_PAGE.title}</h2>
					<p className="sub">{PASSWORD_PAGE.subtitle}</p>
				</div>
				<ChangePasswordForm
					onSuccess={() => {
						navigate('/login', { replace: true, state: { notice: PASSWORD_CHANGED_NOTICE } })
					}}
					footer={(
						<p className="auth-foot">
							<Link to="/account/security">{PASSWORD_PAGE.backToSecurity}</Link>
						</p>
					)}
				/>
			</section>
		</main>
	)
}
