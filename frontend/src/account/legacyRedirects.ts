/** 退役账号深链的固定 replace 目标（search/hash 由 Navigate 丢弃）。 */
export const LEGACY_ACCOUNT_REDIRECTS: ReadonlyArray<{ from: string; to: string }> = [
	{ from: '/settings', to: '/account/settings' },
	{ from: '/change-password', to: '/account/security/password' },
]

export function legacyAccountRedirectPath(pathname: string): string | null {
	const hit = LEGACY_ACCOUNT_REDIRECTS.find((entry) => entry.from === pathname)
	return hit?.to ?? null
}
