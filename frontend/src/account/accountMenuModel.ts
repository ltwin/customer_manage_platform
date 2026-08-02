export type AccountMenuItem =
	| { kind: 'link'; id: string; label: string; to: string }
	| { kind: 'theme'; id: string; label: string }
	| { kind: 'logout'; id: string; label: string }

/** 六动作：身份摘要(/account)＋资料＋安全＋设置＋主题＋退出。 */
export const ACCOUNT_MENU_ITEMS: AccountMenuItem[] = [
	{ kind: 'link', id: 'overview', label: '用户中心', to: '/account' },
	{ kind: 'link', id: 'profile', label: '用户资料', to: '/account/profile' },
	{ kind: 'link', id: 'security', label: '隐私与安全', to: '/account/security' },
	{ kind: 'link', id: 'settings', label: '系统设置', to: '/account/settings' },
	{ kind: 'theme', id: 'theme', label: '切换浅色／深色' },
	{ kind: 'logout', id: 'logout', label: '退出登录' },
]

export function nextMenuIndex(current: number, delta: number, length: number): number {
	if (length <= 0) return 0
	return (current + delta + length) % length
}

export function menuItemFocusTargets(open: boolean): string[] {
	return open ? ACCOUNT_MENU_ITEMS.map((item) => item.id) : []
}
