import { Link } from 'react-router-dom'
import { useAccountCenter } from './AccountCenterContext.tsx'
import {
	acquireAccountAvatarMedia,
	avatarToneFromAccountID,
	fallbackAvatarLabel,
} from './accountAvatarMedia.ts'
import { useEffect, useState } from 'react'

export default function AccountOverviewPage() {
	const { account, profile } = useAccountCenter()
	const data = profile.kind === 'ready' ? profile.data : null
	const label = fallbackAvatarLabel(data?.display_name, account.email)
	const tone = avatarToneFromAccountID(account.id)
	const [avatarURL, setAvatarURL] = useState<string | null>(null)

	useEffect(() => {
		if (!data?.avatar_url || !data.avatar_version) {
			setAvatarURL(null)
			return
		}
		const handle = acquireAccountAvatarMedia(data.avatar_url, data.avatar_version)
		let active = true
		handle.url.then((url) => {
			if (active) setAvatarURL(url)
		}).catch(() => {
			if (active) setAvatarURL(null)
		})
		return () => {
			active = false
			handle.release()
		}
	}, [data?.avatar_url, data?.avatar_version])

	return (
		<main className="content">
			{profile.kind === 'error' && (
				<div className="form-error" role="alert">
					资料加载失败。
					<button className="btn" type="button" onClick={profile.retry}>重试</button>
				</div>
			)}
			{profile.kind === 'ready' && profile.freshness === 'stale' && (
				<div className="form-error" role="status">{profile.refreshError}</div>
			)}
			<section className="account-overview-grid">
				<article className="card account-overview-card">
					<div className="account-profile-avatar-row">
						<span className={`account-profile-avatar-preview tone-${tone}`} aria-hidden="true">
							{avatarURL ? <img src={avatarURL} alt="" /> : label}
						</span>
						<div>
							<h2>{data?.display_name?.trim() || '用户资料'}</h2>
							<p className="sub">{account.email}</p>
						</div>
					</div>
					<p className="sub">维护展示名称与头像；全局入口会立即跟随服务端版本。</p>
					<Link className="btn btn-primary" to="/account/profile">编辑用户资料</Link>
				</article>
				<article className="card account-overview-card">
					<h2>隐私与安全</h2>
					<p className="sub">修改密码、退出当前登录、导出全部 JSON 数据。</p>
					<Link className="btn" to="/account/security">打开隐私与安全</Link>
				</article>
				<article className="card account-overview-card">
					<h2>系统设置</h2>
					<p className="sub">时区、可约时段、提醒参数与 Telegram 摘要。</p>
					<Link className="btn" to="/account/settings">打开系统设置</Link>
				</article>
			</section>
		</main>
	)
}
