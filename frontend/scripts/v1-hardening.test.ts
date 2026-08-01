import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  type PageReadState,
} from '../src/components/pageReadState.ts'
import { createNoteSubmitGate, noteInputAction } from '../src/components/customers/noteInteraction.ts'
import {
  customerPickerActiveDescendant,
  customerPickerShouldClose,
  nextCustomerPickerIndex,
} from '../src/components/customers/customerPickerModel.ts'

const retry = () => undefined

test('page read states have one observable presentation in the fixed priority order', () => {
  const cases: Array<{
    state: PageReadState<string[]>
    expected: ReturnType<typeof pageReadPresentation>
  }> = [
    {
      state: { kind: 'unauthorized' },
      expected: { redirectToLogin: true, showReadyData: false, notice: null },
    },
    {
      state: { kind: 'loading', message: '正在读取客户' },
      expected: {
        redirectToLogin: false,
        showReadyData: false,
        notice: { kind: 'loading', message: '正在读取客户' },
      },
    },
    {
      state: { kind: 'error', message: '读取失败', retryable: true, onRetry: retry },
      expected: {
        redirectToLogin: false,
        showReadyData: false,
        notice: { kind: 'error', message: '读取失败', retryable: true, onRetry: retry },
      },
    },
    {
      state: { kind: 'error', message: '客户不存在', retryable: false },
      expected: {
        redirectToLogin: false,
        showReadyData: false,
        notice: { kind: 'error', message: '客户不存在', retryable: false },
      },
    },
    {
      state: { kind: 'empty', message: '当前筛选下没有客户' },
      expected: {
        redirectToLogin: false,
        showReadyData: false,
        notice: { kind: 'empty', message: '当前筛选下没有客户' },
      },
    },
    {
      state: { kind: 'ready', data: ['customer-1'], freshness: 'current' },
      expected: { redirectToLogin: false, showReadyData: true, notice: null },
    },
    {
      state: {
        kind: 'ready',
        data: ['customer-1'],
        freshness: 'stale',
        refreshError: '刷新失败，显示上次成功数据',
        onRetry: retry,
      },
      expected: {
        redirectToLogin: false,
        showReadyData: true,
        notice: {
          kind: 'refresh-error',
          message: '刷新失败，显示上次成功数据',
          onRetry: retry,
        },
      },
    },
  ]

  for (const item of cases) {
    assert.deepEqual(pageReadPresentation(item.state), item.expected)
  }
})

test('page-owned data becomes stale on refresh failure without becoming empty', () => {
  const current = completePageRead(['customer-1'], false, '暂无客户')
  const refreshing = beginPageRead(current, '正在读取客户', true)
  const stale = failPageRead(refreshing, '刷新失败', retry)

  assert.deepEqual(stale, {
    kind: 'ready',
    data: ['customer-1'],
    freshness: 'stale',
    refreshError: '刷新失败',
    onRetry: retry,
  })
  assert.equal(pageReadPresentation(stale).showReadyData, true)
  assert.equal(pageReadPresentation(stale).notice?.kind, 'refresh-error')
})

test('initial failure and successful empty remain mutually exclusive', () => {
  const loading: PageReadState<string[]> = { kind: 'loading', message: '正在读取客户' }
  assert.deepEqual(failPageRead(loading, '读取失败', retry), {
    kind: 'error',
    message: '读取失败',
    retryable: true,
    onRetry: retry,
  })
  assert.deepEqual(completePageRead([], true, '当前筛选下暂无客户'), {
    kind: 'empty',
    message: '当前筛选下暂无客户',
  })
})

test('customer results keep one page-owned query across the 768/769 responsive boundary', () => {
  const pageSource = readFileSync(new URL('../src/pages/CustomersPage.tsx', import.meta.url), 'utf8')
  const resultSource = readFileSync(new URL('../src/components/customers/CustomerResult.tsx', import.meta.url), 'utf8')
  const cssSource = readFileSync(new URL('../src/v1-hardening.css', import.meta.url), 'utf8')

  assert.equal((pageSource.match(/listCustomers\(/g) ?? []).length, 1)
  assert.doesNotMatch(resultSource, /listCustomers/)
  assert.match(pageSource, /<CustomerResult[\s\S]*customer=\{customer\}/)
  assert.match(cssSource, /\.customer-mobile-results\s*\{\s*display:\s*none;/)
  assert.match(
    cssSource,
    /@media \(max-width: 768px\)[\s\S]*\.customers-desktop-results\s*\{\s*display:\s*none;[\s\S]*\.customer-mobile-results\s*\{\s*display:\s*grid;/,
  )
})

test('note keyboard actions separate submit, IME composition, multiline, and cancel', () => {
  assert.equal(noteInputAction({ key: 'Enter', isComposing: false }), 'submit')
  assert.equal(noteInputAction({ key: 'Enter', isComposing: true }), 'none')
  assert.equal(noteInputAction({ key: 'Enter', isComposing: false, shiftKey: true, multiline: true }), 'none')
  assert.equal(noteInputAction({ key: 'Escape', isComposing: false }), 'cancel')
  assert.equal(noteInputAction({ key: 'Tab', isComposing: false }), 'none')
})

test('note submission gate rejects a repeated submit until the active request settles', () => {
  const gate = createNoteSubmitGate()

  assert.equal(gate.tryStart(), true)
  assert.equal(gate.tryStart(), false)
  gate.finish()
  assert.equal(gate.tryStart(), true)
})

test('empty customer picker keyboard navigation never exposes an invalid active option', () => {
  assert.equal(nextCustomerPickerIndex(null, 0, 'next'), null)
  assert.equal(nextCustomerPickerIndex(0, 0, 'previous'), null)
  assert.equal(customerPickerActiveDescendant('picker', null, 0), undefined)
  assert.equal(customerPickerActiveDescendant('picker', -1, 0), undefined)
  assert.equal(nextCustomerPickerIndex(null, 2, 'next'), 0)
  assert.equal(customerPickerActiveDescendant('picker', 0, 2), 'picker-option-0')
  assert.equal(customerPickerShouldClose(true), false)
  assert.equal(customerPickerShouldClose(false), true)
})

test('mobile calendar keeps v2 write actions reachable while dense detail content stays bounded', () => {
  const workspaceSource = readFileSync(new URL('../src/pages/calendar/CalendarWorkspace.tsx', import.meta.url), 'utf8')
  const detailSource = readFileSync(new URL('../src/pages/calendar/DayDetailPanel.tsx', import.meta.url), 'utf8')
  const calendarCssSource = readFileSync(new URL('../src/pages/calendar/calendar.css', import.meta.url), 'utf8')
  const hardeningCssSource = readFileSync(new URL('../src/v1-hardening.css', import.meta.url), 'utf8')

  assert.match(workspaceSource, /className="calendar-v2-fab"[\s\S]*onClick=\{\(\) => onCreate\(createDate\)\}/)
  assert.match(detailSource, /onClick=\{onCreate\}[\s\S]*在这天加档期/)
  assert.match(detailSource, /onClick=\{\(\) => onEdit\(slot\)\}>编辑<\/button>/)
  assert.match(detailSource, /onClick=\{\(\) => onDelete\(slot\)\}>删除<\/button>/)
  assert.doesNotMatch(workspaceSource, /desktop-schedule-actions/)
  assert.doesNotMatch(detailSource, /desktop-schedule-actions/)
  assert.match(
    calendarCssSource,
    /\.calendar-v2-workspace\[data-layout-mode="bottom-sheet"\] \.calendar-v2-detail\s*\{[\s\S]*max-height:\s*min\(78vh, 720px\);/,
  )
  assert.match(calendarCssSource, /\.calendar-v2-detail-body,[\s\S]*overflow-y:\s*auto;/)
  assert.match(
    calendarCssSource,
    /\.calendar-v2-workspace\[data-layout-mode="bottom-sheet"\] \.calendar-v2-fab\s*\{[\s\S]*right:\s*max\(72px, env\(safe-area-inset-right\)\);[\s\S]*display:\s*grid;/,
  )
  assert.match(
    hardeningCssSource,
    /@media \(max-width: 768px\) and \(pointer: coarse\)[\s\S]*min-height:\s*44px;[\s\S]*min-width:\s*44px;/,
  )
})

test('mobile shell keeps Customers and Calendar direct with safe-area and touch-size contracts', () => {
  const shellSource = readFileSync(new URL('../src/components/AppShell.tsx', import.meta.url), 'utf8')
  const baseCssSource = readFileSync(new URL('../src/index.css', import.meta.url), 'utf8')
  const hardeningCssSource = readFileSync(new URL('../src/v1-hardening.css', import.meta.url), 'utf8')

  assert.match(shellSource, /label: '客户', to: '\/customers'/)
  assert.match(shellSource, /label: '档期', to: '\/calendar'/)
  assert.match(shellSource, /<nav className="bottom-nav" aria-label="主导航">/)
  assert.match(baseCssSource, /\.bottom-nav\s*\{[\s\S]*env\(safe-area-inset-bottom\)/)
  assert.match(baseCssSource, /\.bottom-nav a\s*\{[\s\S]*min-height:\s*48px;/)
  assert.match(
    hardeningCssSource,
    /@media \(max-width: 768px\) and \(pointer: coarse\)[\s\S]*\.bottom-nav a,[\s\S]*min-width:\s*44px;/,
  )
})
