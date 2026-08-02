import type { Settings, UpdateSettingsBody } from '../../api/client.ts'
import type { PatchSectionId, SettingsControllerState } from './settingsControllerKernel.ts'

export type SettingsSaveJob = {
	section: PatchSectionId
}

export type SettingsSaveQueueHandlers = {
	getState(): SettingsControllerState
	dispatchSaveDispatch(section: PatchSectionId): number
	buildBody(section: PatchSectionId, state: SettingsControllerState): UpdateSettingsBody | null
	patch(body: UpdateSettingsBody): Promise<Settings>
	onSuccess(section: PatchSectionId, seq: number, snapshot: Settings): void
	onFailure(section: PatchSectionId, message: string): void
	onUnauthorized(): void
}

/**
 * 跨区 PATCH 全局串行队列。失败释放队列且不卡死；dispose 后丢弃在途响应。
 */
export function createSettingsSaveQueue(handlers: SettingsSaveQueueHandlers) {
	const pending: SettingsSaveJob[] = []
	let running = false
	let disposed = false
	let inFlightSeq: number | null = null

	async function pump(): Promise<void> {
		if (disposed || running) return
		const job = pending.shift()
		if (!job) return
		running = true
		const seq = handlers.dispatchSaveDispatch(job.section)
		inFlightSeq = seq
		const state = handlers.getState()
		const body = handlers.buildBody(job.section, state)
		if (!body) {
			handlers.onFailure(job.section, '无法构建保存请求')
			running = false
			inFlightSeq = null
			void pump()
			return
		}
		try {
			const snapshot = await handlers.patch(body)
			if (disposed || inFlightSeq !== seq) return
			handlers.onSuccess(job.section, seq, snapshot)
		} catch (error: unknown) {
			if (disposed || inFlightSeq !== seq) return
			const status =
				typeof error === 'object' && error !== null && 'status' in error
					? Number((error as { status: unknown }).status)
					: 0
			if (status === 401) {
				handlers.onUnauthorized()
				return
			}
			handlers.onFailure(
				job.section,
				error instanceof Error ? error.message : '保存失败',
			)
		} finally {
			if (!disposed) {
				running = false
				inFlightSeq = null
				void pump()
			}
		}
	}

	return {
		enqueue(job: SettingsSaveJob) {
			if (disposed) return
			pending.push(job)
			void pump()
		},
		pendingCount() {
			return pending.length + (running ? 1 : 0)
		},
		dispose() {
			disposed = true
			pending.length = 0
			running = false
			inFlightSeq = null
		},
		get disposed() {
			return disposed
		},
	}
}

export type SettingsSaveQueue = ReturnType<typeof createSettingsSaveQueue>
