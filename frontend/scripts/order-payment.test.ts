import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildPaymentPatch,
  hydratePaymentDraft,
  validatePaymentDraft,
  yuanInputToCents,
} from '../src/components/orders/paymentDraft.ts'
import type { OrderListItem } from '../src/api/client.ts'

const timezone = 'Asia/Shanghai'

const order = (over: Record<string, unknown> = {}): OrderListItem => ({
  id: 'ord-1',
  account_id: 'acc-1',
  created_at: '2026-06-01T00:00:00Z',
  customer_id: 'cus-1',
  status: 'retouching',
  price: 128000,
  deposit_paid: true,
  balance_paid: false,
  amount_paid: 30000,
  outstanding_amount: 98000,
  paid_at: '2026-07-01T04:00:00Z',
  channel_snapshot: 'douyin',
  delivery_due_at: '2026-07-22',
  delivery_due_is_override: true,
  shot_at: '2026-07-08T04:00:00Z',
  customer_display_name: '阿茶',
  ...over,
}) as OrderListItem

test('hydratePaymentDraft seeds yuan inputs and dates from the order', () => {
  const draft = hydratePaymentDraft(order(), timezone)
  assert.equal(draft.amountPaidYuan, '300')
  assert.equal(draft.paidDate, '2026-07-01')
  assert.equal(draft.dueDate, '2026-07-22')
  assert.equal(draft.dueIsOverride, true)
})

test('hydratePaymentDraft tolerates missing payment facts', () => {
  const draft = hydratePaymentDraft(order({ amount_paid: 0, paid_at: undefined, outstanding_amount: 128000, delivery_due_at: undefined, delivery_due_is_override: false }), timezone)
  assert.equal(draft.amountPaidYuan, '')
  assert.equal(draft.paidDate, '')
  assert.equal(draft.dueDate, '')
  assert.equal(draft.dueIsOverride, false)
})

test('yuanInputToCents converts decimal yuan input to cents', () => {
  assert.equal(yuanInputToCents('1280'), 128000)
  assert.equal(yuanInputToCents('1280.5'), 128050)
  assert.equal(yuanInputToCents(''), null)
  assert.equal(yuanInputToCents('12.345'), null)
  assert.equal(yuanInputToCents('-5'), null)
})

test('validatePaymentDraft rejects negative or over-precision amounts', () => {
  const draft = hydratePaymentDraft(order(), timezone)
  assert.equal(validatePaymentDraft({ ...draft, amountPaidYuan: '-1' }, order()), '已收金额不能为负数')
  assert.equal(validatePaymentDraft({ ...draft, amountPaidYuan: '1.999' }, order()), '金额最多两位小数')
  assert.equal(validatePaymentDraft(draft, order()), null)
})

test('buildPaymentPatch sends amount, paid_at and due override', () => {
  const draft = {
    amountPaidYuan: '680',
    paidDate: '2026-07-14',
    dueDate: '2026-07-30',
    dueIsOverride: false,
  }
  const patch = buildPaymentPatch(draft, order(), timezone)
  assert.equal(patch.amount_paid, 68000)
  assert.equal(patch.paid_at, '2026-07-14T04:00:00Z')
  assert.equal(patch.delivery_due_at, '2026-07-30')
  assert.equal(patch.outstanding_amount, undefined)
})

test('buildPaymentPatch clears override when due date emptied on overridden order', () => {
  const draft = {
    amountPaidYuan: '300',
    paidDate: '',
    dueDate: '',
    dueIsOverride: true,
  }
  const patch = buildPaymentPatch(draft, order(), timezone)
  assert.equal(patch.delivery_due_at, null)
  assert.equal(patch.paid_at, undefined)
})

test('buildPaymentPatch omits unchanged fields', () => {
  const draft = hydratePaymentDraft(order(), timezone)
  const patch = buildPaymentPatch(draft, order(), timezone)
  assert.deepEqual(patch, {})
})

test('buildPaymentPatch resets amount to zero when input cleared to 0', () => {
  const draft = { amountPaidYuan: '0', paidDate: '', dueDate: '', dueIsOverride: false }
  const patch = buildPaymentPatch(draft, order({ amount_paid: 30000 }), timezone)
  assert.equal(patch.amount_paid, 0)
  assert.equal(patch.paid_at, undefined)
})

test('buildPaymentPatch sends paid_at only when date entered', () => {
  const draft = { amountPaidYuan: '', paidDate: '2026-07-15', dueDate: '', dueIsOverride: false }
  const patch = buildPaymentPatch(draft, order({ paid_at: undefined }), timezone)
  assert.equal(patch.paid_at, '2026-07-15T04:00:00Z')
  assert.equal(patch.amount_paid, undefined)
})
