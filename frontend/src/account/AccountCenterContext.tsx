import {
	createContext,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useState,
	type ReactNode,
} from 'react'
import { useSyncExternalStore } from 'react'
import { ApiError, fetchAccountProfile, type AccountProfile } from '../api/client.ts'
import { getAuthSnapshot, subscribeAuth, type Me } from '../auth/session.ts'
import { clearAccountAvatarMediaCache } from './accountAvatarMedia.ts'

/** 直接消费 auth snapshot.account；禁止第二套收窄／复制 parseAccount。 */
export type AuthenticatedAccount = Me

export type ProfileReadState =
	| { kind: 'loading' }
	| { kind: 'ready'; data: AccountProfile; freshness: 'current' }
	| { kind: 'ready'; data: AccountProfile; freshness: 'stale'; refreshError: string }
	| { kind: 'error'; retry: () => void }

export type AccountCenterValue = {
	account: AuthenticatedAccount
	profile: ProfileReadState
	theme: 'light' | 'dark'
	refreshProfile(): void
	setTheme(theme: 'light' | 'dark'): void
	notify(message: string): void
	replaceProfile(data: AccountProfile): void
}

const AccountCenterContext = createContext<AccountCenterValue | null>(null)

type Props = {
	theme: 'light' | 'dark'
	setTheme(theme: 'light' | 'dark'): void
	notify(message: string): void
	children: ReactNode
}

export function AccountCenterProvider({ theme, setTheme, notify, children }: Props) {
	const auth = useSyncExternalStore(subscribeAuth, getAuthSnapshot)
	const account = auth.status === 'authenticated' ? auth.account : null
	const [profile, setProfile] = useState<ProfileReadState>({ kind: 'loading' })
	const [reloadTick, setReloadTick] = useState(0)

	const refreshProfile = useCallback(() => {
		setReloadTick((value) => value + 1)
	}, [])

	const replaceProfile = useCallback((data: AccountProfile) => {
		setProfile({ kind: 'ready', data, freshness: 'current' })
	}, [])

	useEffect(() => {
		if (!account) {
			setProfile({ kind: 'loading' })
			clearAccountAvatarMediaCache()
			return
		}
		let active = true
		setProfile((current) => {
			if (current.kind === 'ready') {
				return { kind: 'ready', data: current.data, freshness: 'stale', refreshError: '正在刷新资料…' }
			}
			return { kind: 'loading' }
		})
		fetchAccountProfile()
			.then((data) => {
				if (!active) return
				setProfile({ kind: 'ready', data, freshness: 'current' })
			})
			.catch((error: unknown) => {
				if (!active) return
				if (error instanceof ApiError && error.status === 401) {
					setProfile({ kind: 'error', retry: refreshProfile })
					return
				}
				setProfile((current) => {
					if (current.kind === 'ready') {
						return {
							kind: 'ready',
							data: current.data,
							freshness: 'stale',
							refreshError: error instanceof Error ? error.message : '资料加载失败',
						}
					}
					return { kind: 'error', retry: refreshProfile }
				})
			})
		return () => {
			active = false
		}
	}, [account, auth.generation, refreshProfile, reloadTick])

	const value = useMemo<AccountCenterValue | null>(() => {
		if (!account) return null
		return {
			account,
			profile,
			theme,
			refreshProfile,
			setTheme,
			notify,
			replaceProfile,
		}
	}, [account, profile, theme, refreshProfile, setTheme, notify, replaceProfile])

	if (!value) return children

	return <AccountCenterContext.Provider value={value}>{children}</AccountCenterContext.Provider>
}

export function useAccountCenter(): AccountCenterValue {
	const value = useContext(AccountCenterContext)
	if (!value) {
		throw new Error('useAccountCenter must be used within AccountCenterProvider')
	}
	return value
}

export function useOptionalAccountCenter(): AccountCenterValue | null {
	return useContext(AccountCenterContext)
}
