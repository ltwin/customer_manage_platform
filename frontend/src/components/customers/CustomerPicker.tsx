import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { ApiError } from '../../api/client.ts'
import CustomerAvatar from './CustomerAvatar'
import {
	loadVisibleCustomerPage,
	resolveCurrentCustomer,
} from './customerPickerModel.ts'
import type { CustomerChoice, CustomerSelection } from './customerPickerModel.ts'
import './CustomerPicker.css'

export type { CustomerChoice, CustomerSelection } from './customerPickerModel.ts'

type Props = {
	value: string
	candidateStatuses: Array<'active' | 'archived'>
	selectedCustomer?: CustomerSelection
	excludeCustomerIds?: string[]
	onChange(choice: CustomerChoice | null): void
	onUnauthorized?: () => void
	required?: boolean
	disabled?: boolean
	label?: string
}

export default function CustomerPicker({
	value,
	candidateStatuses,
	selectedCustomer,
	excludeCustomerIds = [],
	onChange,
	onUnauthorized,
	required = false,
	disabled = false,
	label = '客户',
}: Props) {
	const listboxID = useId()
	const [query, setQuery] = useState('')
	const [options, setOptions] = useState<CustomerChoice[]>([])
	const [loading, setLoading] = useState(false)
	const [error, setError] = useState<string | null>(null)
	const [open, setOpen] = useState(false)
	const [activeIndex, setActiveIndex] = useState(0)
	const unauthorizedRef = useRef(onUnauthorized)
	unauthorizedRef.current = onUnauthorized
	const excludeKey = excludeCustomerIds.join('\u0000')
	const excluded = useMemo(() => new Set(excludeKey ? excludeKey.split('\u0000') : []), [excludeKey])
	const statuses = candidateStatuses.join(',')
	const current = resolveCurrentCustomer(options, value, selectedCustomer)

	useEffect(() => {
		if (!open || disabled) return
		let active = true
		setLoading(true)
		setError(null)
		loadVisibleCustomerPage(query.trim(), statuses, excluded)
			.then((items) => {
				if (!active) return
				setOptions(items)
				setActiveIndex(0)
			})
			.catch((reason: unknown) => {
				if (!active) return
				if (reason instanceof ApiError && reason.status === 401) unauthorizedRef.current?.()
				setError(reason instanceof Error ? reason.message : '客户候选加载失败')
			})
			.finally(() => { if (active) setLoading(false) })
		return () => { active = false }
	}, [disabled, excluded, open, query, statuses])

	function choose(choice: CustomerChoice) {
		onChange(choice)
		setQuery('')
		setOpen(false)
	}

	return (
		<div className="customer-picker">
			<div className="customer-picker-control">
				{current && (
					<CustomerAvatar
						customerId={current.id ?? ''}
						displayName={current.display_name}
						avatarRevision={current.avatar_revision}
						avatarUrl={current.avatar_url}
						size="sm"
						decorative
					/>
				)}
				<input
					className="input"
					role="combobox"
					aria-label={label}
					aria-controls={listboxID}
					aria-expanded={open}
					aria-autocomplete="list"
					value={open ? query : current?.display_name ?? ''}
					placeholder={current ? undefined : `搜索并选择${label}`}
					disabled={disabled}
					onFocus={() => setOpen(true)}
					onChange={(event) => { setQuery(event.target.value); setOpen(true) }}
					onKeyDown={(event) => {
						if (event.key === 'Escape') setOpen(false)
						if (event.key === 'ArrowDown') {
							event.preventDefault()
							setActiveIndex((index) => Math.min(index + 1, options.length - 1))
						}
						if (event.key === 'ArrowUp') {
							event.preventDefault()
							setActiveIndex((index) => Math.max(index - 1, 0))
						}
						if (event.key === 'Enter' && options[activeIndex]) {
							event.preventDefault()
							choose(options[activeIndex])
						}
					}}
				/>
				{value && !required && (
					<button className="icon-btn" type="button" aria-label={`清除${label}`} onClick={() => onChange(null)}>×</button>
				)}
			</div>
			{open && (
				<div id={listboxID} className="customer-picker-list" role="listbox">
					{loading && <div className="customer-picker-state">加载中</div>}
					{error && <div className="customer-picker-state danger-text">{error}</div>}
					{!loading && !error && options.length === 0 && <div className="customer-picker-state">暂无匹配客户</div>}
					{options.map((choice, index) => (
						<button
							key={choice.id}
							className={`customer-picker-option${activeIndex === index ? ' active' : ''}`}
							type="button"
							role="option"
							aria-selected={choice.id === value}
							onMouseDown={(event) => event.preventDefault()}
							onClick={() => choose(choice)}
						>
							<CustomerAvatar customerId={choice.id ?? ''} displayName={choice.display_name} avatarRevision={choice.avatar_revision} avatarUrl={choice.avatar_url} size="sm" decorative />
							<span><strong>{choice.display_name}</strong><small>{shortCustomerID(choice.id ?? '')} · {choice.status}</small></span>
						</button>
					))}
				</div>
			)}
		</div>
	)
}

function shortCustomerID(id: string) {
	return `#${id.replace(/^cus_/, '').slice(-6)}`
}
