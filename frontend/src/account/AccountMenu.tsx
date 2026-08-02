import { useEffect, useId, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { logoutSession } from '../auth/session.ts'
import { useAccountCenter } from './AccountCenterContext.tsx'
import {
	acquireAccountAvatarMedia,
	avatarToneFromAccountID,
	fallbackAvatarLabel,
} from './accountAvatarMedia.ts'
import { ACCOUNT_MENU_ITEMS, nextMenuIndex } from './accountMenuModel.ts'
import './account-menu.css'

type Props = {
	slot: 'desktop' | 'mobile'
}

function menuFocusables(root: HTMLElement | null): HTMLElement[] {
	if (!root) return []
	return Array.from(root.querySelectorAll<HTMLElement>('[role="menuitem"], [role="menuitemcheckbox"]'))
}

export default function AccountMenu({ slot }: Props) {
	const { account, profile, theme, setTheme, notify } = useAccountCenter()
	const navigate = useNavigate()
	const location = useLocation()
	const [open, setOpen] = useState(false)
	const [loggingOut, setLoggingOut] = useState(false)
	const [avatarURL, setAvatarURL] = useState<string | null>(null)
	const triggerRef = useRef<HTMLButtonElement>(null)
	const menuRef = useRef<HTMLDivElement>(null)
	const menuId = useId()
	const profileData = profile.kind === 'ready' ? profile.data : null
	const label = fallbackAvatarLabel(profileData?.display_name, account.email)
	const tone = avatarToneFromAccountID(account.id)

	useEffect(() => {
		setOpen(false)
	}, [location.pathname, account.id])

	useEffect(() => {
		if (!profileData?.avatar_url || !profileData.avatar_version) {
			setAvatarURL(null)
			return
		}
		const handle = acquireAccountAvatarMedia(profileData.avatar_url, profileData.avatar_version)
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
	}, [profileData?.avatar_url, profileData?.avatar_version])

	useEffect(() => {
		if (!open) return
		const onKey = (event: KeyboardEvent) => {
			if (event.key === 'Escape') {
				event.preventDefault()
				setOpen(false)
				triggerRef.current?.focus()
			}
		}
		const onPointer = (event: MouseEvent) => {
			const target = event.target as Node
			if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return
			setOpen(false)
		}
		window.addEventListener('keydown', onKey)
		window.addEventListener('mousedown', onPointer)
		return () => {
			window.removeEventListener('keydown', onKey)
			window.removeEventListener('mousedown', onPointer)
		}
	}, [open])

	useEffect(() => {
		if (!open) return
		const first = menuFocusables(menuRef.current)[0]
		first?.focus()
	}, [open])

	function moveMenuFocus(delta: number) {
		const items = menuFocusables(menuRef.current)
		if (items.length === 0) return
		const current = items.findIndex((item) => item === document.activeElement)
		const next = nextMenuIndex(current < 0 ? 0 : current, delta, items.length)
		items[next]?.focus()
	}

	function onMenuKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
		switch (event.key) {
			case 'ArrowDown':
				event.preventDefault()
				moveMenuFocus(1)
				break
			case 'ArrowUp':
				event.preventDefault()
				moveMenuFocus(-1)
				break
			case 'Home': {
				event.preventDefault()
				const items = menuFocusables(menuRef.current)
				items[0]?.focus()
				break
			}
			case 'End': {
				event.preventDefault()
				const items = menuFocusables(menuRef.current)
				items[items.length - 1]?.focus()
				break
			}
			default:
				break
		}
	}

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
		<div className={`account-menu-slot account-menu-slot-${slot}`} data-account-menu-slot={slot}>
			<button
				ref={triggerRef}
				type="button"
				className="account-menu-trigger"
				aria-haspopup="menu"
				aria-expanded={open}
				aria-controls={menuId}
				onClick={() => setOpen((value) => !value)}
			>
				<span className={`account-avatar tone-${tone}`} aria-hidden="true">
					{avatarURL ? <img src={avatarURL} alt="" /> : label}
				</span>
				<span className="account-menu-trigger-text">
					<span className="account-menu-name">{profileData?.display_name?.trim() || label}</span>
					<span className="account-menu-email">{account.email}</span>
				</span>
			</button>
			{open && (
				<div
					ref={menuRef}
					id={menuId}
					className="account-menu-panel"
					role="menu"
					aria-label="账号菜单"
					onKeyDown={onMenuKeyDown}
				>
					<Link
						className="account-menu-identity"
						role="menuitem"
						to="/account"
						onClick={() => setOpen(false)}
					>
						<span className={`account-avatar tone-${tone}`} aria-hidden="true">
							{avatarURL ? <img src={avatarURL} alt="" /> : label}
						</span>
						<span>
							<strong>{profileData?.display_name?.trim() || '用户中心'}</strong>
							<small>{account.email}</small>
						</span>
					</Link>
					{ACCOUNT_MENU_ITEMS.filter((item) => item.kind !== 'link' || item.to !== '/account').map((item) => {
						if (item.kind === 'link') {
							return (
								<Link
									key={item.id}
									role="menuitem"
									className="account-menu-item"
									to={item.to}
									onClick={() => setOpen(false)}
								>
									{item.label}
								</Link>
							)
						}
						if (item.kind === 'theme') {
							const checked = theme === 'dark'
							return (
								<button
									key={item.id}
									type="button"
									role="menuitemcheckbox"
									aria-checked={checked}
									className="account-menu-item"
									onClick={() => {
										setTheme(checked ? 'light' : 'dark')
										notify(checked ? '已切换到浅色模式' : '已切换到深色模式')
									}}
								>
									{item.label}
								</button>
							)
						}
						return (
							<button
								key={item.id}
								type="button"
								role="menuitem"
								className="account-menu-item account-menu-danger"
								disabled={loggingOut}
								onClick={() => {
									void onLogout()
								}}
							>
								{loggingOut ? '正在退出…' : item.label}
							</button>
						)
					})}
				</div>
			)}
		</div>
	)
}
