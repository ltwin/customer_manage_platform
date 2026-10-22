// 活原型自检：分别以「声明卡片扩展」和「不声明」两种客户端连上服务，跑一遍主要工具。
// 先 node server.mjs，再 node selftest.mjs。只打印摘要，不断言。
import { Client, StreamableHTTPClientTransport } from '@modelcontextprotocol/client'

const URL_ = process.env.MCP_URL ?? 'http://127.0.0.1:8774/mcp'

async function run(label, withUi) {
  const capabilities = withUi ? { extensions: { 'io.modelcontextprotocol/ui': { mimeTypes: ['text/html;profile=mcp-app'] } } } : {}
  const client = new Client({ name: `selftest-${label}`, version: '0' }, { capabilities })
  await client.connect(new StreamableHTTPClientTransport(new URL(URL_)))
  console.log(`\n== ${label} ==`)
  console.log('instructions 开头：', (client.getInstructions() ?? '').split('\n')[0])
  const { tools } = await client.listTools()
  for (const t of tools) console.log(`  ${t.name.padEnd(18)} ${String(t.title).padEnd(8)} ui=${JSON.stringify(t._meta?.ui ?? null)}`)
  const dry = await client.callTool({ name: 'generate_node', arguments: { canvas_id: 'cv_9f2a', node_id: 'shot-2', count: 2, dry_run: true } })
  console.log('dry_run 文字：', dry.content[0].text.split('\n').slice(0, 2).join(' / '))
  console.log('dry_run _meta：', Object.keys(dry._meta ?? {}).join(',') || '无', '· structured：', Boolean(dry.structuredContent))
  const sub = await client.callTool({ name: 'generate_node', arguments: { canvas_id: 'cv_9f2a', node_id: 'shot-2', count: 2 } })
  const id = sub.structuredContent?.tasks?.[0]?.id
  const w = await client.callTool({ name: 'wait_tasks', arguments: { task_ids: [id], timeout_s: 2 } })
  console.log('wait_tasks（2 秒）：', w.content[0].text.split('\n')[0])
  if (withUi) {
    const r = await client.readResource({ uri: 'ui://creative/tasks/v1' })
    console.log('界面资源：', r.contents[0].mimeType, `${r.contents[0].text.length} 字符`, JSON.stringify(r.contents[0]._meta))
  } else {
    try {
      await client.readResource({ uri: 'ui://creative/tasks/v1' })
      console.log('界面资源：不该读到')
    } catch (e) {
      console.log('界面资源：读不到（符合预期）', String(e.message).slice(0, 60))
    }
  }
  const scene = await client.callTool({ name: 'render_view', arguments: { node_id: 'scene-1' } })
  console.log('render_view：', scene.content[0].text.split('\n')[0])
  await client.close()
}

await run('有卡片', true)
await run('无卡片', false)
