import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { ApiError } from '../../api/client.ts'
import CustomerAvatar from './CustomerAvatar'
import {
	loadVisibleCustomerPage,
	customerPickerActiveDescendant,
	customerPickerShouldClose,
	nextCustomerPickerIndex,
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
	const [activeIndex, setActiveIndex] = useState<number | null>(null)
	const [reloadTick, setReloadTick] = useState(0)
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
		setOptions([])
		setActiveIndex(null)
		loadVisibleCustomerPage(query.trim(), statuses, excluded)
			.then((items) => {
				if (!active) return
				setOptions(items)
				setActiveIndex(items.length > 0 ? 0 : null)
			})
			.catch((reason: unknown) => {
				if (!active) return
				setActiveIndex(null)
				if (reason instanceof ApiError && reason.status === 401) {
					unauthorizedRef.current?.()
					return
				}
				setError(reason instanceof Error ? reason.message : '客户候选加载失败')
			})
			.finally(() => { if (active) setLoading(false) })
		return () => { active = false }
	}, [disabled, excluded, open, query, reloadTick, statuses])

	function choose(choice: CustomerChoice) {
		onChange(choice)
		setQuery('')
		setOpen(false)
		setActiveIndex(null)
	}

	const activeDescendant = open
		? customerPickerActiveDescendant(listboxID, activeIndex, options.length)
		: undefined

	return (
		<div
			className="customer-picker"
			onBlurCapture={(event) => {
				const focusStillInside = event.relatedTarget instanceof Node
					&& event.currentTarget.contains(event.relatedTarget)
				if (customerPickerShouldClose(focusStillInside)) {
					setOpen(false)
					setActiveIndex(null)
				}
			}}
		>
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
					aria-activedescendant={activeDescendant}
					value={open ? query : current?.display_name ?? ''}
					placeholder={current ? undefined : `搜索并选择${label}`}
					disabled={disabled}
					onFocus={() => setOpen(true)}
					onChange={(event) => {
						setQuery(event.target.value)
						setActiveIndex(null)
						setOpen(true)
					}}
					onKeyDown={(event) => {
						if (event.key === 'Escape' || event.key === 'Tab') {
							setOpen(false)
							setActiveIndex(null)
						}
						if (event.key === 'ArrowDown') {
							event.preventDefault()
							setActiveIndex((index) => nextCustomerPickerIndex(index, options.length, 'next'))
						}
						if (event.key === 'ArrowUp') {
							event.preventDefault()
							setActiveIndex((index) => nextCustomerPickerIndex(index, options.length, 'previous'))
						}
						if (event.key === 'Enter' && activeIndex !== null && options[activeIndex]) {
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
					{error && (
						<div className="customer-picker-state danger-text" role="alert">
							<div>{error}</div>
							<button className="btn btn-sm" type="button" onClick={() => setReloadTick((tick) => tick + 1)}>
								重试
							</button>
						</div>
					)}
					{!loading && !error && options.length === 0 && <div className="customer-picker-state">暂无匹配客户</div>}
					{options.map((choice, index) => (
						<button
							key={choice.id}
							id={`${listboxID}-option-${index}`}
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
