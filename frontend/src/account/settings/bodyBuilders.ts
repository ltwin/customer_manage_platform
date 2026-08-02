import type { ChurnThreshold, Settings, UpdateSettingsBody } from '../../api/client.ts'
import {
	applyAvailabilityDraft,
	type AvailabilitySettingsDraft,
} from '../../pages/settings/availabilityDraft.ts'

export type GeneralDraft = {
	timezone: string
}

export type RemindersDraft = {
	birthdayLeadDays: number
	followUpAfterDays: number
	churnThresholds: ChurnThreshold[]
}

export type TelegramDraft = {
	digestHour: number
}

const CHURN_SHOOT_TYPES = ['portrait', 'cosplay', 'other'] as const

/** 后端 entry overlay 以默认 180 为底；提交必须带齐三类型。 */
export function completeChurnThresholds(
	snapshot: Settings,
	draft: readonly ChurnThreshold[],
): ChurnThreshold[] {
	return CHURN_SHOOT_TYPES.map((shootType) => {
		const fromDraft = draft.find((entry) => entry.shoot_type === shootType)
		if (fromDraft) {
			return { shoot_type: shootType, days: fromDraft.days }
		}
		const fromSnapshot = snapshot.churn_thresholds.find((entry) => entry.shoot_type === shootType)
		return { shoot_type: shootType, days: fromSnapshot?.days ?? 180 }
	})
}

export function buildGeneralBody(_snapshot: Settings, draft: GeneralDraft): UpdateSettingsBody {
	return { timezone: draft.timezone.trim() }
}

export function buildAvailabilityBody(
	_snapshot: Settings,
	draft: AvailabilitySettingsDraft,
): UpdateSettingsBody {
	// 只取 availability；勿展开 applyAvailabilityDraft 的空 body（会留下 churn_thresholds: undefined）。
	const { availability } = applyAvailabilityDraft({}, draft)
	return { availability }
}

export function buildRemindersBody(snapshot: Settings, draft: RemindersDraft): UpdateSettingsBody {
	return {
		birthday_lead_days: draft.birthdayLeadDays,
		follow_up_after_days: draft.followUpAfterDays,
		churn_thresholds: completeChurnThresholds(snapshot, draft.churnThresholds),
	}
}

export function buildTelegramBody(_snapshot: Settings, draft: TelegramDraft): UpdateSettingsBody {
	return { digest_hour: draft.digestHour }
}

export function bodyKeys(body: UpdateSettingsBody): string[] {
	return Object.keys(body).sort()
}
