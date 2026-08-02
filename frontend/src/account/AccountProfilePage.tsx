import { useEffect, useRef, useState, type FormEvent } from 'react'
import {
	ApiError,
	deleteAccountProfileAvatar,
	patchAccountProfile,
	putAccountProfileAvatar,
} from '../api/client.ts'
import { useAccountCenter } from './AccountCenterContext.tsx'
import {
	acquireAccountAvatarMedia,
	avatarToneFromAccountID,
	fallbackAvatarLabel,
} from './accountAvatarMedia.ts'

export default function AccountProfilePage() {
	const { account, profile, notify, replaceProfile, refreshProfile } = useAccountCenter()
	const data = profile.kind === 'ready' ? profile.data : null
	const [displayName, setDisplayName] = useState('')
	const [nameDirty, setNameDirty] = useState(false)
	const [savingName, setSavingName] = useState(false)
	const [savingAvatar, setSavingAvatar] = useState(false)
	const [error, setError] = useState<string | null>(null)
	const [avatarURL, setAvatarURL] = useState<string | null>(null)
	const fileInputRef = useRef<HTMLInputElement>(null)
	const label = fallbackAvatarLabel(data?.display_name, account.email)
	const tone = avatarToneFromAccountID(account.id)

	useEffect(() => {
		// A4b：409 重读后保留未提交本地输入，不自动覆盖服务端新版本进表单。
		if (data && !nameDirty) setDisplayName(data.display_name ?? '')
	}, [data, nameDirty])

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

	async function onSaveName(event: FormEvent) {
		event.preventDefault()
		if (!data || savingName) return
		setError(null)
		setSavingName(true)
		try {
			const trimmed = displayName.trim()
			const next = await patchAccountProfile(data.profile_revision, trimmed === '' ? null : trimmed)
			setNameDirty(false)
			replaceProfile(next)
			notify(trimmed === '' ? '已清除展示名称' : '展示名称已保存')
		} catch (err) {
			if (err instanceof ApiError && err.status === 409) {
				setError('资料已被其他操作更新，请用当前修订重试。本地未提交输入已保留。')
				refreshProfile()
			} else {
				setError(err instanceof Error ? err.message : '保存失败')
			}
		} finally {
			setSavingName(false)
		}
	}

	async function onUploadAvatar(file: File | undefined) {
		if (!data || !file || savingAvatar) return
		setError(null)
		setSavingAvatar(true)
		try {
			const next = await putAccountProfileAvatar(file, data.avatar_revision)
			replaceProfile(next)
			notify('头像已更新')
		} catch (err) {
			if (err instanceof ApiError && err.status === 409) {
				setError('头像已被其他操作更新，正在刷新…')
				refreshProfile()
			} else {
				setError(err instanceof Error ? err.message : '头像上传失败')
			}
		} finally {
			setSavingAvatar(false)
			if (fileInputRef.current) fileInputRef.current.value = ''
		}
	}

	async function onRemoveAvatar() {
		if (!data || savingAvatar) return
		setError(null)
		setSavingAvatar(true)
		try {
			const next = await deleteAccountProfileAvatar(data.avatar_revision)
			replaceProfile(next)
			notify('头像已移除')
		} catch (err) {
			if (err instanceof ApiError && err.status === 409) {
				setError('头像已被其他操作更新，正在刷新…')
				refreshProfile()
			} else {
				setError(err instanceof Error ? err.message : '移除头像失败')
			}
		} finally {
			setSavingAvatar(false)
		}
	}

	if (profile.kind === 'loading') {
		return (
			<main className="content">
				<p className="sub">正在加载用户资料…</p>
			</main>
		)
	}

	if (profile.kind === 'error') {
		return (
			<main className="content">
				<div className="form-error" role="alert">
					资料加载失败。
					<button className="btn" type="button" onClick={profile.retry}>重试</button>
				</div>
			</main>
		)
	}

	return (
		<main className="content settings-stack">
			{profile.freshness === 'stale' && (
				<div className="form-error" role="status">{profile.refreshError}</div>
			)}
			<section className="card form-stack">
				<h2>头像</h2>
				<div className="account-profile-avatar-row">
					<span className={`account-profile-avatar-preview tone-${tone}`} aria-hidden="true">
						{avatarURL ? <img src={avatarURL} alt="" /> : label}
					</span>
					<div className="topbar-actions">
						<button
							className="btn btn-primary"
							type="button"
							disabled={savingAvatar}
							onClick={() => fileInputRef.current?.click()}
						>
							{savingAvatar ? '处理中…' : data.avatar_url ? '替换头像' : '上传头像'}
						</button>
						{data.avatar_url && (
							<button className="btn" type="button" disabled={savingAvatar} onClick={() => void onRemoveAvatar()}>
								移除头像
							</button>
						)}
					</div>
				</div>
				<input
					ref={fileInputRef}
					type="file"
					accept="image/jpeg,image/png,image/webp"
					hidden
					onChange={(event) => void onUploadAvatar(event.target.files?.[0])}
				/>
				<p className="sub">支持 JPEG／PNG／WebP 原图；不做客户侧 PNG 归一化。</p>
			</section>

			<form className="card form-stack" onSubmit={onSaveName}>
				<h2>展示名称</h2>
				<label>
					展示名称（可留空清除）
					<input
						value={displayName}
						onChange={(event) => {
							setDisplayName(event.target.value)
							setNameDirty(true)
						}}
						maxLength={80}
						disabled={savingName}
					/>
				</label>
				<button className="btn btn-primary" type="submit" disabled={savingName || profile.freshness === 'stale'}>
					{savingName ? '保存中…' : '保存展示名称'}
				</button>
			</form>

			<section className="card">
				<h2>只读身份</h2>
				<dl className="account-readonly-meta">
					<dt>登录邮箱</dt>
					<dd>{account.email}</dd>
					<dt>创建时间</dt>
					<dd>{new Date(account.created_at).toLocaleString()}</dd>
				</dl>
			</section>

			{error && (
				<div className="form-error" role="alert">
					{error}
				</div>
			)}
		</main>
	)
}
