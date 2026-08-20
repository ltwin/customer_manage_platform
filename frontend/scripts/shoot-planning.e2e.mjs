import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { basename, join, resolve } from 'node:path'
import process from 'node:process'
import { chromium } from 'playwright'

const baseURL = (process.env.PLANNING_E2E_BASE_URL ?? 'http://localhost:5173').replace(/\/$/, '')
const email = process.env.PLANNING_E2E_EMAIL ?? ''
const password = process.env.PLANNING_E2E_PASSWORD ?? ''
const evidenceDir = process.env.PLANNING_E2E_EVIDENCE_DIR
  ? resolve(process.env.PLANNING_E2E_EVIDENCE_DIR)
  : mkdtempSync(join(tmpdir(), 'planning-v1-e2e-'))
mkdirSync(evidenceDir, { recursive: true, mode: 0o700 })

const browser = await chromium.launch({ headless: process.env.PLANNING_E2E_HEADED !== '1' })
const scenarios = []
const failureCases = []
const h1h2Rows = []
const responsiveRows = []
let planID = ''
let accessToken = ''
let storageState

const sha256 = (value) => createHash('sha256').update(value).digest('hex')

function writeEvidence(name, value) {
  writeFileSync(join(evidenceDir, name), `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 })
}

function recordScenario(id, assertions) {
  scenarios.push({ id, status: 'passed', assertions })
}

function recordFailure(id, oracle, evidence) {
  failureCases.push({ id, status: 'passed', oracle, evidence })
}

function consoleErrorAllowed(message, allowlist) {
  return /favicon/i.test(message) || allowlist.some((pattern) => pattern.test(message))
}

async function observePage(page, scenario, { coarse = false, scale = 1, consoleErrorAllowlist = [] } = {}) {
  const consoleErrors = []
  const pageErrors = []
  const onConsole = (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  }
  const onPageError = (error) => pageErrors.push(error.message)
  page.on('console', onConsole)
  page.on('pageerror', onPageError)
  try {
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(500)
    const snapshot = await page.evaluate(() => {
      const root = document.documentElement
      const labels = [...document.querySelectorAll('label')]
      const controls = [...document.querySelectorAll('input, select, textarea')]
      const unlabeledControls = controls.filter((control) => {
        if (control.getAttribute('aria-label') || control.getAttribute('aria-labelledby')) return false
        if (control.id && labels.some((label) => label.htmlFor === control.id)) return false
        return !control.closest('label')
      }).length
      const mobileAccountTrigger = document.querySelector('[data-account-menu-slot="mobile"] .account-menu-trigger')
      const accountRect = mobileAccountTrigger?.getBoundingClientRect()
      const intersectsAccountTrigger = accountRect && accountRect.width > 0 && accountRect.height > 0
        ? [...document.querySelectorAll('.topbar h1, .topbar .crumb, .topbar-actions button, .topbar-actions a')].some((element) => {
            const rect = element.getBoundingClientRect()
            return rect.width > 0 && rect.height > 0 &&
              rect.left < accountRect.right && rect.right > accountRect.left &&
              rect.top < accountRect.bottom && rect.bottom > accountRect.top
          })
        : false
      return {
        clientWidth: root.clientWidth,
        scrollWidth: root.scrollWidth,
        innerWidth: window.innerWidth,
        visualViewportScale: window.visualViewport?.scale ?? 1,
        devicePixelRatio: window.devicePixelRatio,
        mainCount: document.querySelectorAll('main').length,
        h1Count: document.querySelectorAll('h1').length,
        landmarkCount: document.querySelectorAll('main, header, nav, aside, footer').length,
        statusRegionCount: document.querySelectorAll('[role="status"], [aria-live]').length,
        unlabeledControls,
        topbarAccountOverlap: Boolean(intersectsAccountTrigger),
        bodyText: document.body.innerText,
      }
    })
    if (snapshot.scrollWidth > snapshot.clientWidth + 1) {
      await page.screenshot({ path: join(evidenceDir, `${scenario}-overflow.png`), fullPage: true })
    }
    assert.ok(snapshot.scrollWidth <= snapshot.clientWidth + 1, `${scenario}: horizontal overflow ${snapshot.scrollWidth}/${snapshot.clientWidth}`)
    assert.ok(snapshot.mainCount >= 1, `${scenario}: missing main landmark`)
    assert.ok(snapshot.h1Count >= 1, `${scenario}: missing h1`)
    assert.equal(snapshot.unlabeledControls, 0, `${scenario}: unlabeled form controls`)
    assert.equal(snapshot.topbarAccountOverlap, false, `${scenario}: mobile account trigger overlaps topbar content`)
    assert.doesNotMatch(snapshot.bodyText, /(?:评审注释|fail-closed|\bG1\b|\bG2\b|\bG3\b)/, `${scenario}: engineering text leaked into UI`)

    const physicalWidth = snapshot.innerWidth * snapshot.devicePixelRatio
    const effectiveCSSWidth = snapshot.clientWidth / snapshot.visualViewportScale
    if (scale === 2) {
      assert.ok(snapshot.innerWidth <= 640, `${scenario}: 200% equivalent did not reduce innerWidth (${snapshot.innerWidth})`)
      assert.ok(snapshot.clientWidth <= 640, `${scenario}: 200% equivalent did not reduce clientWidth (${snapshot.clientWidth})`)
      assert.equal(snapshot.visualViewportScale, 1, `${scenario}: unexpected visual-only page scale`)
      assert.equal(snapshot.devicePixelRatio, 2, `${scenario}: 200% evidence must use DPR 2`)
      assert.equal(physicalWidth, 1280, `${scenario}: physical width must remain 1280px`)
      assert.ok(effectiveCSSWidth <= 640, `${scenario}: effective CSS width did not enter narrow reflow`)
    }

    const focusableCount = await page.locator('a[href]:visible, button:visible, input:visible, select:visible, textarea:visible, [tabindex]:visible').count()
    if (focusableCount > 0) {
      await page.keyboard.press('Tab')
      const focused = await page.evaluate(() => document.activeElement !== document.body && document.activeElement !== document.documentElement)
      assert.equal(focused, true, `${scenario}: keyboard focus did not enter the page`)
    }

    let undersized = []
    if (coarse) {
      undersized = await page.locator('main button:visible, header button:visible, [role="tab"]:visible').evaluateAll((nodes) => nodes
        .map((node) => {
          const rect = node.getBoundingClientRect()
          return { name: (node.getAttribute('aria-label') || node.textContent || '').trim().slice(0, 60), width: rect.width, height: rect.height }
        })
        .filter((item) => item.width < 44 || item.height < 44))
      assert.deepEqual(undersized, [], `${scenario}: coarse pointer targets below 44px`)
    }

    await page.waitForTimeout(100)
    assert.deepEqual(pageErrors, [], `${scenario}: page errors`)
    assert.deepEqual(consoleErrors.filter((message) => !consoleErrorAllowed(message, consoleErrorAllowlist)), [], `${scenario}: console errors`)
    const screenshot = join(evidenceDir, `${scenario}.png`)
    await page.screenshot({ path: screenshot, fullPage: true })
    const row = {
      scenario,
      status: 'passed',
      viewport: {
        width: page.viewportSize()?.width ?? 0,
        height: page.viewportSize()?.height ?? 0,
        scale,
        coarse,
        device_scale_factor: snapshot.devicePixelRatio,
      },
      geometry: { client_width: snapshot.clientWidth, scroll_width: snapshot.scrollWidth },
      layout: {
        inner_width: snapshot.innerWidth,
        client_width: snapshot.clientWidth,
        visual_viewport_scale: snapshot.visualViewportScale,
        device_pixel_ratio: snapshot.devicePixelRatio,
        physical_width: physicalWidth,
        effective_css_width: effectiveCSSWidth,
      },
      semantics: {
        landmarks: snapshot.landmarkCount,
        h1: snapshot.h1Count,
        status_regions: snapshot.statusRegionCount,
        unlabeled_controls: snapshot.unlabeledControls,
      },
      undersized_targets: undersized,
      screenshot: basename(screenshot),
    }
    responsiveRows.push(row)
    return row
  } finally {
    page.off('console', onConsole)
    page.off('pageerror', onPageError)
  }
}

async function login(page) {
  await page.goto(`${baseURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByLabel('邮箱').fill(email)
  await page.getByLabel('登录密码').fill(password)
  const responsePromise = page.waitForResponse((response) => response.url().includes('/api/v1/auth/login') && response.request().method() === 'POST')
  await page.getByRole('button', { name: '登录' }).click()
  const response = await responsePromise
  assert.equal(response.status(), 200, 'login API failed')
  const grant = await response.json()
  assert.match(grant.access_token, /^\S+$/, 'login response omitted access token')
  accessToken = grant.access_token
  await page.waitForURL(/\/dashboard(?:$|\?)/, { timeout: 15_000 })
  recordScenario('authenticated-login', ['login API 200', 'dashboard navigation completed'])
}

async function createPlan(page, unique) {
  await page.goto(`${baseURL}/shoot-plans`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: '拍摄策划', exact: true }).waitFor()
  await page.getByRole('button', { name: /新建策划/ }).click()
  const dialog = page.getByRole('dialog', { name: '新建拍摄策划' })
  await dialog.getByLabel('标题').fill(`Hardening E2E ${unique}`)
  await dialog.getByLabel('拍摄主体').fill('Synthetic planning acceptance')
  await dialog.getByRole('button', { name: '创建并打开' }).click()
  await page.waitForURL(/\/shoot-plans\/[^/?#]+$/, { timeout: 15_000 })
  planID = decodeURIComponent(new URL(page.url()).pathname.split('/').at(-1) ?? '')
  assert.match(planID, /^spl_/, 'created plan id is not canonical')
  await page.getByRole('tab', { name: '经营草稿', exact: true }).waitFor()
  recordScenario('plan-create', ['canonical plan ID assigned', 'workspace loaded'])
}

async function saveBrief(page) {
  await page.getByLabel('作品').fill('逆光叙事')
  await page.getByLabel('角色').fill('主角')
  await page.getByLabel('创作 brief').fill('以克制的正面与侧身动作表达人物状态。')
  await page.getByLabel('情绪与氛围').fill('清晰、冷静、有层次')
  await page.getByLabel('视觉关键词').fill('逆光、人物、叙事')
  await page.getByRole('button', { name: '保存 brief' }).click()
  await page.getByRole('status').filter({ hasText: '已保存最新版本' }).waitFor()
  recordScenario('brief-save', ['brief mutation acknowledged', 'workspace revision reloaded'])
}

async function addShotAndReadiness(page) {
  await page.getByRole('tab', { name: /^镜头表/ }).click()
  await page.getByRole('button', { name: '＋ 新增镜头' }).click()
  const shotDialog = page.getByRole('dialog', { name: '新增镜头' })
  await shotDialog.getByLabel('镜头标题').fill('站姿主镜')
  await shotDialog.getByLabel('场景').fill('窗边')
  await shotDialog.getByLabel('动作').fill('站姿正面')
  await shotDialog.getByRole('button', { name: '保存镜头' }).click()
  await shotDialog.waitFor({ state: 'hidden' })
  await page.getByRole('heading', { name: '站姿主镜', exact: true }).waitFor()

  await page.getByRole('tab', { name: /^准备项/ }).click()
  await page.getByRole('button', { name: '＋ 新增准备项' }).click()
  const readinessDialog = page.getByRole('dialog', { name: '新增准备项' })
  await readinessDialog.getByLabel('标题').fill('灯架已到位')
  await readinessDialog.getByLabel('层级').selectOption('required')
  await readinessDialog.getByLabel('拍摄前核对').selectOption('checked')
  await readinessDialog.getByRole('button', { name: '保存准备项' }).click()
  await readinessDialog.waitFor({ state: 'hidden' })
  await page.getByRole('heading', { name: '灯架已到位', exact: true }).waitFor()

  await page.getByRole('tab', { name: /^镜头表/ }).click()
  const shotCard = page.locator('article').filter({ has: page.getByRole('heading', { name: '站姿主镜', exact: true }) })
  const linkButton = shotCard.getByRole('button', { name: '灯架已到位' })
  await linkButton.click()
  const linkElement = await linkButton.elementHandle()
  assert.ok(linkElement, 'readiness link button disappeared')
  await page.waitForFunction((element) => element.classList.contains('active'), linkElement)
  recordScenario('shot-readiness-create-link', ['shot created', 'required checked readiness created', 'explicit shot link persisted'])
}

async function ingestConversation(page) {
  await page.getByRole('button', { name: '从聊天整理' }).click()
  await page.waitForURL(/\/ingestions\/new$/)
  await page.getByLabel('聊天记录 / 备忘录').fill('要带灯架\n\n站姿正面\n\n侧身回头\n\nhttps://example.com/reference')
  const loadedSessionResponse = page.waitForResponse((response) => /\/ingestion-sessions\/ing_[^/]+$/.test(response.url()) && response.request().method() === 'GET')
  await page.getByRole('button', { name: '解析并查看候选' }).click()
  await page.waitForURL(/\/ingestions\/(?!new$)[^/?#]+$/)
  assert.equal((await loadedSessionResponse).status(), 200, 'created ingestion session did not reload')
  await page.getByRole('heading', { name: '确认候选', exact: true }).waitFor()
  assert.equal(await page.getByLabel('候选标题').count(), 3, 'parser did not produce three content candidates')
  const reference = page.locator('.ingestion-reference')
  await reference.first().waitFor()
  const referenceCount = await reference.count()
  assert.equal(referenceCount, 1, 'parser did not produce one reference candidate')
  await page.getByRole('button', { name: '查看保存摘要' }).click()
  await page.getByRole('heading', { name: '确认保存', exact: true }).waitFor()
  const commitResponsePromise = page.waitForResponse((response) => response.url().includes('/ingestion-sessions/') && response.url().endsWith('/commit'))
  await page.getByRole('button', { name: '确认保存' }).click()
  const commitResponse = await commitResponsePromise
  assert.equal(commitResponse.status(), 200, 'ingestion commit API failed')
  const commitOutcome = await Promise.race([
    page.getByRole('heading', { name: '已保存摄取结果', exact: true }).waitFor().then(() => null),
    page.getByRole('alert').last().waitFor().then(async () => page.getByRole('alert').last().innerText()),
  ])
  if (commitOutcome) throw new Error(`ingestion commit rejected: ${commitOutcome}`)
  await page.getByRole('button', { name: '返回策划工作台' }).click()
  await page.waitForURL(new RegExp(`/shoot-plans/${planID}$`))
  await page.getByRole('tab', { name: /^镜头表 3$/ }).waitFor()
  await page.getByRole('tab', { name: /^镜头表/ }).click()
  await page.getByRole('heading', { name: '站姿正面', exact: true }).waitFor()
  await page.getByRole('heading', { name: '侧身回头', exact: true }).waitFor()
  recordScenario('ingestion-atomic-commit', ['three candidates previewed', 'one reference previewed', 'atomic commit visible in workspace'])
}

async function verifyDraftRunRejected(page) {
  await page.goto(`${baseURL}/shoot-plans/${encodeURIComponent(planID)}/run`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: '无法进入现场模式' }).waitFor()
  await page.getByText('只有已就绪或拍摄中的策划可以进入现场模式。', { exact: true }).waitFor()
  recordFailure('draft-run-rejected', 'server rejects invalid transition before opening a run session', 'browser received stable invalid-transition UI')
  await page.getByRole('link', { name: '返回工作台' }).click()
  await page.waitForURL(new RegExp(`/shoot-plans/${planID}$`))
}

async function verifyBusinessUnavailable(page) {
  await page.getByRole('tab', { name: '经营草稿', exact: true }).click()
  await page.getByRole('button', { name: '重新生成草稿' }).click()
  await page.getByText(/无法生成经营草稿：订单价格草稿：需要先关联订单/).waitFor()
  await page.getByText('保存复杂度事实后生成草稿。', { exact: true }).first().waitFor()
  recordFailure('business-without-order', 'unlinked plan remains without an order draft', 'browser showed order_required and empty draft')
}

async function createSyntheticCRM(context, unique) {
  const origin = new URL(baseURL).origin
  const headers = { Authorization: `Bearer ${accessToken}`, Origin: origin }
  const customerName = `规划验收-${unique}`
  const customerResponse = await context.request.post(`${baseURL}/api/v1/customers`, {
    headers,
    data: {
      display_name: customerName,
      channel: 'xiaohongshu',
      identities: [{ platform: 'wechat', handle: `planning-${unique}` }],
    },
  })
  assert.equal(customerResponse.status(), 201, 'synthetic customer creation failed')
  const customer = await customerResponse.json()
  const orderTitle = `规划订单-${unique}`
  const orderResponse = await context.request.post(`${baseURL}/api/v1/orders`, {
    headers: { ...headers, 'Idempotency-Key': `planning-e2e-order-${unique}` },
    data: { creation_mode: 'new', customer_id: customer.id, title: orderTitle, price: 100000, status: 'consulting' },
  })
  assert.equal(orderResponse.status(), 201, 'synthetic order creation failed')
  const order = await orderResponse.json()
  return { customerName, orderTitle, customerRef: sha256(customer.id), orderRef: sha256(order.id) }
}

async function linkCRMAndGenerateBusiness(page, crm) {
  await page.getByRole('tab', { name: '创作 brief', exact: true }).click()
  await page.getByLabel('搜索客户').fill(crm.customerName)
  await page.getByRole('button', { name: '搜索客户' }).click()
  await page.getByRole('button', { name: `关联 ${crm.customerName}` }).click()
  await page.getByRole('button', { name: `关联订单 ${crm.orderTitle}` }).waitFor()
  await page.getByRole('button', { name: `关联订单 ${crm.orderTitle}` }).click()
  await page.getByText(crm.orderTitle, { exact: false }).first().waitFor()

  await page.getByRole('tab', { name: '经营草稿', exact: true }).click()
  await page.getByLabel('付费场地数').fill('1')
  await page.getByLabel('助理人数').fill('1')
  await page.getByLabel('精修张数').fill('12')
  await page.getByLabel('预估时长（分钟）').fill('120')
  await page.getByRole('button', { name: '保存事实' }).click()
  await page.getByRole('status').filter({ hasText: '经营事实已保存' }).waitFor()
  await page.getByLabel('绝对目标价（元，可选）').fill('1200')
  await page.getByRole('button', { name: '重新生成草稿' }).click()
  await page.getByRole('status').filter({ hasText: '订单价格草稿已生成' }).waitFor()
  const orderDraft = page.locator('article').filter({ has: page.getByRole('heading', { name: '订单价格草稿', exact: true }) })
  await orderDraft.getByText('待确认', { exact: true }).waitFor()
  recordScenario('crm-business-draft', ['synthetic customer and order linked through UI', 'private facts saved', 'fresh order draft shown as pending confirmation'])
}

async function issueAndRevokeProposal(page) {
  await page.getByRole('tab', { name: '分享协作', exact: true }).click()
  const proposal = page.locator('section').filter({ has: page.getByRole('heading', { name: '方案概览档', exact: true }) })
  await proposal.getByRole('button', { name: '签发链接' }).click()
  const secretDialog = page.getByRole('dialog', { name: '请立刻保存分享链接' })
  const oneTimeURL = await secretDialog.getByLabel('完整分享链接').inputValue()
  assert.match(oneTimeURL, /^https?:\/\/[^\s]+\/shared\/plans\/[^\s]+$/, 'one-time share URL is malformed')
  await secretDialog.getByRole('checkbox').check()
  await secretDialog.getByRole('button', { name: '关闭' }).click()
  await secretDialog.waitFor({ state: 'hidden' })

  const anonymous = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light', locale: 'zh-CN' })
  const anonymousPage = await anonymous.newPage()
  await anonymousPage.goto(oneTimeURL, { waitUntil: 'domcontentloaded' })
  await anonymousPage.getByText('方案概览', { exact: true }).first().waitFor()
  const anonymousText = await anonymousPage.locator('body').innerText()
  assert.doesNotMatch(anonymousText, /(?:经营草稿|订单价格草稿|绝对目标价|付费场地数|助理人数|精修张数|规划订单-)/)
  assert.doesNotMatch(anonymousText, /站姿主镜|站姿正面|侧身回头/)
  h1h2Rows.push({ id: 'proposal-anonymous-business-redaction', status: 'passed', oracle: 'proposal DOM excludes private business and shot content' })

  await proposal.getByRole('button', { name: '撤销' }).click()
  const confirm = page.getByRole('dialog', { name: '撤销方案概览档分享？' })
  await confirm.getByRole('button', { name: '确认' }).click()
  await proposal.getByText('已撤销', { exact: true }).waitFor()
  await anonymousPage.goto(oneTimeURL, { waitUntil: 'domcontentloaded' })
  await anonymousPage.getByRole('heading', { name: '这个链接已经失效了' }).waitFor()
  const revokedText = await anonymousPage.locator('body').innerText()
  assert.doesNotMatch(revokedText, /(?:撤销|过期|归档|经营草稿|订单价格|站姿主镜)/)
  await anonymous.close()
  recordFailure('proposal-revoked-uniform-404', 'revoked anonymous URL presents the uniform unavailable state', 'anonymous browser contains no revocation reason or private fields')
  recordScenario('proposal-issue-read-revoke', ['one-time URL captured only in memory', 'anonymous proposal redaction passed', 'revoked URL became uniformly unavailable'])
}

async function saveResult(page, label) {
  const actionButton = page.getByRole('button', { name: label })
  if (label === '清除本镜结果' && await actionButton.isDisabled()) {
    throw new Error('clear action disabled for ' + await page.locator('.run-shot-card h2').innerText())
  }
  const responsePromise = page.waitForResponse((response) => response.url().includes('/capture') && response.request().method() === 'POST')
  await actionButton.click()
  const response = await responsePromise
  const body = await response.json()
  assert.equal(response.status(), 201, 'run result API failed')
  if (label === '清除本镜结果') assert.equal(body.current_outcome ?? null, null, 'clear response retained an outcome')
  if (label === '✓ 完成拍摄') assert.equal(body.current_outcome?.result, 'captured', 'capture response projected the wrong outcome')
  if (label === '跳过本镜') assert.equal(body.current_outcome?.result, 'skipped', 'skip response projected the wrong outcome')
  const savedMessage = label === '✓ 完成拍摄' ? '已保存：完成拍摄' : label === '跳过本镜' ? '已保存：本镜跳过' : '已保存：结果已清除'
  await page.getByRole('status').filter({ hasText: savedMessage }).waitFor()
}

async function goToShot(page, title) {
  const order = ['站姿主镜', '站姿正面', '侧身回头']
  const seen = []
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const current = (await page.locator('.run-shot-card h2').innerText()).trim()
    seen.push(current)
    if (current === title) return
    const next = page.getByRole('button', { name: '下一镜 →' })
    const previous = page.getByRole('button', { name: '← 上一镜' })
    const currentPosition = order.indexOf(current)
    const targetPosition = order.indexOf(title)
    if (targetPosition >= 0 && currentPosition >= 0 && targetPosition < currentPosition && !(await previous.isDisabled())) await previous.click()
    else if (!(await next.isDisabled())) await next.click()
    else if (!(await previous.isDisabled())) await previous.click()
    else assert.fail('cannot navigate to ' + title + ' from ' + current)
    await page.waitForTimeout(100)
  }
  throw new Error('run mode did not reach shot ' + title + '; seen=' + seen.join(' -> '))
}

async function runExecutionFlow(context, page) {
  await page.getByRole('button', { name: '标记已就绪' }).click()
  await page.getByRole('button', { name: '进入 Run Mode' }).waitFor()
  await page.getByRole('button', { name: '进入 Run Mode' }).click()
  await page.waitForURL(/\/run$/)
  await page.getByRole('heading', { name: '站姿主镜', exact: true }).waitFor()

  await context.setOffline(true)
  await page.getByRole('button', { name: '✓ 完成拍摄' }).click()
  await page.getByRole('alert').filter({ hasText: '未保存' }).waitFor()
  assert.equal(await page.getByText('已保存：完成拍摄', { exact: true }).count(), 0, 'offline mutation showed false success')
  await context.setOffline(false)
  await saveResult(page, '✓ 完成拍摄')
  recordFailure('run-offline-retry', 'offline attempt showed unsaved state; retry after reconnect succeeded', 'same UI action retried without a false captured state')

  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.locator('.run-shot-card h2').waitFor()
  await goToShot(page, '站姿主镜')
  await saveResult(page, '清除本镜结果')
  await goToShot(page, '站姿主镜')
  await saveResult(page, '✓ 完成拍摄')

  await goToShot(page, '站姿正面')
  await page.getByLabel('跳过原因').selectOption('time_insufficient')
  await saveResult(page, '跳过本镜')
  await goToShot(page, '站姿正面')
  await saveResult(page, '✓ 完成拍摄')

  await goToShot(page, '侧身回头')
  await page.getByLabel('跳过原因').selectOption('other')
  await page.getByLabel('现场备注').fill('客户临时有事，这一镜改期补拍')
  await saveResult(page, '跳过本镜')
  await saveResult(page, '清除本镜结果')
  await saveResult(page, '✓ 完成拍摄')
  await page.getByText('全部镜头已有结果', { exact: true }).waitFor()

  await page.getByRole('link', { name: '退出现场模式' }).click()
  await page.getByRole('tab', { name: '执行历史', exact: true }).click()
  const history = page.locator('.planning-history-item')
  assert.equal(await history.count(), 8, 'execution event history did not preserve every successful transition')
  assert.equal(await history.getByText('已捕获', { exact: true }).count(), 4)
  assert.equal(await history.getByText('已跳过', { exact: true }).count(), 2)
  assert.equal(await history.getByText('已清除', { exact: true }).count(), 2)
  recordFailure('execution-history-supersession', 'captured, cleared, skipped, and replacement events remain append-only', '8 events visible with 4 captured, 2 skipped, and 2 cleared')
  recordScenario('run-mode-execution', ['offline failure recovered', 'three shots ended captured', 'append-only history retained superseded outcomes'])
}

async function runViewport(name, options, pagePaths) {
  const context = await browser.newContext({
    viewport: options.viewport,
    deviceScaleFactor: options.deviceScaleFactor ?? 1,
    hasTouch: options.coarse ?? false,
    isMobile: options.coarse ?? false,
    colorScheme: 'light',
    locale: 'zh-CN',
    storageState,
  })
  const page = await context.newPage()
  for (const item of pagePaths) {
    await page.goto(`${baseURL}${item.path}`, { waitUntil: 'domcontentloaded' })
    if (item.waitFor) await page.getByRole(item.waitFor.role, { name: item.waitFor.name, exact: item.waitFor.exact }).waitFor()
    await observePage(page, `${name}-${item.id}`, {
      coarse: options.coarse,
      scale: options.scale,
      consoleErrorAllowlist: item.consoleErrorAllowlist ?? [],
    })
  }
  storageState = await context.storageState()
  await context.close()
}

let overallStatus = 'passed'
let failureMessage = null
try {
  if (!email || !password) {
    overallStatus = 'blocked'
    failureMessage = 'PLANNING_E2E_EMAIL and PLANNING_E2E_PASSWORD are required for the authenticated business flow'
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: 'zh-CN' })
    const page = await context.newPage()
    await page.goto(`${baseURL}/login`, { waitUntil: 'domcontentloaded' })
    await observePage(page, 'public-1280-login')
    await context.close()
  } else {
    const unique = `${Date.now()}`
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light', locale: 'zh-CN' })
    const page = await context.newPage()
    await login(page)
    await createPlan(page, unique)
    await observePage(page, 'authenticated-create-1280')
    await saveBrief(page)
    await addShotAndReadiness(page)
    await ingestConversation(page)
    await verifyDraftRunRejected(page)
    await verifyBusinessUnavailable(page)
    const crm = await createSyntheticCRM(context, unique)
    await linkCRMAndGenerateBusiness(page, crm)
    await issueAndRevokeProposal(page)
    await runExecutionFlow(context, page)
    storageState = await context.storageState()
    await context.close()

    h1h2Rows.push(
      { id: 'proposal-revoked-uniform-unavailable', status: 'passed', oracle: 'revoked proposal exposes neither reason nor private fields' },
      { id: 'authenticated-private-business-only', status: 'passed', oracle: 'business draft was reachable only from the authenticated detail tab' },
      { id: 'h1-dedicated-no-plan-account', status: 'not_executed', oracle: 'requires a separately provisioned account that has never created a plan' },
      { id: 'full-share-feedback-assignment', status: 'not_executed', oracle: 'requires eligible shared-shoot evidence and remains in the owner acceptance gate' },
    )

    const pages = [
      { id: 'index', path: '/shoot-plans', waitFor: { role: 'heading', name: '拍摄策划', exact: true } },
      { id: 'workspace', path: `/shoot-plans/${encodeURIComponent(planID)}`, waitFor: { role: 'tab', name: '经营草稿', exact: true } },
      { id: 'run', path: `/shoot-plans/${encodeURIComponent(planID)}/run`, waitFor: { role: 'heading', name: '站姿主镜', exact: true } },
      {
        id: 'expired-share',
        path: '/shared/plans/invalid-hardening-token',
        waitFor: { role: 'heading', name: '这个链接已经失效了', exact: true },
        consoleErrorAllowlist: [/^Failed to load resource: the server responded with a status of 404 \(Not Found\)$/],
      },
    ]
    await runViewport('desktop-1600', { viewport: { width: 1600, height: 1000 } }, pages)
    await runViewport('desktop-1280', { viewport: { width: 1280, height: 900 } }, pages)
    await runViewport('mobile-375', { viewport: { width: 375, height: 812 }, deviceScaleFactor: 2, coarse: true }, pages)
    await runViewport('zoom-200', { viewport: { width: 640, height: 450 }, deviceScaleFactor: 2, scale: 2 }, pages)
  }
} catch (error) {
  overallStatus = 'failed'
  failureMessage = error instanceof Error ? error.message : String(error)
  scenarios.push({ id: 'runner', status: 'failed', assertions: [], error: failureMessage })
} finally {
  await browser.close()
}

const report = {
  schema: 'planning-v1-e2e-results-v1',
  base_origin: new URL(baseURL).origin,
  authenticated: Boolean(email && password),
  plan_ref: planID ? `sha256-${sha256(planID)}` : null,
  credential_material_persisted: false,
  trace_material_persisted: false,
  scenarios,
  overall_status: overallStatus,
  failure: failureMessage,
}
const failureMatrix = {
  schema: 'planning-v1-failure-matrix-v1',
  cases: failureCases,
  executed_case_count: failureCases.length,
  coverage_complete: false,
  deferred_cases: [
    'ingestion-owner-fault-injection',
    'shared-asset-object-restart-late-delete',
    'reminder-generation-fence-resolution',
    'production-shaped-destructive-restore',
  ],
  overall_status: overallStatus === 'failed' ? 'failed' : failureCases.every((item) => item.status === 'passed') ? 'partial' : 'failed',
}
const h1h2Matrix = {
  schema: 'planning-v1-h1-h2-negative-matrix-v1',
  rows: h1h2Rows,
  forbidden_semantics: ['private business facts', 'order price', 'labor cost', 'shot detail in proposal', 'revocation reason'],
  overall_status: overallStatus === 'failed' ? 'failed' : h1h2Rows.some((row) => row.status === 'not_executed') ? 'partial' : 'passed',
}
const responsiveMatrix = {
  schema: 'planning-v1-prototype-responsive-a11y-matrix-v1',
  rows: responsiveRows,
  required_viewports: ['1600x1000', '1280x900', '375x812-coarse', '640x450-css-dpr2-200-percent'],
  automated_checks: ['horizontal overflow', 'main/h1 landmarks', 'form labels', 'keyboard entry', 'engineering text exclusion', 'coarse target sizing', '200 percent CSS reflow'],
  manual_checks_deferred: ['screen-reader announcement quality', 'high-light visual contrast'],
  overall_status: overallStatus === 'failed' ? 'failed' : responsiveRows.length === 17 ? 'partial' : overallStatus,
}

writeEvidence('planning_v1_e2e_results.json', report)
writeEvidence('planning_v1_failure_matrix.json', failureMatrix)
writeEvidence('planning_v1_h1_h2_negative_matrix.json', h1h2Matrix)
writeEvidence('planning_v1_prototype_responsive_a11y_matrix.json', responsiveMatrix)
console.log(`planning shoot E2E: ${overallStatus}; evidence=${evidenceDir}`)
process.exit(overallStatus === 'passed' ? 0 : overallStatus === 'blocked' ? 2 : 1)
