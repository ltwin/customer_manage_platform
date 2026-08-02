/** 改密成功后登录页须实际渲染的 notice（roadmap §4.8 / design D4）。 */
export const PASSWORD_CHANGED_NOTICE = '密码已修改，所有设备需要重新登录。'

let pendingLoginNotice: string | null = null

/** 在 setAnonymous 之前排队，避免 RequireAuth Navigate 冲掉 location.state.notice。 */
export function queueLoginNotice(notice: string): void {
	pendingLoginNotice = notice
}

export function peekLoginNotice(): string | null {
	return pendingLoginNotice
}

export function takeLoginNotice(): string | null {
	const notice = pendingLoginNotice
	pendingLoginNotice = null
	return notice
}
