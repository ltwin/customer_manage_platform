import { listCustomers } from '../../api/client.ts'
import type { CustomerListResponse } from '../../api/client.ts'

export type CustomerChoice = CustomerListResponse['items'][number]
/** 已选客户的最小展示形态：编辑档期等场景只有 id/名称/状态，头像与渠道字段可缺省 */
export type CustomerSelection = Pick<CustomerChoice, 'id' | 'display_name' | 'status'> &
	Partial<Pick<CustomerChoice, 'channel' | 'avatar_revision' | 'avatar_version' | 'avatar_url'>>

const pageSize = 20

export type CustomerPickerDirection = 'next' | 'previous'

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

export function nextCustomerPickerIndex(
	activeIndex: number | null,
	optionCount: number,
	direction: CustomerPickerDirection,
): number | null {
	if (optionCount <= 0) return null
	if (activeIndex === null || activeIndex < 0 || activeIndex >= optionCount) {
		return direction === 'next' ? 0 : optionCount - 1
	}
	return direction === 'next'
		? Math.min(activeIndex + 1, optionCount - 1)
		: Math.max(activeIndex - 1, 0)
}

export function customerPickerActiveDescendant(
	listboxID: string,
	activeIndex: number | null,
	optionCount: number,
): string | undefined {
	if (activeIndex === null || activeIndex < 0 || activeIndex >= optionCount) return undefined
	return `${listboxID}-option-${activeIndex}`
}

export function customerPickerShouldClose(focusStillInside: boolean): boolean {
	return !focusStillInside
}
