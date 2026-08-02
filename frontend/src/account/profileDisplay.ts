import type { AccountProfile } from '../api/client.ts'
import { fallbackAvatarLabel } from './accountAvatarMedia.ts'

/** 全局入口与资料页共用的展示快照；mutation 后应以服务端 profile 原子替换。 */
export type ProfileDisplaySnapshot = {
	displayName: string | null
	avatarVersion: string | null
	avatarURL: string | null
	fallbackLabel: string
}

export function profileDisplaySnapshot(
	profile: AccountProfile,
	email: string,
): ProfileDisplaySnapshot {
	return {
		displayName: profile.display_name?.trim() ? profile.display_name.trim() : null,
		avatarVersion: profile.avatar_version ?? null,
		avatarURL: profile.avatar_url ?? null,
		fallbackLabel: fallbackAvatarLabel(profile.display_name, email),
	}
}

export function applyProfileMutation(
	_previous: ProfileDisplaySnapshot,
	next: AccountProfile,
	email: string,
): ProfileDisplaySnapshot {
	return profileDisplaySnapshot(next, email)
}
