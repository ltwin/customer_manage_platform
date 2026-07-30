import assert from 'node:assert/strict'
import test from 'node:test'

import {
  isPackagePriceYuanInputAllowed,
  packagePriceCentsToYuan,
  packagePriceYuanToCents,
  validatePackagePriceYuan,
} from '../src/pages/packagePrice.ts'

test('package price input accepts cents precision only', () => {
  assert.equal(isPackagePriceYuanInputAllowed('680'), true)
  assert.equal(isPackagePriceYuanInputAllowed('680.'), true)
  assert.equal(isPackagePriceYuanInputAllowed('680.5'), true)
  assert.equal(isPackagePriceYuanInputAllowed('680.55'), true)
  assert.equal(isPackagePriceYuanInputAllowed('0.05'), true)
  assert.equal(isPackagePriceYuanInputAllowed('12.345'), false)
  assert.equal(isPackagePriceYuanInputAllowed('0.004'), false)
  assert.equal(isPackagePriceYuanInputAllowed('-1'), false)
  assert.equal(isPackagePriceYuanInputAllowed('1e3'), false)
})

test('package price validation rejects unsupported precision', () => {
  assert.equal(validatePackagePriceYuan('0'), null)
  assert.equal(validatePackagePriceYuan('12.34'), null)
  assert.equal(validatePackagePriceYuan('680.'), null)
  assert.equal(validatePackagePriceYuan('12.345'), '基础价最多保留两位小数')
  assert.equal(validatePackagePriceYuan('0.004'), '基础价最多保留两位小数')
  assert.equal(validatePackagePriceYuan('21474836.47'), null)
  assert.equal(validatePackagePriceYuan('21474836.48'), '基础价超出范围')
})

test('package price conversion does not round invalid precision', () => {
  assert.equal(packagePriceYuanToCents('0'), 0)
  assert.equal(packagePriceYuanToCents('12'), 1200)
  assert.equal(packagePriceYuanToCents('12.3'), 1230)
  assert.equal(packagePriceYuanToCents('12.34'), 1234)
  assert.equal(packagePriceYuanToCents('680.'), 68000)
  assert.equal(packagePriceYuanToCents('0.05'), 5)
  assert.throws(() => packagePriceYuanToCents('12.345'), /precision exceeds cents/)
})

test('package price cents display preserves editable decimal text', () => {
  assert.equal(isPackagePriceYuanInputAllowed('680.'), true)
  assert.equal(packagePriceCentsToYuan(68000), '680')
  assert.equal(packagePriceCentsToYuan(68050), '680.5')
  assert.equal(packagePriceCentsToYuan(68055), '680.55')
  assert.equal(packagePriceCentsToYuan(5), '0.05')
})
