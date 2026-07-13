import { listCustomers } from '../../api/client.ts'
import type { CustomerListResponse } from '../../api/client.ts'

export type CustomerChoice = CustomerListResponse['items'][number]
export type CustomerSelection = Pick<CustomerChoice, 'id' | 'display_name' | 'status' | 'channel' | 'avatar_revision' | 'avatar_version' | 'avatar_url'>

const pageSize = 20

export function resolveCurrentCustomer(
	options: CustomerChoice[],
	value: string,
	selectedCustomer?: CustomerSelection,
): CustomerChoice | CustomerSelection | undefined {
	return options.find((item) => item.id === value) ?? (selectedCustomer?.id === value ? selectedCustomer : undefined)
}

export async function loadVisibleCustomerPage(query: string, statuses: string, excluded: Set<string>): Promise<CustomerChoice[]> {
	for (let page = 1; ; page += 1) {
		const result = await listCustomers({ q: query || undefined, status: statuses, page, pageSize })
		const visible = result.items.filter((item) => !excluded.has(item.id ?? '') && item.status !== 'merged')
		if (visible.length > 0 || page * pageSize >= result.total || result.items.length === 0) return visible
	}
}
