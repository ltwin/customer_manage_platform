import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import type { Settings, UpdateSettingsBody } from '../src/api/client.ts'

const storage = new Map<string, string>()
Object.defineProperty(globalThis, 'localStorage', {
	value: {
		getItem(key: string) { return storage.get(key) ?? null },
		setItem(key: string, value: string) { storage.set(key, value) },
		removeItem(key: string) { storage.delete(key) },
	},
})

let fetches = 0
const revoked: string[] = []
let objectURLs = 0
Object.defineProperty(globalThis, 'fetch', {
	value: async () => {
		fetches += 1
		return new Response(new Blob(['account-avatar']), { status: 200, headers: { 'Content-Type': 'image/png' } })
	},
})
Object.defineProperty(URL, 'createObjectURL', {
	value: () => `blob:account-${++objectURLs}`,
})
Object.defineProperty(URL, 'revokeObjectURL', {
	value: (url: string) => revoked.push(url),
})

const menuModel = await import('../src/account/accountMenuModel.ts')
const media = await import('../src/account/accountAvatarMedia.ts')
const display = await import('../src/account/profileDisplay.ts')
const session = await import('../src/auth/session.ts')
const loginNotice = await import('../src/account/security/loginNotice.ts')
const securityCopy = await import('../src/account/security/securityCopy.ts')
const client = await import('../src/api/client.ts')
const bodyBuilders = await import('../src/account/settings/bodyBuilders.ts')
const kernel = await import('../src/account/settings/settingsControllerKernel.ts')
const saveQueue = await import('../src/account/settings/settingsSaveQueue.ts')
const menuModelSource = await import('../src/account/legacyRedirects.ts')
const menuCSS = readFileSync(new URL('../src/account/account-menu.css', import.meta.url), 'utf8')
const accountMenuSource = readFileSync(new URL('../src/account/AccountMenu.tsx', import.meta.url), 'utf8')
const accountSettingsSource = readFileSync(
	new URL('../src/account/settings/AccountSettingsPage.tsx', import.meta.url),
	'utf8',
)
const appSource = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
const appShellSource = readFileSync(new URL('../src/components/AppShell.tsx', import.meta.url), 'utf8')
const indexCSS = readFileSync(new URL('../src/index.css', import.meta.url), 'utf8')
const layoutSource = readFileSync(
	new URL('../src/account/AccountCenterLayout.tsx', import.meta.url),
	'utf8',
)
const securityPage = readFileSync(new URL('../src/account/AccountSecurityPage.tsx', import.meta.url), 'utf8')
const passwordPage = readFileSync(new URL('../src/account/AccountPasswordPage.tsx', import.meta.url), 'utf8')
const changeForm = readFileSync(new URL('../src/account/ChangePasswordForm.tsx', import.meta.url), 'utf8')
const clientSource = readFileSync(new URL('../src/api/client.ts', import.meta.url), 'utf8')
const exportCard = readFileSync(new URL('../src/components/DataExportCard.tsx', import.meta.url), 'utf8')
const packageJSON = readFileSync(new URL('../package.json', import.meta.url), 'utf8')
const makefile = readFileSync(new URL('../../Makefile', import.meta.url), 'utf8')

const settingsFixture: Settings = {
	timezone: 'Asia/Shanghai',
	birthday_lead_days: 4,
	follow_up_after_days: 8,
	digest_hour: 10,
	delivery_sla_days: 14,
	health_tiers: {
		sleeping_ratio: 1.2,
		at_risk_ratio: 2,
		lost_ratio: 3.5,
		fallback_cadence_days: 120,
	},
	telegram_chat_id: 'chat-fixture',
	churn_thresholds: [
		{ shoot_type: 'portrait', days: 120 },
		{ shoot_type: 'cosplay', days: 150 },
		{ shoot_type: 'other', days: 180 },
	],
	availability: {
		weekly: {
			1: { start: '09:30', end: '18:30' },
			2: null,
			3: { start: '10:00', end: '19:00' },
			4: null,
			5: { start: '11:00', end: '20:00' },
			6: { start: '09:00', end: '17:00' },
			7: null,
		},
		min_opening_minutes: 90,
		turnaround_minutes: 45,
	},
}

function cloneSettings(settings: Settings): Settings {
	return structuredClone(settings)
}

/** 复刻后端 Get→overlay→Upsert（含 churn 默认 180 叠加）。 */
function createSettingsRMWDouble(initial: Settings) {
	let store = cloneSettings(initial)
	const bodies: UpdateSettingsBody[] = []
	return {
		bodies,
		get(): Settings {
			return cloneSettings(store)
		},
		async patch(body: UpdateSettingsBody): Promise<Settings> {
			bodies.push(structuredClone(body))
			const next = cloneSettings(store)
			if (body.timezone !== undefined) next.timezone = body.timezone
			if (body.birthday_lead_days !== undefined) next.birthday_lead_days = body.birthday_lead_days
			if (body.follow_up_after_days !== undefined) {
				next.follow_up_after_days = body.follow_up_after_days
			}
			if (body.digest_hour !== undefined) next.digest_hour = body.digest_hour
			if (body.availability !== undefined) next.availability = structuredClone(body.availability)
			if (body.churn_thresholds !== undefined) {
				const defaults: Settings['churn_thresholds'] = [
					{ shoot_type: 'portrait', days: 180 },
					{ shoot_type: 'cosplay', days: 180 },
					{ shoot_type: 'other', days: 180 },
				]
				next.churn_thresholds = defaults.map((entry) => {
					const overlay = body.churn_thresholds!.find((item) => item.shoot_type === entry.shoot_type)
					return overlay ? { ...overlay } : entry
				})
			}
			store = next
			return cloneSettings(store)
		},
	}
}

const authAccount = {
	id: 'account-fixture',
	email: 'fixture@example.invalid',
	created_at: '2026-08-02T00:00:00Z',
	timezone: 'Asia/Shanghai',
}
const access = (token: string) => ({ access_token: token, token_type: 'Bearer' as const, expires_in: 600 as const })

test('A8 menu model exposes six actions pointing at final /account URLs', () => {
	assert.equal(menuModel.ACCOUNT_MENU_ITEMS.length, 6)
	const links = menuModel.ACCOUNT_MENU_ITEMS.filter((item) => item.kind === 'link')
	assert.deepEqual(links.map((item) => item.to), [
		'/account',
		'/account/profile',
		'/account/security',
		'/account/settings',
	])
	assert.ok(menuModel.ACCOUNT_MENU_ITEMS.some((item) => item.kind === 'theme'))
	assert.ok(menuModel.ACCOUNT_MENU_ITEMS.some((item) => item.kind === 'logout'))
	assert.equal(menuModel.nextMenuIndex(0, 1, 6), 1)
	assert.equal(menuModel.nextMenuIndex(5, 1, 6), 0)
	assert.equal(menuModel.nextMenuIndex(0, -1, 6), 5)
})

test('A8 dual-slot CSS hides the inactive slot with display:none', () => {
	assert.match(menuCSS, /\.account-menu-slot-mobile\s*\{\s*display:\s*none;/)
	assert.match(
		menuCSS,
		/@media\s*\(max-width:\s*860px\)\s*\{[\s\S]*?\.account-menu-slot-desktop\s*\{\s*display:\s*none;/,
	)
	assert.match(
		menuCSS,
		/@media\s*\(max-width:\s*860px\)\s*\{[\s\S]*\.account-menu-slot-mobile\s*\{\s*display:\s*block;/,
	)
})

test('A17 profile mutation atomically replaces global display snapshot', () => {
	const previous = display.profileDisplaySnapshot({
		display_name: '旧名',
		profile_revision: 'pr-1',
		avatar_revision: 'ar-1',
		avatar_version: 'sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		avatar_url: '/api/v1/account/profile/avatar/content?v=sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
	}, authAccount.email)
	const next = display.applyProfileMutation(previous, {
		display_name: '新名',
		profile_revision: 'pr-2',
		avatar_revision: 'ar-2',
		avatar_version: 'sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		avatar_url: '/api/v1/account/profile/avatar/content?v=sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
	}, authAccount.email)
	assert.equal(next.displayName, '新名')
	assert.equal(next.fallbackLabel, '新')
	assert.notEqual(next.avatarVersion, previous.avatarVersion)
	assert.notEqual(next.avatarURL, previous.avatarURL)
})

test('A18 auth generation change clears account avatar media cache', async () => {
	session.setAuthenticated(access('token-a'), authAccount)
	const handle = media.acquireAccountAvatarMedia(
		'/api/v1/account/profile/avatar/content?v=sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		'sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
	)
	const url = await handle.url
	assert.equal(fetches, 1)
	assert.match(url, /^blob:account-/)
	session.setAnonymous()
	assert.ok(revoked.includes(url))
	const next = media.acquireAccountAvatarMedia(
		'/api/v1/account/profile/avatar/content?v=sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		'sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
	)
	await next.url
	assert.equal(fetches, 2)
	next.release()
})

test('A4b grapheme fallback keeps emoji sequences intact', () => {
	assert.equal(media.firstAccountGrapheme('👩‍🎨 小茶'), '👩‍🎨')
	assert.equal(media.fallbackAvatarLabel(null, 'alice@example.invalid'), 'a')
	assert.equal(media.fallbackAvatarLabel('  ', 'alice@example.invalid'), 'a')
})

test('A10 account settings page no longer embeds security or export cards', () => {
	// D11：旧 SettingsPage 已删；承接面为 AccountSettingsPage
	assert.doesNotMatch(accountSettingsSource, /DataExportCard/)
	assert.doesNotMatch(accountSettingsSource, /account-security-card/)
	assert.doesNotMatch(accountSettingsSource, /logoutSession/)
	assert.doesNotMatch(accountSettingsSource, /保存全部/)
})

test('final /account/* routes are mounted without placeholder dead links', () => {
	assert.match(appSource, /path="\/account"/)
	assert.match(appSource, /path="profile"/)
	assert.match(appSource, /path="security"/)
	assert.match(appSource, /path="security\/password"/)
	assert.match(appSource, /path="settings"/)
	assert.match(appSource, /AccountSettingsPage/)
	assert.doesNotMatch(appSource, /pages\/SettingsPage|from ['"].*\/SettingsPage['"]/)
	assert.doesNotMatch(appSource, /即将上线|coming soon|TODO.*account/i)
})

test('hardening A1/A2: fixed replace redirects discard search/hash via pathname-only from', () => {
	assert.equal(menuModelSource.legacyAccountRedirectPath('/settings'), '/account/settings')
	assert.equal(menuModelSource.legacyAccountRedirectPath('/change-password'), '/account/security/password')
	assert.equal(menuModelSource.legacyAccountRedirectPath('/account/settings'), null)
	assert.match(appSource, /path="\/settings" element=\{<Navigate to="\/account\/settings" replace/)
	assert.match(appSource, /path="\/change-password" element=\{<Navigate to="\/account\/security\/password" replace/)
	// RequireAuth 只传 pathname，保证登录后二次落到无 query/hash 的旧 path 再 replace
	assert.match(appSource, /from: location\.pathname/)
	assert.doesNotMatch(appSource, /from: location\.pathname \+ location\.search/)
})

test('hardening A3/A13: navItems drop settings; floating theme-toggle removed', () => {
	assert.doesNotMatch(appShellSource, /label: '设置'/)
	assert.doesNotMatch(appShellSource, /to: '\/settings'/)
	assert.doesNotMatch(appShellSource, /\bSettings\b/)
	assert.doesNotMatch(appShellSource, /theme-toggle/)
	assert.doesNotMatch(appShellSource, /\bSun\b|\bMoon\b/)
	assert.doesNotMatch(indexCSS, /\.theme-toggle/)
	const navKeys = [...appShellSource.matchAll(/key: '([^']+)'/g)].map((match) => match[1])
	assert.deepEqual(navKeys, ['dashboard', 'customers', 'orders', 'calendar', 'planning', 'packages', 'reminders'])
})

test('hardening A4: AccountMenu cycles ArrowUp/Down/Home/End via nextMenuIndex', () => {
	assert.match(accountMenuSource, /nextMenuIndex/)
	assert.match(accountMenuSource, /ArrowDown/)
	assert.match(accountMenuSource, /ArrowUp/)
	assert.match(accountMenuSource, /case 'Home'/)
	assert.match(accountMenuSource, /case 'End'/)
	assert.match(accountMenuSource, /Escape/)
	assert.match(accountMenuSource, /triggerRef\.current\?\.focus\(\)/)
	assert.equal(menuModel.nextMenuIndex(0, 1, 6), 1)
	assert.equal(menuModel.nextMenuIndex(5, 1, 6), 0)
	assert.equal(menuModel.nextMenuIndex(0, -1, 6), 5)
})

test('STEP-000b seams: layout forwards ShellContext; theme via AccountCenterContext', () => {
	assert.match(layoutSource, /useOutletContext<ShellContext>/)
	assert.match(layoutSource, /<Outlet context=\{shell\} \/>/)
	assert.match(accountSettingsSource, /useShell\(\)/)
	assert.match(accountSettingsSource, /retryTimezone/)
	assert.match(accountSettingsSource, /useAccountCenter\(\)/)
	assert.match(accountSettingsSource, /setTheme/)
	assert.doesNotMatch(accountSettingsSource, /localStorage/)
	assert.doesNotMatch(accountSettingsSource, /od-crm-theme/)
})

test('A1–A4 body builders emit only owned fields', () => {
	const general = bodyBuilders.buildGeneralBody(settingsFixture, { timezone: ' America/New_York ' })
	assert.deepEqual(bodyBuilders.bodyKeys(general), ['timezone'])
	assert.equal(general.timezone, 'America/New_York')

	const availability = bodyBuilders.buildAvailabilityBody(settingsFixture, {
		weekly: settingsFixture.availability.weekly,
		minOpeningMinutes: 60,
		turnaroundMinutes: 15,
	})
	assert.deepEqual(bodyBuilders.bodyKeys(availability), ['availability'])

	const reminders = bodyBuilders.buildRemindersBody(settingsFixture, {
		birthdayLeadDays: 5,
		followUpAfterDays: 9,
		churnThresholds: [{ shoot_type: 'portrait', days: 90 }],
		deliverySlaDays: 21,
		healthTiers: settingsFixture.health_tiers,
	})
	assert.deepEqual(bodyBuilders.bodyKeys(reminders).sort(), [
		'birthday_lead_days',
		'churn_thresholds',
		'delivery_sla_days',
		'follow_up_after_days',
		'health_tiers',
	])
	assert.equal(reminders.delivery_sla_days, 21)
	assert.equal(reminders.churn_thresholds?.length, 3)
	assert.deepEqual(
		reminders.churn_thresholds?.map((entry) => entry.shoot_type).sort(),
		['cosplay', 'other', 'portrait'],
	)
	assert.equal(reminders.churn_thresholds?.find((entry) => entry.shoot_type === 'portrait')?.days, 90)
	assert.equal(reminders.churn_thresholds?.find((entry) => entry.shoot_type === 'cosplay')?.days, 150)

	const telegram = bodyBuilders.buildTelegramBody(settingsFixture, { digestHour: 21 })
	assert.deepEqual(bodyBuilders.bodyKeys(telegram), ['digest_hour'])
	assert.equal(telegram.digest_hour, 21)
	assert.equal('telegram_chat_id' in telegram, false)
})

test('A5 dirty section keeps draft and gets server-updated banner when peer saves', () => {
	let state = kernel.initialSettingsControllerState()
	state = kernel.settingsControllerReducer(state, { type: 'LOAD_START' })
	const loadSeq = state.latestSeq
	state = kernel.settingsControllerReducer(state, {
		type: 'LOAD_SUCCESS',
		seq: loadSeq,
		snapshot: settingsFixture,
	})
	state = kernel.settingsControllerReducer(state, {
		type: 'EDIT_GENERAL',
		timezone: 'Europe/Paris',
	})
	assert.equal(state.general.dirty, true)
	assert.equal(state.general.draft.timezone, 'Europe/Paris')

	state = kernel.settingsControllerReducer(state, { type: 'SAVE_QUEUED', section: 'telegram' })
	state = kernel.settingsControllerReducer(state, { type: 'SAVE_DISPATCH', section: 'telegram' })
	const saveSeq = state.latestSeq
	const peerSnapshot = {
		...settingsFixture,
		digest_hour: 22,
		timezone: 'Asia/Tokyo',
	}
	state = kernel.settingsControllerReducer(state, {
		type: 'SAVE_SUCCESS',
		section: 'telegram',
		seq: saveSeq,
		snapshot: peerSnapshot,
	})

	assert.equal(state.general.dirty, true)
	assert.equal(state.general.draft.timezone, 'Europe/Paris')
	assert.equal(state.general.serverUpdated, true)
	assert.equal(state.snapshot?.timezone, 'Asia/Tokyo')
	assert.equal(state.telegram.dirty, false)
	assert.equal(state.telegram.draft.digestHour, 22)
})

test('A5b failed PATCH keeps section draft/error and does not pollute snapshot', () => {
	let state = kernel.initialSettingsControllerState()
	state = kernel.settingsControllerReducer(state, { type: 'LOAD_START' })
	state = kernel.settingsControllerReducer(state, {
		type: 'LOAD_SUCCESS',
		seq: state.latestSeq,
		snapshot: settingsFixture,
	})
	state = kernel.settingsControllerReducer(state, {
		type: 'EDIT_GENERAL',
		timezone: 'Europe/Berlin',
	})
	state = kernel.settingsControllerReducer(state, { type: 'SAVE_QUEUED', section: 'general' })
	state = kernel.settingsControllerReducer(state, {
		type: 'SAVE_FAIL',
		section: 'general',
		message: '网络错误',
	})
	assert.equal(state.general.dirty, true)
	assert.equal(state.general.draft.timezone, 'Europe/Berlin')
	assert.equal(state.general.error, '网络错误')
	assert.equal(state.general.saving, false)
	assert.equal(state.snapshot?.timezone, 'Asia/Shanghai')
})

test('A6 stale blocks submit; refresh hydrates only clean sections', () => {
	let state = kernel.initialSettingsControllerState()
	state = kernel.settingsControllerReducer(state, { type: 'LOAD_START' })
	state = kernel.settingsControllerReducer(state, {
		type: 'LOAD_SUCCESS',
		seq: state.latestSeq,
		snapshot: settingsFixture,
	})
	state = kernel.settingsControllerReducer(state, {
		type: 'EDIT_GENERAL',
		timezone: 'Europe/Paris',
	})
	state = kernel.settingsControllerReducer(state, { type: 'LOAD_START' })
	assert.equal(state.freshness, 'stale')
	assert.equal(kernel.canSubmitSettings(state), false)

	const refreshed = {
		...settingsFixture,
		timezone: 'Asia/Tokyo',
		digest_hour: 11,
	}
	state = kernel.settingsControllerReducer(state, {
		type: 'LOAD_SUCCESS',
		seq: state.latestSeq,
		snapshot: refreshed,
	})
	assert.equal(kernel.canSubmitSettings(state), true)
	assert.equal(state.general.dirty, true)
	assert.equal(state.general.draft.timezone, 'Europe/Paris')
	assert.equal(state.general.serverUpdated, true)
	assert.equal(state.telegram.dirty, false)
	assert.equal(state.telegram.draft.digestHour, 11)
})

test('A5c settingsSaveQueue serializes patches and RMW double keeps both fields', async () => {
	const double = createSettingsRMWDouble(settingsFixture)
	let state = kernel.initialSettingsControllerState()
	state = kernel.settingsControllerReducer(state, { type: 'LOAD_START' })
	state = kernel.settingsControllerReducer(state, {
		type: 'LOAD_SUCCESS',
		seq: state.latestSeq,
		snapshot: double.get(),
	})
	state = kernel.settingsControllerReducer(state, {
		type: 'EDIT_GENERAL',
		timezone: 'America/New_York',
	})
	state = kernel.settingsControllerReducer(state, {
		type: 'EDIT_TELEGRAM',
		digestHour: 18,
	})

	const stateBox = { current: state }
	let releaseFirst!: () => void
	const firstGate = new Promise<void>((resolve) => {
		releaseFirst = resolve
	})
	let patchCalls = 0

	const queue = saveQueue.createSettingsSaveQueue({
		getState: () => stateBox.current,
		dispatchSaveDispatch(section) {
			stateBox.current = kernel.settingsControllerReducer(stateBox.current, {
				type: 'SAVE_DISPATCH',
				section,
			})
			return stateBox.current.latestSeq
		},
		buildBody(section, current) {
			if (section === 'general') {
				return bodyBuilders.buildGeneralBody(current.snapshot!, current.general.draft)
			}
			return bodyBuilders.buildTelegramBody(current.snapshot!, current.telegram.draft)
		},
		async patch(body) {
			patchCalls += 1
			if (patchCalls === 1) await firstGate
			return double.patch(body)
		},
		onSuccess(section, seq, snapshot) {
			stateBox.current = kernel.settingsControllerReducer(stateBox.current, {
				type: 'SAVE_SUCCESS',
				section,
				seq,
				snapshot,
			})
		},
		onFailure(section, message) {
			stateBox.current = kernel.settingsControllerReducer(stateBox.current, {
				type: 'SAVE_FAIL',
				section,
				message,
			})
		},
		onUnauthorized() {},
	})

	stateBox.current = kernel.settingsControllerReducer(stateBox.current, {
		type: 'SAVE_QUEUED',
		section: 'general',
	})
	queue.enqueue({ section: 'general' })
	stateBox.current = kernel.settingsControllerReducer(stateBox.current, {
		type: 'SAVE_QUEUED',
		section: 'telegram',
	})
	queue.enqueue({ section: 'telegram' })

	await new Promise((resolve) => setTimeout(resolve, 10))
	assert.equal(patchCalls, 1)
	assert.equal(queue.pendingCount(), 2)
	releaseFirst()
	await new Promise((resolve) => setTimeout(resolve, 20))
	assert.equal(patchCalls, 2)
	assert.equal(double.get().timezone, 'America/New_York')
	assert.equal(double.get().digest_hour, 18)
	assert.equal(double.bodies[0] && bodyBuilders.bodyKeys(double.bodies[0]).join(','), 'timezone')
	assert.equal(double.bodies[1] && bodyBuilders.bodyKeys(double.bodies[1]).join(','), 'digest_hour')
	assert.ok(stateBox.current.appliedSeq >= 2)
	queue.dispose()
})

test('A11 account settings sections expose labelledby, alerts, and disabled fieldsets', () => {
	assert.match(
		accountSettingsSource,
		/aria-labelledby="accountSettingsGeneralTitle"/,
	)
	assert.match(
		accountSettingsSource,
		/aria-labelledby="accountSettingsAvailabilityTitle"/,
	)
	assert.match(
		accountSettingsSource,
		/aria-labelledby="accountSettingsRemindersTitle"/,
	)
	assert.match(
		accountSettingsSource,
		/aria-labelledby="accountSettingsTelegramTitle"/,
	)
	assert.match(
		accountSettingsSource,
		/aria-labelledby="accountSettingsAppearanceTitle"/,
	)
	assert.match(accountSettingsSource, /role="alert"/)
	assert.match(
		accountSettingsSource,
		/fieldset[\s\S]*disabled=\{state\.general\.saving \|\| submitBlocked\}/,
	)
	assert.match(
		accountSettingsSource,
		/fieldset[\s\S]*disabled=\{state\.availability\.saving \|\| submitBlocked\}/,
	)
	assert.match(
		accountSettingsSource,
		/fieldset[\s\S]*disabled=\{state\.reminders\.saving \|\| submitBlocked\}/,
	)
	assert.match(
		accountSettingsSource,
		/fieldset[\s\S]*disabled=\{state\.telegram\.saving \|\| submitBlocked\}/,
	)
})

test('A7 Telegram bind uses button + openTelegramDeepLink without href/storage', () => {
	assert.match(accountSettingsSource, /openTelegramDeepLink/)
	assert.match(accountSettingsSource, /createTelegramBindToken/)
	assert.match(accountSettingsSource, /clearPendingLink/)
	assert.match(accountSettingsSource, /10 \* 60 \* 1000/)
	assert.doesNotMatch(accountSettingsSource, /<a[^>]+href=\{[^}]*deep/i)
	assert.doesNotMatch(accountSettingsSource, /localStorage|sessionStorage/)
	assert.doesNotMatch(accountSettingsSource, /console\.(log|info|debug|warn|error)/)
})

test('A8/A13 appearance uses AccountCenterContext; od-crm-theme single write in AppShell', () => {
	assert.match(accountSettingsSource, /仅作用于当前浏览器/)
	assert.match(accountSettingsSource, /setTheme\(/)
	const themeWrites = appShellSource.match(/localStorage\.setItem\('od-crm-theme'/g) ?? []
	assert.equal(themeWrites.length, 1)
	assert.doesNotMatch(accountSettingsSource, /localStorage/)
})

test('CHK-008 account settings does not own security/export final surface', () => {
	assert.doesNotMatch(accountSettingsSource, /DataExportCard/)
	assert.doesNotMatch(accountSettingsSource, /ChangePasswordForm/)
	assert.doesNotMatch(accountSettingsSource, /logoutSession/)
})

test('STEP-000b baseline: security surface, shared form, settings stripped, test harness registered', () => {
	assert.match(appSource, /path="security"/)
	assert.match(appSource, /AccountSecurityPage/)
	assert.match(passwordPage, /ChangePasswordForm/)
	assert.doesNotMatch(accountSettingsSource, /DataExportCard|account-security-card|logoutSession/)
	assert.match(packageJSON, /"test:account-center"/)
	assert.match(packageJSON, /"test:customer-avatar"/)
	// 注册守卫：门禁改用 glob 发现 scripts/*.test.*，单文件自动入闸；
	// 这里钉住发现机制，换回手工清单会让这条失败。
	assert.match(makefile, /node --test .*"scripts\/\*\.test\.ts" "scripts\/\*\.test\.mjs"/)
})

test('CHK-001 identity card consumes AccountCenterContext and static verified label', () => {
	assert.match(securityPage, /useAccountCenter\(\)/)
	assert.match(securityPage, /account\.email/)
	assert.match(securityPage, /IDENTITY_SECTION/)
	assert.equal(securityCopy.IDENTITY_SECTION.verifiedValue, '已验证')
	assert.doesNotMatch(securityPage, /email_verified|parseAccount/)
	assert.doesNotMatch(securityPage, /设备中心|换绑邮箱|删除账号|登录历史|OAuth/)
})

test('CHK-002 changePassword keeps requestWithoutAuthRetry; wrong password never refreshes', async () => {
	assert.match(
		clientSource,
		/export function changePassword\([^)]*\): Promise<void> \{\s*return requestWithoutAuthRetry/,
	)
	assert.match(changeForm, /queueLoginNotice\(PASSWORD_CHANGED_NOTICE\)/)
	assert.match(changeForm, /setAnonymous\(\)/)
	assert.match(changeForm, /当前密码不正确/)
	assert.match(changeForm, /尝试次数过多/)

	session.setAuthenticated(access('password-change-access'), authAccount)
	let changeCalls = 0
	let refreshCalls = 0
	globalThis.fetch = async (input) => {
		const url = String(input)
		if (url.endsWith('/auth/refresh')) {
			refreshCalls += 1
			return Response.json(access('unexpected-refreshed-access'))
		}
		changeCalls += 1
		return Response.json({ error: { code: 'unauthorized', message: '认证失败' } }, { status: 401 })
	}

	await assert.rejects(
		client.changePassword('wrong-current-password', 'new-password-12'),
		(error: unknown) => error instanceof client.ApiError && error.status === 401,
	)
	assert.equal(changeCalls, 1)
	assert.equal(refreshCalls, 0)
	assert.equal(session.getAccessToken(), 'password-change-access')
	assert.equal(session.getAuthSnapshot().status, 'authenticated')
})

test('CHK-003 password-changed notice survives RequireAuth race via queue', () => {
	assert.equal(loginNotice.PASSWORD_CHANGED_NOTICE, '密码已修改，所有设备需要重新登录。')
	loginNotice.queueLoginNotice(loginNotice.PASSWORD_CHANGED_NOTICE)
	assert.equal(loginNotice.peekLoginNotice(), loginNotice.PASSWORD_CHANGED_NOTICE)
	assert.match(appSource, /peekLoginNotice/)
	assert.match(changeForm, /queueLoginNotice\(PASSWORD_CHANGED_NOTICE\)[\s\S]*setAnonymous\(\)/)
	assert.equal(loginNotice.takeLoginNotice(), loginNotice.PASSWORD_CHANGED_NOTICE)
	assert.equal(loginNotice.takeLoginNotice(), null)
})

test('CHK-004/A6 export copy matches schema v3 metadata-only avatar contract', () => {
	assert.match(exportCard, /EXPORT_SECTION/)
	assert.match(securityCopy.EXPORT_SECTION.avatarHint, /version/)
	assert.match(securityCopy.EXPORT_SECTION.avatarHint, /media_type/)
	assert.match(securityCopy.EXPORT_SECTION.avatarHint, /size/)
	assert.match(securityCopy.EXPORT_SECTION.avatarHint, /updated_at/)
	assert.match(securityCopy.EXPORT_SECTION.avatarHint, /avatar_url/)
	assert.doesNotMatch(securityCopy.EXPORT_SECTION.avatarHint, /公开头像引用/)
	assert.match(securityCopy.EXPORT_SECTION.piiHint, /姓名、手机号、社交身份、备注、Telegram chat ID/)
	assert.match(exportCard, /disabled=\{downloading\}/)
	assert.match(exportCard, /if \(downloading\) return/)
})

test('CHK-005/CHK-006 final IA boundary vocabulary without re-adding settings security card', () => {
	assert.match(securityPage, /BOUNDARY_SECTION/)
	for (const term of [
		'设备中心',
		'换绑邮箱',
		'登录历史',
		'OAuth',
		'删除账号',
		'彻底擦除备份',
		'24 小时',
		'GC',
		'历史备份',
	]) {
		assert.match(securityCopy.BOUNDARY_SECTION.body, new RegExp(term))
	}
	assert.doesNotMatch(accountSettingsSource, /DataExportCard|account-security-card|logoutSession/)
})

test('CHK-008/A12 legacy paths redirect; password page keeps security footer', () => {
	assert.match(appSource, /path="\/change-password" element=\{<Navigate to="\/account\/security\/password" replace/)
	assert.match(passwordPage, /ChangePasswordForm/)
	assert.match(passwordPage, /to="\/account\/security"/)
	assert.doesNotMatch(appSource, /ChangePasswordPage/)
})

test('hardening A12: HTTP /settings API retained after old page delete', () => {
	assert.match(clientSource, /request<Settings>\('\/settings'/)
	assert.match(clientSource, /function getSettings/)
	assert.match(clientSource, /function updateSettings/)
})

test('CHK-009 privacy-security does not claim device center or universal security API', () => {
	assert.doesNotMatch(securityPage, /\/api\/v1\/security|fetchSecurity|listSessions|revokeOther/)
	assert.doesNotMatch([securityPage, changeForm, exportCard].join('\n'), /console\.(?:log|debug|info)/)
})
