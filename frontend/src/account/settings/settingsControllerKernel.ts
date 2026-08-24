import type { ChurnThreshold, Settings } from '../../api/client.ts'
import {
	fromSettings,
	type AvailabilitySettingsDraft,
} from '../../pages/settings/availabilityDraft.ts'
import type { GeneralDraft, RemindersDraft, TelegramDraft } from './bodyBuilders.ts'

export type PatchSectionId = 'general' | 'availability' | 'reminders' | 'telegram'

export type SectionState<T> = {
	draft: T
	dirty: boolean
	saving: boolean
	error: string | null
	serverUpdated: boolean
}

export type SettingsControllerState = {
	snapshot: Settings | null
	readiness: 'idle' | 'loading' | 'ready' | 'error'
	freshness: 'current' | 'stale'
	refreshError: string | null
	/** 单调请求序号：GET 与 PATCH 共享；仅最新序号响应可刷新 snapshot。 */
	latestSeq: number
	appliedSeq: number
	general: SectionState<GeneralDraft>
	availability: SectionState<AvailabilitySettingsDraft | null>
	reminders: SectionState<RemindersDraft>
	telegram: SectionState<TelegramDraft>
}

export type SettingsControllerAction =
	| { type: 'LOAD_START' }
	| { type: 'LOAD_SUCCESS'; seq: number; snapshot: Settings }
	| { type: 'LOAD_FAIL'; seq: number; message: string }
	| { type: 'EDIT_GENERAL'; timezone: string }
	| { type: 'EDIT_AVAILABILITY'; draft: AvailabilitySettingsDraft }
	| { type: 'EDIT_REMINDERS'; patch: Partial<RemindersDraft> }
	| { type: 'EDIT_TELEGRAM'; digestHour: number }
	| { type: 'SAVE_QUEUED'; section: PatchSectionId }
	| { type: 'SAVE_DISPATCH'; section: PatchSectionId }
	| { type: 'SAVE_SUCCESS'; section: PatchSectionId; seq: number; snapshot: Settings }
	| { type: 'SAVE_FAIL'; section: PatchSectionId; message: string }
	| { type: 'DISMISS_SERVER_UPDATED'; section: PatchSectionId }
	| { type: 'RESET' }

function emptyGeneral(): GeneralDraft {
	return { timezone: 'Asia/Shanghai' }
}

function emptyReminders(): RemindersDraft {
	return {
		birthdayLeadDays: 3,
		followUpAfterDays: 7,
		churnThresholds: [],
		deliverySlaDays: 14,
	}
}

function emptyTelegram(): TelegramDraft {
	return { digestHour: 9 }
}

function section<T>(draft: T): SectionState<T> {
	return {
		draft,
		dirty: false,
		saving: false,
		error: null,
		serverUpdated: false,
	}
}

export function initialSettingsControllerState(): SettingsControllerState {
	return {
		snapshot: null,
		readiness: 'idle',
		freshness: 'current',
		refreshError: null,
		latestSeq: 0,
		appliedSeq: 0,
		general: section(emptyGeneral()),
		availability: section(null),
		reminders: section(emptyReminders()),
		telegram: section(emptyTelegram()),
	}
}

function hydrateGeneral(snapshot: Settings): GeneralDraft {
	return { timezone: snapshot.timezone }
}

function hydrateReminders(snapshot: Settings): RemindersDraft {
	return {
		birthdayLeadDays: snapshot.birthday_lead_days,
		followUpAfterDays: snapshot.follow_up_after_days,
		churnThresholds: snapshot.churn_thresholds.map((entry: ChurnThreshold) => ({ ...entry })),
		deliverySlaDays: snapshot.delivery_sla_days,
	}
}

function hydrateTelegram(snapshot: Settings): TelegramDraft {
	return { digestHour: snapshot.digest_hour }
}

function hydrateCleanSections(
	state: SettingsControllerState,
	snapshot: Settings,
	options: { markDirtyServerUpdated: boolean },
): Pick<
	SettingsControllerState,
	'general' | 'availability' | 'reminders' | 'telegram'
> {
	const nextGeneral = state.general.dirty
		? {
				...state.general,
				serverUpdated: options.markDirtyServerUpdated ? true : state.general.serverUpdated,
			}
		: section(hydrateGeneral(snapshot))
	const nextAvailability = state.availability.dirty
		? {
				...state.availability,
				serverUpdated: options.markDirtyServerUpdated ? true : state.availability.serverUpdated,
			}
		: section(fromSettings(snapshot))
	const nextReminders = state.reminders.dirty
		? {
				...state.reminders,
				serverUpdated: options.markDirtyServerUpdated ? true : state.reminders.serverUpdated,
			}
		: section(hydrateReminders(snapshot))
	const nextTelegram = state.telegram.dirty
		? {
				...state.telegram,
				serverUpdated: options.markDirtyServerUpdated ? true : state.telegram.serverUpdated,
			}
		: section(hydrateTelegram(snapshot))
	return {
		general: nextGeneral,
		availability: nextAvailability,
		reminders: nextReminders,
		telegram: nextTelegram,
	}
}

function patchSection(
	state: SettingsControllerState,
	id: PatchSectionId,
	patch: Partial<SectionState<unknown>>,
): SettingsControllerState {
	const current = state[id]
	return {
		...state,
		[id]: { ...current, ...patch },
	}
}

export function canSubmitSettings(state: SettingsControllerState): boolean {
	return state.readiness === 'ready' && state.freshness === 'current' && state.snapshot !== null
}

export function isLatestSeq(state: SettingsControllerState, seq: number): boolean {
	return seq === state.latestSeq
}

export function settingsControllerReducer(
	state: SettingsControllerState,
	action: SettingsControllerAction,
): SettingsControllerState {
	switch (action.type) {
		case 'RESET':
			return initialSettingsControllerState()

		case 'LOAD_START': {
			const latestSeq = state.latestSeq + 1
			if (state.snapshot) {
				return {
					...state,
					latestSeq,
					freshness: 'stale',
					refreshError: '正在刷新设置…',
				}
			}
			return {
				...state,
				latestSeq,
				readiness: 'loading',
				freshness: 'current',
				refreshError: null,
			}
		}

		case 'LOAD_SUCCESS': {
			if (!isLatestSeq(state, action.seq)) return state
			const hydrated = hydrateCleanSections(state, action.snapshot, {
				markDirtyServerUpdated: state.snapshot !== null,
			})
			return {
				...state,
				snapshot: action.snapshot,
				readiness: 'ready',
				freshness: 'current',
				refreshError: null,
				appliedSeq: action.seq,
				...hydrated,
			}
		}

		case 'LOAD_FAIL': {
			if (!isLatestSeq(state, action.seq)) return state
			if (state.snapshot) {
				return {
					...state,
					freshness: 'stale',
					refreshError: action.message,
				}
			}
			return {
				...state,
				readiness: 'error',
				refreshError: action.message,
			}
		}

		case 'EDIT_GENERAL':
			return {
				...state,
				general: {
					...state.general,
					draft: { timezone: action.timezone },
					dirty: true,
					error: null,
				},
			}

		case 'EDIT_AVAILABILITY':
			return {
				...state,
				availability: {
					...state.availability,
					draft: action.draft,
					dirty: true,
					error: null,
				},
			}

		case 'EDIT_REMINDERS':
			return {
				...state,
				reminders: {
					...state.reminders,
					draft: { ...state.reminders.draft, ...action.patch },
					dirty: true,
					error: null,
				},
			}

		case 'EDIT_TELEGRAM':
			return {
				...state,
				telegram: {
					...state.telegram,
					draft: { digestHour: action.digestHour },
					dirty: true,
					error: null,
				},
			}

		case 'SAVE_QUEUED':
			return patchSection(state, action.section, { saving: true, error: null })

		case 'SAVE_DISPATCH': {
			const latestSeq = state.latestSeq + 1
			return {
				...patchSection(state, action.section, { saving: true, error: null }),
				latestSeq,
			}
		}

		case 'SAVE_SUCCESS': {
			const clearedSaving = patchSection(state, action.section, { saving: false })
			if (!isLatestSeq(clearedSaving, action.seq)) {
				return clearedSaving
			}
			const afterOwn: SettingsControllerState = {
				...clearedSaving,
				[action.section]: section(
					action.section === 'general'
						? hydrateGeneral(action.snapshot)
						: action.section === 'availability'
							? fromSettings(action.snapshot)
							: action.section === 'reminders'
								? hydrateReminders(action.snapshot)
								: hydrateTelegram(action.snapshot),
				),
			}
			const hydrated = hydrateCleanSections(afterOwn, action.snapshot, {
				markDirtyServerUpdated: true,
			})
			return {
				...afterOwn,
				snapshot: action.snapshot,
				readiness: 'ready',
				freshness: 'current',
				refreshError: null,
				appliedSeq: action.seq,
				...hydrated,
				// 刚保存的区已在 afterOwn 清 dirty；hydrateCleanSections 不得把它标成 serverUpdated
				[action.section]: afterOwn[action.section],
			}
		}

		case 'SAVE_FAIL':
			return patchSection(state, action.section, {
				saving: false,
				error: action.message,
			})

		case 'DISMISS_SERVER_UPDATED':
			return patchSection(state, action.section, { serverUpdated: false })

		default:
			return state
	}
}

/** SAVE_DISPATCH 之后读取分配到的序号（供 queue／测试使用）。 */
export function latestSeqOf(state: SettingsControllerState): number {
	return state.latestSeq
}
