import assert from 'node:assert/strict'
import test from 'node:test'

import { formatCustomerTotalSpend } from '../src/pages/customerDetailMoney.ts'

test('customer total spend displays cents as a precise CNY amount', () => {
  assert.equal(formatCustomerTotalSpend(113800), '¥1,138.00')
  assert.equal(formatCustomerTotalSpend(120), '¥1.20')
  assert.equal(formatCustomerTotalSpend(5), '¥0.05')
  assert.equal(formatCustomerTotalSpend(0), '¥0.00')
})
