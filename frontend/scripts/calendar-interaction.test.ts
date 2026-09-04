import assert from 'node:assert/strict'
import test from 'node:test'

import { detailScrollEase, detailScrollTop } from '../src/pages/calendar/detailScroll.ts'
import {
  SLOT_HOVER_CARD_GAP,
  SLOT_HOVER_CARD_MARGIN,
  slotHoverCardPosition,
} from '../src/pages/calendar/slotHoverGeometry.ts'

const viewport = { width: 1440, height: 900 }
const card = { width: 272, height: 180 }

test('悬停卡默认贴在事项右侧', () => {
  const anchor = { top: 200, left: 400, width: 120, height: 24 }
  assert.deepEqual(slotHoverCardPosition(anchor, card, viewport), {
    left: 400 + 120 + SLOT_HOVER_CARD_GAP,
    top: 200,
  })
})

test('右侧放不下时翻到左侧', () => {
  const anchor = { top: 200, left: 1200, width: 120, height: 24 }
  assert.deepEqual(slotHoverCardPosition(anchor, card, viewport), {
    left: 1200 - SLOT_HOVER_CARD_GAP - card.width,
    top: 200,
  })
})

test('两侧都放不下时对齐事项左缘并夹回视口内', () => {
  const narrow = { width: 360, height: 640 }
  const position = slotHoverCardPosition({ top: 100, left: 40, width: 280, height: 24 }, card, narrow)
  assert.equal(position.left, 40)

  const pushed = slotHoverCardPosition({ top: 100, left: 200, width: 100, height: 24 }, card, narrow)
  assert.equal(pushed.left, narrow.width - card.width - SLOT_HOVER_CARD_MARGIN)
})

test('贴近视口底部时上移，不越界', () => {
  const anchor = { top: 860, left: 400, width: 120, height: 24 }
  const position = slotHoverCardPosition(anchor, card, viewport)
  assert.equal(position.top, viewport.height - card.height - SLOT_HOVER_CARD_MARGIN)
})

test('目标已完整可见时不滚动', () => {
  const top = detailScrollTop({
    scrollTop: 100,
    viewportHeight: 400,
    scrollHeight: 1200,
    elementTop: 150,
    elementHeight: 120,
  })
  assert.equal(top, 100)
})

test('目标在下方时滚到容器中间', () => {
  const top = detailScrollTop({
    scrollTop: 0,
    viewportHeight: 400,
    scrollHeight: 1200,
    elementTop: 700,
    elementHeight: 100,
  })
  assert.equal(top, 700 - (400 - 100) / 2)
})

test('滚动位置夹在 [0, scrollHeight - viewportHeight]', () => {
  const first = detailScrollTop({
    scrollTop: 300,
    viewportHeight: 400,
    scrollHeight: 1200,
    elementTop: 0,
    elementHeight: 80,
  })
  assert.equal(first, 0)

  const last = detailScrollTop({
    scrollTop: 0,
    viewportHeight: 400,
    scrollHeight: 1200,
    elementTop: 1120,
    elementHeight: 80,
  })
  assert.equal(last, 800)
})

test('缓动函数覆盖端点并且单调递增', () => {
  assert.equal(detailScrollEase(0), 0)
  assert.equal(detailScrollEase(1), 1)
  assert.equal(detailScrollEase(-0.5), 0)
  assert.equal(detailScrollEase(1.5), 1)
  assert.ok(detailScrollEase(0.25) < detailScrollEase(0.75))
  // 缓出：前半程走得比线性快
  assert.ok(detailScrollEase(0.5) > 0.5)
})

test('内容短于容器时不产生负偏移', () => {
  const top = detailScrollTop({
    scrollTop: 0,
    viewportHeight: 400,
    scrollHeight: 200,
    elementTop: 0,
    elementHeight: 600,
  })
  assert.equal(top, 0)
})
