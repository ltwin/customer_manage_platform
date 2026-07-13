import { fetchAvatarBlob } from '../../api/client.ts'
import { getAuthGeneration, subscribeToken } from '../../auth/token.ts'

type Entry = {
	refs: number
	controller: AbortController
	promise: Promise<string>
	objectURL?: string
}

export type AvatarMediaHandle = {
	url: Promise<string>
	release(): void
}

const entries = new Map<string, Entry>()

export function acquireAvatarMedia(avatarURL: string, avatarRevision: string): AvatarMediaHandle {
	const key = `${getAuthGeneration()}\u0000${avatarURL}\u0000${avatarRevision}`
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
					throw new DOMException('Avatar media released', 'AbortError')
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

export function clearAvatarMediaCache(): void {
	for (const entry of entries.values()) {
		entry.controller.abort()
		if (entry.objectURL) URL.revokeObjectURL(entry.objectURL)
	}
	entries.clear()
}

subscribeToken(clearAvatarMediaCache)

export function firstGrapheme(value: string): string {
	const normalized = value.trim()
	if (!normalized) return '客'
	const segmenter = new Intl.Segmenter('zh-CN', { granularity: 'grapheme' })
	return segmenter.segment(normalized)[Symbol.iterator]().next().value?.segment ?? '客'
}
