import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
	ApiError,
	createTelegramBindToken,
	getSettings,
	updateSettings,
	type ChurnThreshold,
	type Settings,
	type UpdateSettingsBody,
} from '../../api/client.ts'
import StateNotice from '../../components/StateNotice.tsx'
import { useShell } from '../../components/shellContext.ts'
import { openTelegramDeepLink } from '../../components/telegramBinding.ts'
import AvailabilityEditor from '../../pages/settings/AvailabilityEditor.tsx'
import {
	validateAvailabilityDraft,
	type AvailabilitySettingsDraft,
} from '../../pages/settings/availabilityDraft.ts'
import '../../pages/settings/settings.css'
import { useAccountCenter } from '../AccountCenterContext.tsx'
import {
	buildAvailabilityBody,
	buildGeneralBody,
	buildRemindersBody,
	buildTelegramBody,
	validateHealthTiersDraft,
} from './bodyBuilders.ts'
import {
	canSubmitSettings,
	initialSettingsControllerState,
	settingsControllerReducer,
	type PatchSectionId,
	type SettingsControllerAction,
	type SettingsControllerState,
} from './settingsControllerKernel.ts'
import { createSettingsSaveQueue } from './settingsSaveQueue.ts'
import './account-settings.css'
import PlanningBusinessRulesSection from './PlanningBusinessRulesSection.tsx'

const shootTypeLabels: Record<ChurnThreshold['shoot_type'], string> = {
	portrait: '写真',
	cosplay: 'Cosplay',
	other: '其他',
}

function buildSectionBody(
	section: PatchSectionId,
	state: SettingsControllerState,
): UpdateSettingsBody | null {
	if (!state.snapshot) return null
	switch (section) {
		case 'general':
			return buildGeneralBody(state.snapshot, state.general.draft)
		case 'availability':
			if (!state.availability.draft) return null
			return buildAvailabilityBody(state.snapshot, state.availability.draft)
		case 'reminders':
			return buildRemindersBody(state.snapshot, state.reminders.draft)
		case 'telegram':
			return buildTelegramBody(state.snapshot, state.telegram.draft)
	}
}

function validateSection(
	section: PatchSectionId,
	state: SettingsControllerState,
): string | null {
	switch (section) {
		case 'general':
			if (!state.general.draft.timezone.trim()) return '时区必填'
			return null
		case 'availability': {
			if (!state.availability.draft) return '可约时段尚未加载完成'
			const result = validateAvailabilityDraft(state.availability.draft)
			return result.ok === false ? result.message : null
		}
		case 'reminders': {
			const { birthdayLeadDays, followUpAfterDays, churnThresholds, deliverySlaDays } = state.reminders.draft
			if (birthdayLeadDays < 1 || followUpAfterDays < 1) return '天数须 ≥ 1'
			if (deliverySlaDays < 1) return '交付 SLA 天数须 ≥ 1'
			for (const entry of churnThresholds) {
				if (entry.days < 1) return '流失阈值天数须 ≥ 1'
			}
			return validateHealthTiersDraft(state.reminders.draft.healthTiers)
		}
		case 'telegram': {
			const hour = state.telegram.draft.digestHour
			if (hour < 0 || hour > 23) return 'digest_hour 须在 0-23'
			return null
		}
	}
}

export default function AccountSettingsPage() {
	const navigate = useNavigate()
	const { notify, retryTimezone } = useShell()
	const { theme, setTheme } = useAccountCenter()
	const [state, setState] = useState(initialSettingsControllerState)
	const stateRef = useRef(state)
	stateRef.current = state

	const dispatchSynced = useCallback((action: SettingsControllerAction) => {
		const next = settingsControllerReducer(stateRef.current, action)
		stateRef.current = next
		setState(next)
		return next
	}, [])

	const [reloadTick, setReloadTick] = useState(0)
	const [binding, setBinding] = useState(false)
	const [bindingError, setBindingError] = useState<string | null>(null)
	const [blockedDeepLink, setBlockedDeepLink] = useState<string | null>(null)
	const bindExpiryTimer = useRef<number | null>(null)

	const clearPendingLink = useCallback(() => {
		if (bindExpiryTimer.current !== null) {
			window.clearTimeout(bindExpiryTimer.current)
			bindExpiryTimer.current = null
		}
		setBlockedDeepLink(null)
	}, [])

	useEffect(() => clearPendingLink, [clearPendingLink])

	const queueRef = useRef<ReturnType<typeof createSettingsSaveQueue> | null>(null)

	useEffect(() => {
		const queue = createSettingsSaveQueue({
			getState: () => stateRef.current,
			dispatchSaveDispatch(section) {
				return dispatchSynced({ type: 'SAVE_DISPATCH', section }).latestSeq
			},
			buildBody: buildSectionBody,
			patch: updateSettings,
			onSuccess(section, seq, snapshot) {
				dispatchSynced({ type: 'SAVE_SUCCESS', section, seq, snapshot })
				notify(
					section === 'general'
						? '常规设置已保存'
						: section === 'availability'
							? '可约时段已保存'
							: section === 'reminders'
								? '提醒规则已保存'
								: 'Telegram 摘要已保存',
				)
				if (section === 'general') {
					retryTimezone()
				}
			},
			onFailure(section, message) {
				dispatchSynced({ type: 'SAVE_FAIL', section, message })
			},
			onUnauthorized() {
				queue.dispose()
				navigate('/login', { replace: true })
			},
		})
		queueRef.current = queue
		return () => {
			queue.dispose()
			if (queueRef.current === queue) {
				queueRef.current = null
			}
		}
	}, [dispatchSynced, navigate, notify, retryTimezone])

	useEffect(() => {
		const seq = dispatchSynced({ type: 'LOAD_START' }).latestSeq
		let active = true
		getSettings()
			.then((snapshot) => {
				if (!active) return
				dispatchSynced({ type: 'LOAD_SUCCESS', seq, snapshot })
			})
			.catch((err: unknown) => {
				if (!active) return
				if (err instanceof ApiError && err.status === 401) {
					navigate('/login', { replace: true })
					return
				}
				dispatchSynced({
					type: 'LOAD_FAIL',
					seq,
					message: err instanceof Error ? err.message : '加载设置失败',
				})
			})
		return () => {
			active = false
		}
	}, [dispatchSynced, navigate, reloadTick])

	function enqueueSave(section: PatchSectionId) {
		if (!canSubmitSettings(stateRef.current)) return
		const validationError = validateSection(section, stateRef.current)
		if (validationError) {
			dispatchSynced({ type: 'SAVE_FAIL', section, message: validationError })
			return
		}
		dispatchSynced({ type: 'SAVE_QUEUED', section })
		queueRef.current?.enqueue({ section })
	}

	async function onBindTelegram() {
		clearPendingLink()
		setBindingError(null)
		setBinding(true)
		try {
			const { deep_link: deepLink } = await createTelegramBindToken()
			if (openTelegramDeepLink(deepLink)) {
				notify('已打开 Telegram，请在私聊中确认绑定')
				return
			}
			setBlockedDeepLink(deepLink)
			setBindingError('弹窗被浏览器拦截，请使用下方按钮再次打开 Telegram')
			bindExpiryTimer.current = window.setTimeout(clearPendingLink, 10 * 60 * 1000)
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) {
				navigate('/login', { replace: true })
				return
			}
			setBindingError(err instanceof Error ? err.message : '生成 Telegram 绑定链接失败')
		} finally {
			setBinding(false)
		}
	}

	function retryBlockedPopup() {
		if (blockedDeepLink && openTelegramDeepLink(blockedDeepLink)) {
			clearPendingLink()
			setBindingError(null)
			notify('已打开 Telegram，请在私聊中确认绑定')
		}
	}

	const stale = state.freshness === 'stale'
	const submitBlocked = !canSubmitSettings(state)

	if (state.readiness === 'loading' || state.readiness === 'idle') {
		return (
			<main className="content settings-stack account-settings">
				<StateNotice kind="loading" message="正在加载设置" />
			</main>
		)
	}

	if (state.readiness === 'error' || !state.snapshot) {
		return (
			<main className="content settings-stack account-settings">
				<StateNotice
					kind="error"
					message={state.refreshError ?? '加载设置失败'}
					retryable
					onRetry={() => setReloadTick((value) => value + 1)}
				/>
			</main>
		)
	}

	const settings: Settings = state.snapshot

	return (
		<main className="content settings-stack account-settings">
			{stale && (
				<StateNotice
					kind="refresh-error"
					message={state.refreshError ?? '设置可能已过期'}
					onRetry={() => setReloadTick((value) => value + 1)}
				/>
			)}

			<section className="card form-stack" aria-labelledby="accountSettingsGeneralTitle">
				<div>
					<h2 id="accountSettingsGeneralTitle">常规</h2>
					<p className="sub">账号时区；保存后刷新壳层时区缓存。</p>
				</div>
				{state.general.serverUpdated && (
					<p className="account-settings-server-updated" role="status">
						服务端设置已更新；未保存的本地修改仍保留。
						<button
							type="button"
							className="btn"
							onClick={() =>
								dispatchSynced({ type: 'DISMISS_SERVER_UPDATED', section: 'general' })
							}
						>
							知道了
						</button>
					</p>
				)}
				<form
					onSubmit={(event) => {
						event.preventDefault()
						enqueueSave('general')
					}}
				>
					<fieldset
						className="settings-edit-fields"
						disabled={state.general.saving || submitBlocked}
					>
						<label>
							账号时区（IANA）
							<input
								value={state.general.draft.timezone}
								onChange={(event) =>
									dispatchSynced({ type: 'EDIT_GENERAL', timezone: event.target.value })
								}
								required
							/>
						</label>
						{state.general.error && (
							<div className="form-error" role="alert">
								{state.general.error}
							</div>
						)}
						<div className="topbar-actions">
							<button
								className="btn btn-primary"
								type="submit"
								disabled={state.general.saving || submitBlocked}
							>
								{state.general.saving ? '保存中…' : '保存常规'}
							</button>
						</div>
					</fieldset>
				</form>
			</section>

			<section className="card form-stack" aria-labelledby="accountSettingsAvailabilityTitle">
				<div>
					<h2 id="accountSettingsAvailabilityTitle">可约时段</h2>
					<p className="sub">周可约窗口与最小空档、转场缓冲。</p>
				</div>
				{state.availability.serverUpdated && (
					<p className="account-settings-server-updated" role="status">
						服务端设置已更新；未保存的本地修改仍保留。
						<button
							type="button"
							className="btn"
							onClick={() =>
								dispatchSynced({ type: 'DISMISS_SERVER_UPDATED', section: 'availability' })
							}
						>
							知道了
						</button>
					</p>
				)}
				<form
					onSubmit={(event) => {
						event.preventDefault()
						enqueueSave('availability')
					}}
				>
					<fieldset
						className="settings-edit-fields"
						disabled={state.availability.saving || submitBlocked}
					>
						{state.availability.draft ? (
							<AvailabilityEditor
								draft={state.availability.draft}
								disabled={state.availability.saving || submitBlocked}
								onChange={(draft: AvailabilitySettingsDraft) =>
									dispatchSynced({ type: 'EDIT_AVAILABILITY', draft })
								}
							/>
						) : (
							<StateNotice kind="loading" message="正在准备可约时段" />
						)}
						{state.availability.error && (
							<div className="form-error" role="alert">
								{state.availability.error}
							</div>
						)}
						<div className="topbar-actions">
							<button
								className="btn btn-primary"
								type="submit"
								disabled={
									state.availability.saving || submitBlocked || !state.availability.draft
								}
							>
								{state.availability.saving ? '保存中…' : '保存可约时段'}
							</button>
						</div>
					</fieldset>
				</form>
			</section>

			<section className="card form-stack" aria-labelledby="accountSettingsRemindersTitle">
				<div>
					<h2 id="accountSettingsRemindersTitle">提醒规则</h2>
					<p className="sub">生日提前、交付回访、流失阈值与交付 SLA 默认值。</p>
				</div>
				{state.reminders.serverUpdated && (
					<p className="account-settings-server-updated" role="status">
						服务端设置已更新；未保存的本地修改仍保留。
						<button
							type="button"
							className="btn"
							onClick={() =>
								dispatchSynced({ type: 'DISMISS_SERVER_UPDATED', section: 'reminders' })
							}
						>
							知道了
						</button>
					</p>
				)}
				<form
					onSubmit={(event) => {
						event.preventDefault()
						enqueueSave('reminders')
					}}
				>
					<fieldset
						className="settings-edit-fields"
						disabled={state.reminders.saving || submitBlocked}
					>
						<label>
							生日提前提醒天数
							<input
								type="number"
								min={1}
								value={state.reminders.draft.birthdayLeadDays}
								onChange={(event) =>
									dispatchSynced({
										type: 'EDIT_REMINDERS',
										patch: { birthdayLeadDays: Number(event.target.value) },
									})
								}
							/>
						</label>
						<label>
							交付后回访天数
							<input
								type="number"
								min={1}
								value={state.reminders.draft.followUpAfterDays}
								onChange={(event) =>
									dispatchSynced({
										type: 'EDIT_REMINDERS',
										patch: { followUpAfterDays: Number(event.target.value) },
									})
								}
							/>
						</label>
						<label>
							交付 SLA 天数（拍摄后承诺交片）
							<input
								type="number"
								min={1}
								value={state.reminders.draft.deliverySlaDays}
								onChange={(event) =>
									dispatchSynced({
										type: 'EDIT_REMINDERS',
										patch: { deliverySlaDays: Number(event.target.value) },
									})
								}
							/>
						</label>
						<fieldset>
							<legend>健康度分层（个人节奏倍数 · 仪表盘客户资产盘点）</legend>
							<label>
								沉睡分界（倍）
								<input
									type="number"
									step="0.1"
									min={0.1}
									value={state.reminders.draft.healthTiers.sleeping_ratio}
									onChange={(event) =>
										dispatchSynced({
											type: 'EDIT_REMINDERS',
											patch: {
												healthTiers: {
													...state.reminders.draft.healthTiers,
													sleeping_ratio: Number(event.target.value),
												},
											},
										})
									}
								/>
							</label>
							<label>
								高危分界（倍）
								<input
									type="number"
									step="0.1"
									min={0.1}
									value={state.reminders.draft.healthTiers.at_risk_ratio}
									onChange={(event) =>
										dispatchSynced({
											type: 'EDIT_REMINDERS',
											patch: {
												healthTiers: {
													...state.reminders.draft.healthTiers,
													at_risk_ratio: Number(event.target.value),
												},
											},
										})
									}
								/>
							</label>
							<label>
								已流失分界（倍）
								<input
									type="number"
									step="0.1"
									min={0.1}
									value={state.reminders.draft.healthTiers.lost_ratio}
									onChange={(event) =>
										dispatchSynced({
											type: 'EDIT_REMINDERS',
											patch: {
												healthTiers: {
													...state.reminders.draft.healthTiers,
													lost_ratio: Number(event.target.value),
												},
											},
										})
									}
								/>
							</label>
							<label>
								通用基线（天 · 仅 1 次拍摄客户）
								<input
									type="number"
									min={30}
									max={365}
									value={state.reminders.draft.healthTiers.fallback_cadence_days}
									onChange={(event) =>
										dispatchSynced({
											type: 'EDIT_REMINDERS',
											patch: {
												healthTiers: {
													...state.reminders.draft.healthTiers,
													fallback_cadence_days: Number(event.target.value),
												},
											},
										})
									}
								/>
							</label>
						</fieldset>
						<fieldset>
							<legend>流失阈值（天）</legend>
							{state.reminders.draft.churnThresholds.map((entry, index) => (
								<label key={entry.shoot_type}>
									{shootTypeLabels[entry.shoot_type] ?? entry.shoot_type}
									<input
										type="number"
										min={1}
										value={entry.days}
										onChange={(event) => {
											const days = Number(event.target.value)
											const churnThresholds = state.reminders.draft.churnThresholds.map(
												(item, itemIndex) =>
													itemIndex === index ? { ...item, days } : item,
											)
											dispatchSynced({
												type: 'EDIT_REMINDERS',
												patch: { churnThresholds },
											})
										}}
									/>
								</label>
							))}
						</fieldset>
						{state.reminders.error && (
							<div className="form-error" role="alert">
								{state.reminders.error}
							</div>
						)}
						<div className="topbar-actions">
							<button
								className="btn btn-primary"
								type="submit"
								disabled={state.reminders.saving || submitBlocked}
							>
								{state.reminders.saving ? '保存中…' : '保存提醒规则'}
							</button>
						</div>
					</fieldset>
				</form>
			</section>

			<section className="card form-stack telegram-binding-card" aria-labelledby="accountSettingsTelegramTitle">
				<div>
					<h2 id="accountSettingsTelegramTitle">Telegram 摘要</h2>
					<p className="sub">
						{settings.telegram_chat_id
							? '已绑定；摘要会发送到当前私聊。'
							: '尚未绑定；绑定后可接收每日摘要并使用 /today。'}
					</p>
				</div>
				{state.telegram.serverUpdated && (
					<p className="account-settings-server-updated" role="status">
						服务端设置已更新；未保存的本地修改仍保留。
						<button
							type="button"
							className="btn"
							onClick={() =>
								dispatchSynced({ type: 'DISMISS_SERVER_UPDATED', section: 'telegram' })
							}
						>
							知道了
						</button>
					</p>
				)}
				<div className="telegram-binding-actions">
					<span className={`badge ${settings.telegram_chat_id ? 'badge-success' : 'badge-muted'}`}>
						{settings.telegram_chat_id ? '已绑定' : '未绑定'}
					</span>
					<button
						className="btn btn-primary"
						type="button"
						disabled={binding}
						onClick={() => void onBindTelegram()}
					>
						{binding
							? '正在生成绑定链接…'
							: settings.telegram_chat_id
								? '重新绑定'
								: '绑定 Telegram'}
					</button>
				</div>
				{bindingError && (
					<div className="form-error" role="alert">
						{bindingError}
					</div>
				)}
				{blockedDeepLink && (
					<button className="btn" type="button" onClick={retryBlockedPopup}>
						再次打开 Telegram
					</button>
				)}
				<form
					onSubmit={(event) => {
						event.preventDefault()
						enqueueSave('telegram')
					}}
				>
					<fieldset
						className="settings-edit-fields"
						disabled={state.telegram.saving || submitBlocked}
					>
						<label>
							每日摘要小时（0-23，按账号时区）
							<input
								type="number"
								min={0}
								max={23}
								value={state.telegram.draft.digestHour}
								onChange={(event) =>
									dispatchSynced({
										type: 'EDIT_TELEGRAM',
										digestHour: Number(event.target.value),
									})
								}
							/>
						</label>
						{state.telegram.error && (
							<div className="form-error" role="alert">
								{state.telegram.error}
							</div>
						)}
						<div className="topbar-actions">
							<button
								className="btn btn-primary"
								type="submit"
								disabled={state.telegram.saving || submitBlocked}
							>
								{state.telegram.saving ? '保存中…' : '保存摘要小时'}
							</button>
						</div>
					</fieldset>
				</form>
			</section>

			<PlanningBusinessRulesSection
				key={settings.planning_business_rule_revision}
				settings={settings}
				disabled={submitBlocked}
				onSaved={() => {
					notify('经营规则已保存')
					setReloadTick((value) => value + 1)
				}}
				onRefresh={() => setReloadTick((value) => value + 1)}
			/>

			<section className="card form-stack" aria-labelledby="accountSettingsAppearanceTitle">
				<div>
					<h2 id="accountSettingsAppearanceTitle">外观</h2>
					<p className="sub">仅作用于当前浏览器，不会同步到其他设备。</p>
				</div>
				<div className="account-settings-theme-row">
					<label>
						<span>主题</span>
						<select
							value={theme}
							onChange={(event) =>
								setTheme(event.target.value === 'dark' ? 'dark' : 'light')
							}
						>
							<option value="light">浅色</option>
							<option value="dark">暗色</option>
						</select>
					</label>
				</div>
			</section>
		</main>
	)
}
