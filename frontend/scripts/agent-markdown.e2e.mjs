// Rendering acceptance against the actual component and project styles.
// Start Vite first, then: node scripts/agent-markdown.e2e.mjs
import assert from 'node:assert/strict'
import { readFile, writeFile, rm, mkdir } from 'node:fs/promises'
import { chromium } from 'playwright'
const fixture = new URL('../.markdown-qa.html', import.meta.url)
const output = process.env.MARKDOWN_EVIDENCE || '/tmp/agent-markdown-evidence'
await mkdir(output, { recursive: true })
await writeFile(
  fixture,
  `<html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="root" class="cc-workspace"></div><script type="module">
import React,{useState} from 'react';import {createRoot} from 'react-dom/client';
import '/src/index.css';import '/src/creative-canvas/editor/workspace.css';import '/src/creative-canvas/editor/agent.css';
import {AgentMarkdown} from '/src/creative-canvas/editor/AgentMarkdown.tsx';
function App(){const [message,setMessage]=useState({source:'普通消息',streaming:false});window.setMarkdown=(source,streaming=false)=>setMessage({source,streaming});return React.createElement('aside',{className:'cc-agent-panel'},React.createElement('header',null,'Markdown 验收'),React.createElement('div',{className:'cc-agent-history'},React.createElement(AgentMarkdown,message)))}
createRoot(document.getElementById('root')).render(React.createElement(App));
</script></body></html>`,
)
const browser = await chromium.launch({ headless: true })
const errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 950 } })
  page.on('pageerror', (error) => errors.push(error.message))
  let imageFailures = 0
  await page.route('https://media.example/**', async (route) => {
    const name = new URL(route.request().url()).pathname.split('/').at(-1)
    if (name === 'retry.png' && imageFailures++ === 0)
      return route.fulfill({ status: 404, body: 'missing' })
    if (name === 'bad.webm')
      return route.fulfill({ status: 404, body: 'missing' })
    const file = name === 'retry.png' ? 'image.png' : name
    const mime = {
      'image.png': 'image/png',
      'video.webm': 'video/webm',
      'audio.wav': 'audio/wav',
    }[file]
    if (!mime) return route.abort()
    return route.fulfill({
      contentType: mime,
      body: await readFile(
        new URL(`./fixtures/agent-media/${file}`, import.meta.url),
      ),
    })
  })
  await page.goto(
    `${process.env.CREATIVE_AGENT_URL || 'http://127.0.0.1:5179'}/.markdown-qa.html`,
  )
  await page.locator('.cc-agent-text').waitFor()
  async function show(source, streaming = false) {
    await page.evaluate(
      ({ source, streaming }) => window.setMarkdown(source, streaming),
      { source, streaming },
    )
  }
  const diagram =
    '```mermaid\ngraph TD\n A[确认主题] --> B[拍摄]\n B --> C[交付]\n```'
  const rich =
    '# 拍摄方案\n\n**重点**与 *斜体*、~~删除~~，查看 [说明](https://example.com)。\n\n> 保留环境层次\n\n- [x] 场地\n- [ ] 灯光\n  - 主光\n\n| 项目 | 费用 |\n| --- | ---: |\n| 灯光 | ¥300 |\n\n行内 $$E=mc^2$$，价格 $5 和 $10。\n\n$$\n\\frac{1}{f}=\\frac{1}{u}+\\frac{1}{v}\n$$\n\n' +
    diagram +
    '\n\n```typescript\nconst aperture = 2.8;\nconsole.log(aperture);\n```'
  await show(rich)
  await page.locator('.cc-md-diagram-image svg').waitFor()
  await page.locator('pre code span[style]').first().waitFor()
  assert.equal(await page.locator('table tbody tr').count(), 1)
  const textColor = await page
    .locator('.cc-agent-text')
    .evaluate((el) => getComputedStyle(el).color)
  assert.equal(
    await page
      .locator('td')
      .first()
      .evaluate((el) => getComputedStyle(el).color),
    textColor,
    'table text uses the chat theme',
  )
  assert.equal(await page.locator('.katex').count(), 2)
  assert.equal(await page.locator('ul ul').count(), 1)
  assert.equal(await page.locator('input[type=checkbox]').count(), 2)
  const colors = await page
    .locator('pre code span[style]')
    .evaluateAll((els) => [
      ...new Set(els.map((el) => getComputedStyle(el).color)),
    ])
  assert(
    colors.length >= 3,
    'syntax highlighting has actual colors without Tailwind',
  )
  assert.equal(
    await page.locator('a').getAttribute('href'),
    'https://example.com/',
  )
  assert.equal(
    await page
      .locator('strong')
      .evaluate((el) => getComputedStyle(el).fontWeight),
    '600',
  )
  await page.screenshot({ path: `${output}/desktop.png` })
  await page.setViewportSize({ width: 375, height: 812 })
  const log = page.locator('.cc-agent-history')
  await show(
    '| 项目 | 长说明 |\n| --- | --- |\n| A | ' +
      'LongUnbrokenValue'.repeat(40) +
      ' |\n\n$$\n' +
      Array(25).fill('x^2').join('+') +
      '\n$$',
  )
  await page.locator('.katex-display').waitFor()
  assert(
    await log.evaluate((el) => el.scrollWidth <= el.clientWidth + 1),
    'wide content stays within panel',
  )
  assert(
    await page
      .locator('.katex-display')
      .evaluate((el) => el.scrollWidth > el.clientWidth),
    'long formula scrolls independently',
  )
  await show(rich)
  await page.locator('.cc-md-diagram-image svg').waitFor()
  await page.screenshot({ path: `${output}/mobile.png` })
  await show(
    '> ' +
      diagram.replaceAll('\n', '\n> ') +
      '\n\n- 分镜\n\n  ' +
      diagram.replaceAll('\n', '\n  '),
  )
  await page.waitForFunction(
    () => document.querySelectorAll('.cc-md-diagram-image svg').length === 2,
  )
  const ids = await page
    .locator('.cc-md-diagram-image svg')
    .evaluateAll((els) => els.map((el) => el.id))
  assert.equal(
    new Set(ids).size,
    2,
    'repeated diagrams have distinct SVG identities',
  )
  await show('````mermaid\ngraph TD\n A-->B\n```', true)
  await page.getByText('图表正在接收…').waitFor()
  assert.equal(await page.locator('.cc-md-diagram-image svg').count(), 0)
  await show('~~~mermaid\ngraph TD\n A-->B\n~~~~')
  await page.locator('.cc-md-diagram-image svg').waitFor()
  await show('**未闭合\n\n```mermaid\ngraph TD\n A-->B', true)
  await page.getByText('图表正在接收…').waitFor()
  assert.equal(
    await page.locator('[data-diagram-pending] pre').innerText(),
    'graph TD\n A-->B',
  )
  await show('```mermaid\ngraph THIS IS INVALID\n A-->>{\n```')
  await page.getByText('图表无法显示，保留源码').waitFor()
  assert(
    (await page.locator('.cc-md-fallback pre').innerText()).includes(
      'THIS IS INVALID',
    ),
  )
  await show('$$\\unknownCommand{x}$$')
  await page.locator('.katex').waitFor()
  assert(
    (await page.locator('.cc-agent-text').innerText()).includes(
      'unknownCommand',
    ),
  )
  await show(
    '<script>window.__bad=1</script>\n\n[bad](javascript:alert(1))\n\n![不可用](javascript:alert%281%29)\n\n安全正文',
  )
  await page.getByText('安全正文', { exact: true }).waitFor()
  assert.equal(await page.locator('.cc-agent-markdown script').count(), 0)
  assert.equal(await page.locator('.cc-agent-markdown img').count(), 0)
  assert.equal(await page.locator('a[href^="javascript:"]').count(), 0)
  assert.equal(await page.evaluate(() => window.__bad), undefined)
  // Actual media decoding/playback, failure recovery and stable streaming nodes.
  const mediaSource =
    '![拍摄参考](https://media.example/image.png)\n\n[视频样片](https://media.example/video.webm?signature=example)\n\n[配乐试听](https://media.example/audio.wav)'
  await show(mediaSource, true)
  await page.waitForFunction(
    () => document.querySelector('.cc-md-image')?.naturalWidth === 1280,
  )
  const video = page.locator('.cc-md-media video')
  const audio = page.locator('.cc-md-media audio')
  for (const player of [video, audio]) {
    await player.scrollIntoViewIfNeeded()
    await page.waitForFunction(
      (selector) => document.querySelector(selector)?.readyState >= 2,
      await player.evaluate((el) => el.tagName.toLowerCase()),
    )
    assert.equal(
      await player.evaluate((el) => el.controls && !el.autoplay && el.paused),
      true,
    )
    await player.evaluate(async (el) => {
      el.dataset.probe = 'stable'
      await el.play()
    })
    await page.waitForFunction(
      (selector) => document.querySelector(selector)?.currentTime > 0,
      await player.evaluate((el) => el.tagName.toLowerCase()),
    )
    await player.evaluate((el) => el.pause())
  }
  await show(mediaSource + '\n\n继续生成文字', true)
  await page.getByText('继续生成文字').waitFor()
  assert.equal(await video.getAttribute('data-probe'), 'stable')
  assert.equal(await audio.getAttribute('data-probe'), 'stable')
  assert.equal(
    await log.evaluate((el) => el.scrollWidth <= el.clientWidth + 1),
    true,
  )
  assert.equal(
    await page.locator('.cc-md-image').getAttribute('referrerpolicy'),
    'no-referrer',
  )
  await page.screenshot({ path: `${output}/media-mobile.png` })
  await page.setViewportSize({ width: 1280, height: 950 })
  await page.screenshot({ path: `${output}/media-desktop.png` })
  await show('![重试图片](https://media.example/retry.png)')
  await page.getByText('图片加载失败：重试图片').waitFor()
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await page.waitForFunction(
    () => document.querySelector('.cc-md-image')?.naturalWidth === 1280,
  )
  assert.equal(imageFailures, 2)
  await show('[失效视频](https://media.example/bad.webm)')
  await page.getByText('媒体无法播放，可打开原链接查看。').waitFor()
  assert.equal(
    await page.getByRole('link', { name: '失效视频' }).getAttribute('href'),
    'https://media.example/bad.webm',
  )
  await show('[恢复视频](https://media.example/video.webm)')
  await page.waitForFunction(
    () => document.querySelector('video')?.readyState >= 2,
  )
  const failedPage = await browser.newPage()
  await failedPage.route('**/*streamdown*mermaid*', (route) =>
    route.abort('failed'),
  )
  await failedPage.goto(
    `${process.env.CREATIVE_AGENT_URL || 'http://127.0.0.1:5179'}/.markdown-qa.html`,
  )
  await failedPage.locator('.cc-agent-text').waitFor()
  await failedPage.evaluate(
    (source) => window.setMarkdown(source),
    '## 正文保持可读\n\n' + diagram,
  )
  await failedPage.getByText('图表无法显示，保留源码').waitFor()
  assert.equal(
    await failedPage.getByRole('heading', { name: '正文保持可读' }).count(),
    1,
  )
  await failedPage.close()
  assert.deepEqual(errors, [])
  console.log(
    'PASS: rich Markdown, computed styles, 375px overflow, nested/duplicate diagrams, actual fence closure, malformed content, inert HTML/URLs, image recovery and real audio/video playback',
  )
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
