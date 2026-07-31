import assert from 'node:assert/strict'
import test from 'node:test'

const storage = new Map<string, string>()
Object.defineProperty(globalThis, 'localStorage', {
	value: {
		getItem(key: string) { return storage.get(key) ?? null },
		setItem(key: string, value: string) { storage.set(key, value) },
		removeItem(key: string) { storage.delete(key) },
	},
})

let fetches = 0
let objectURLs = 0
const revoked: string[] = []
Object.defineProperty(globalThis, 'fetch', {
	value: async () => {
		fetches += 1
		return new Response(new Blob(['avatar']), { status: 200, headers: { 'Content-Type': 'image/png' } })
	},
})
Object.defineProperty(URL, 'createObjectURL', {
	value: () => `blob:avatar-${++objectURLs}`,
})
Object.defineProperty(URL, 'revokeObjectURL', {
	value: (url: string) => revoked.push(url),
})

const media = await import('../src/components/customers/customerAvatarMedia.ts')
const picker = await import('../src/components/customers/customerPickerModel.ts')
const session = await import('../src/auth/session.ts')
const authAccount = {
	id: 'account-fixture', email: 'fixture@example.invalid',
	created_at: '2026-07-31T00:00:00Z', timezone: 'Asia/Shanghai',
}
const access = (token: string) => ({ access_token: token, token_type: 'Bearer' as const, expires_in: 600 as const })

test('grapheme fallback keeps emoji sequences intact', () => {
	assert.equal(media.firstGrapheme('👩‍🎨 小茶'), '👩‍🎨')
	assert.equal(media.firstGrapheme('e\u0301clair'), 'e\u0301')
	assert.equal(media.firstGrapheme(''), '客')
})

test('media cache deduplicates consumers and revokes only after the final release', async () => {
	session.setAuthenticated(access('token-a'), authAccount)
	const first = media.acquireAvatarMedia('/api/v1/customers/c/avatar/content?v=sha256-a', 'ar-1')
	const second = media.acquireAvatarMedia('/api/v1/customers/c/avatar/content?v=sha256-a', 'ar-1')
	assert.equal(await first.url, await second.url)
	assert.equal(fetches, 1)
	first.release()
	assert.equal(revoked.length, 0)
	second.release()
	assert.equal(revoked.length, 1)
})

test('revision and auth generation changes do not reuse prior media', async () => {
	const revision = media.acquireAvatarMedia('/api/v1/customers/c/avatar/content?v=sha256-a', 'ar-2')
	await revision.url
	assert.equal(fetches, 2)
	session.setAnonymous()
	assert.equal(revoked.length, 2)
	const nextAuth = media.acquireAvatarMedia('/api/v1/customers/c/avatar/content?v=sha256-a', 'ar-2')
	await nextAuth.url
	assert.equal(fetches, 3)
	nextAuth.release()
})

test('picker sends the caller status matrix and removes merged candidates', async () => {
	const urls: string[] = []
	Object.defineProperty(globalThis, 'fetch', {
		value: async (input: string | URL | Request) => {
			urls.push(String(input))
			return Response.json({
				items: [customer('active'), customer('merged')],
				total: 2,
				page: 1,
				page_size: 20,
			})
		},
	})

	const active = await picker.loadVisibleCustomerPage('', 'active', new Set())
	const historical = await picker.loadVisibleCustomerPage('小茶', 'active,archived', new Set())
	assert.deepEqual(active.map((item) => item.status), ['active'])
	assert.deepEqual(historical.map((item) => item.status), ['active'])
	assert.match(urls[0] ?? '', /status=active(?:&|$)/)
	assert.match(urls[1] ?? '', /q=%E5%B0%8F%E8%8C%B6/)
	assert.match(urls[1] ?? '', /status=active%2Carchived/)
})

test('picker continues after a raw page is entirely excluded', async () => {
	const urls: string[] = []
	Object.defineProperty(globalThis, 'fetch', {
		value: async (input: string | URL | Request) => {
			urls.push(String(input))
			const page = urls.length
			return Response.json({
				items: page === 1
					? Array.from({ length: 20 }, (_, index) => customer('active', `cus_excluded_${index}`))
					: [customer('active', 'cus_visible')],
				total: 21,
				page,
				page_size: 20,
			})
		},
	})
	const excluded = new Set(Array.from({ length: 20 }, (_, index) => `cus_excluded_${index}`))
	const result = await picker.loadVisibleCustomerPage('', 'active', excluded)
	assert.deepEqual(result.map((item) => item.id), ['cus_visible'])
	assert.match(urls[1] ?? '', /page=2/)
})

test('picker keeps a selected non-active customer pinned outside current candidates', () => {
	const archived = customer('archived', 'cus_archived')
	assert.equal(picker.resolveCurrentCustomer([], 'cus_archived', archived), archived)
	assert.equal(picker.resolveCurrentCustomer([], '', archived), undefined)
})

function customer(status: 'active' | 'archived' | 'merged', id = `cus_${status}`) {
	return {
		id,
		display_name: status,
		status,
		channel: 'other',
		avatar_revision: 'ar-0',
		created_at: '2026-07-13T00:00:00Z',
		updated_at: '2026-07-13T00:00:00Z',
	}
}
