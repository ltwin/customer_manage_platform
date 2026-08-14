import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(path: string): string {
  return readFileSync(new URL(path, import.meta.url), 'utf8')
}

test('CRM mutation request uses generated union types and never accepts account scope from callers', () => {
  const api = source('../src/planning/api.ts')
  const schema = source('../src/api/schema.d.ts')

  assert.match(api, /export type ShootPlanMutationRequest = components\['schemas'\]\['ShootPlanMutationRequest'\]/)
  assert.match(api, /body: ShootPlanMutationRequest/)
  assert.match(schema, /ShootPlanMutationRequest/)
  assert.match(schema, /PlanningSummary/)
  assert.match(schema, /link_customer/)
  assert.doesNotMatch(api, /account_id/)
})

test('workbench CRM card keeps customer then order then future shoot slot and weak-coupling copy', () => {
  const panel = source('../src/planning/panels/CrmLinkPanel.tsx')
  const brief = source('../src/planning/panels/BriefPanel.tsx')

  assert.match(brief, /<CrmLinkPanel plan=\{plan\} busy=\{busy\} runCommand=\{runCommand\} \/>/)
  assert.match(panel, /data-testid="crm-link-card"/)
  assert.match(panel, /<h2>CRM 关联<\/h2>/)
  assert.match(panel, /弱耦合：关联动作不会推动订单或档期状态。/)
  assert.match(panel, /<dt>客户<\/dt>/)
  assert.match(panel, /<dt>订单<\/dt>/)
  assert.match(panel, /<dt>未来拍摄档期<\/dt>/)
  const customerAt = panel.indexOf('<dt>客户</dt>')
  const orderAt = panel.indexOf('<dt>订单</dt>')
  const slotAt = panel.indexOf('<dt>未来拍摄档期</dt>')
  assert.ok(customerAt >= 0 && customerAt < orderAt && orderAt < slotAt)
})

test('order_deleted tombstone does not depend on order_id and still offers relink', () => {
  const panel = source('../src/planning/panels/CrmLinkPanel.tsx')

  assert.match(panel, /crm\?\.order_id \|\| crm\?\.state === 'order_deleted'/)
  assert.match(panel, /订单已删除，当前引用已清空/)
  assert.doesNotMatch(panel, /crm\.state !== 'order_deleted'/)
  assert.match(panel, /crm\?\.customer_id && !crm\.order_id && orders\.length > 0/)
  assert.match(panel, /const pageSize = 100/)
  assert.match(panel, /items\.length >= result\.total/)
})

test('unlink confirmation states full share invalidation, reminder withdrawal, and no order or slot side effects', () => {
  const panel = source('../src/planning/panels/CrmLinkPanel.tsx')

  assert.match(panel, /完整档分享链接会立即失效（不降级）/)
  assert.match(panel, /认领提醒会被撤销/)
  assert.match(panel, /订单和档期本身不受任何影响/)
  assert.match(panel, /role="alertdialog"/)
})

test('CRM pages omit planning summary DOM when there is no plan and never invent an empty-state prompt', () => {
  const link = source('../src/components/PlanningSummaryLink.tsx')
  const customers = source('../src/pages/CustomersPage.tsx')
  const customerResult = source('../src/components/customers/CustomerResult.tsx')
  const customerDetail = source('../src/pages/CustomerDetailPage.tsx')
  const orders = source('../src/components/orders/OrderWorkspace.tsx')
  const dayDetail = source('../src/pages/calendar/DayDetailPanel.tsx')

  assert.match(link, /if \(!summary \|\| summary\.plan_count < 1 \|\| !summary\.primary_plan\) return null/)
  assert.match(link, /components\['schemas'\]\['PlanningSummary'\]/)
  for (const page of [customers, customerResult, customerDetail, orders, dayDetail]) {
    assert.match(page, /<PlanningSummaryLink summary=\{/)
    assert.doesNotMatch(page, /暂无策划|去创建策划|还没有策划|尚未关联策划/)
  }
})

test('CRM association conflict copy and CSS contracts stay aligned with the generated 409 codes', () => {
  const presentation = source('../src/planning/presentation.ts')
  const css = source('../src/planning/planning.css')

  assert.match(presentation, /customer_link_conflict/)
  assert.match(presentation, /order_link_conflict/)
  assert.match(presentation, /source_changed/)
  assert.match(presentation, /order_still_linked/)
  assert.match(css, /\.planning-crm-kv/)
  assert.match(css, /\.planning-crm-confirm button:focus-visible/)
  assert.match(css, /@media \(max-width: 768px\)[\s\S]*\.planning-side-stack \{ grid-template-columns: 1fr; \}/)
})
