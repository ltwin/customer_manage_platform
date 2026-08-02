import { fetchAvatarBlob } from '../api/client.ts'
import { getAuthGeneration, subscribeAuth } from '../auth/session.ts'

type Entry = {
	refs: number
	controller: AbortController
	promise: Promise<string>
	objectURL?: string
}

export type AccountAvatarMediaHandle = {
	url: Promise<string>
	release(): void
}

const entries = new Map<string, Entry>()

export function acquireAccountAvatarMedia(
	avatarURL: string,
	avatarVersion: string,
): AccountAvatarMediaHandle {
	const key = `${getAuthGeneration()}\u0000${avatarURL}\u0000${avatarVersion}`
	let entry = entries.get(key)
	if (!entry) {
		const controller = new AbortController()
		entry = {
			refs: 0,
			controller,
			promise: fetchAvatarBlob(avatarURL, controller.signal).then((blob) => {
				const objectURL = URL.createObjectURL(blob)
				const current = entries.get(key)
				if (!current || current.refs === 0) {
					URL.revokeObjectURL(objectURL)
					throw new DOMException('Account avatar media released', 'AbortError')
				}
				current.objectURL = objectURL
				return objectURL
			}).catch((error: unknown) => {
				entries.delete(key)
				throw error
			}),
		}
		entries.set(key, entry)
	}
	entry.refs += 1
	let released = false
	return {
		url: entry.promise,
		release() {
			if (released) return
			released = true
			const current = entries.get(key)
			if (!current) return
			current.refs -= 1
			if (current.refs > 0) return
			current.controller.abort()
			if (current.objectURL) URL.revokeObjectURL(current.objectURL)
			entries.delete(key)
		},
	}
}

export function clearAccountAvatarMediaCache(): void {
	for (const entry of entries.values()) {
		entry.controller.abort()
		if (entry.objectURL) URL.revokeObjectURL(entry.objectURL)
	}
	entries.clear()
}

subscribeAuth(clearAccountAvatarMediaCache)

export function firstAccountGrapheme(value: string): string {
	const normalized = value.trim()
	if (!normalized) return '影'
	const segmenter = new Intl.Segmenter('zh-CN', { granularity: 'grapheme' })
	return segmenter.segment(normalized)[Symbol.iterator]().next().value?.segment ?? '影'
}

export function fallbackAvatarLabel(displayName: string | null | undefined, email: string): string {
	if (displayName && displayName.trim()) return firstAccountGrapheme(displayName)
	if (email.trim()) return firstAccountGrapheme(email)
	return '影'
}

export function avatarToneFromAccountID(accountID: string): number {
	let hash = 0
	for (let i = 0; i < accountID.length; i += 1) {
		hash = (hash * 31 + accountID.charCodeAt(i)) >>> 0
	}
	return hash % 6
}
