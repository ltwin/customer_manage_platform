import { useEffect, useRef, useState } from 'react'
import { acquireAvatarMedia, firstGrapheme } from './customerAvatarMedia'
import './CustomerAvatar.css'

type Props = {
	customerId: string
	displayName: string
	avatarRevision: string
	avatarUrl?: string
	size?: 'sm' | 'md' | 'lg'
	decorative?: boolean
}

export default function CustomerAvatar({
	displayName,
	avatarRevision,
	avatarUrl,
	size = 'md',
	decorative = false,
}: Props) {
	const rootRef = useRef<HTMLSpanElement>(null)
	const [visible, setVisible] = useState(false)
	const [objectURL, setObjectURL] = useState<string | null>(null)
	const [failed, setFailed] = useState(false)

	useEffect(() => {
		const node = rootRef.current
		if (!node || typeof IntersectionObserver === 'undefined') {
			setVisible(true)
			return
		}
		const observer = new IntersectionObserver(([entry]) => {
			if (entry?.isIntersecting) {
				setVisible(true)
				observer.disconnect()
			}
		})
		observer.observe(node)
		return () => observer.disconnect()
	}, [])

	useEffect(() => {
		setObjectURL(null)
		setFailed(false)
		if (!visible || !avatarUrl) return
		const handle = acquireAvatarMedia(avatarUrl, avatarRevision)
		let active = true
		handle.url.then((url) => {
			if (active) setObjectURL(url)
		}).catch(() => {
			if (active) setFailed(true)
		})
		return () => {
			active = false
			handle.release()
		}
	}, [avatarRevision, avatarUrl, visible])

	const label = decorative ? undefined : `${displayName}的头像`
	return (
		<span
			ref={rootRef}
			className={`avatar customer-avatar customer-avatar-${size} ${fallbackTone(displayName)}`}
			aria-hidden={decorative || undefined}
			role={decorative ? undefined : 'img'}
			aria-label={label}
		>
			{objectURL && !failed ? (
				<img src={objectURL} alt="" onError={() => setFailed(true)} />
			) : firstGrapheme(displayName)}
		</span>
	)
}

function fallbackTone(value: string): string {
	let hash = 0
	for (const character of value) hash = (hash * 31 + (character.codePointAt(0) ?? 0)) >>> 0
	return `a${(hash % 4) + 2}`
}
