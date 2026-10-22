// 假的 MCP 服务：只在页面内存里推进任务，返回 shared/results.mjs 的结果。
// 每个宿主一份实例，四个宿主互不影响。
import * as R from '../shared/results.mjs'

const GEN_MS = 6000 // 生成约 6 秒完成（真实约 20 秒）
const RENDER_MS = 4000 // 页面打开后回看约 4 秒完成

export class MockServer {
  constructor(host) {
    this.host = host
    this.tasks = new Map()
    this.pageOpenedAt = 0
    this.seq = 0
  }

  // 创作者打开了创意空间页面：排队的场景任务被页面认领
  openPage() {
    if (!this.pageOpenedAt) this.pageOpenedAt = Date.now()
  }

  #advance(t) {
    const now = Date.now()
    if (t.kind === 'generate') {
      t.progress = Math.min(1, (now - t.startedAt) / GEN_MS)
      t.status = t.progress >= 1 ? 'succeeded' : 'running'
    } else if (!this.pageOpenedAt) {
      t.status = 'waiting_for_page'
    } else {
      t.progress = Math.min(1, (now - Math.max(this.pageOpenedAt, t.startedAt)) / RENDER_MS)
      t.status = t.progress >= 1 ? 'succeeded' : 'running'
    }
    return t
  }

  #task(kind) {
    const id = `${kind === 'generate' ? 'cwexec' : 'csjob'}_${(++this.seq).toString(36)}${Math.random().toString(36).slice(2, 5)}`
    const t = { id, kind, status: 'queued', progress: 0, startedAt: Date.now() }
    this.tasks.set(id, t)
    return this.#advance(t)
  }

  async call(name, args = {}) {
    await new Promise((r) => setTimeout(r, name === 'wait_tasks' ? 900 : 250))
    switch (name) {
      case 'get_capabilities':
        return R.getCapabilities({ pageOpen: Boolean(this.pageOpenedAt), host: this.host })
      case 'list_canvases':
        return R.listCanvases()
      case 'read_canvas':
        return R.readCanvas()
      case 'set_node_prompt':
        return R.setNodePrompt()
      case 'generate_node':
        return args.dry_run ? R.generateDryRun() : R.generateSubmit(this.#task('generate'))
      case 'wait_tasks':
        return R.waitTasks((args.task_ids ?? []).map((id) => this.#advance(this.tasks.get(id))).filter(Boolean))
      case 'edit_scene':
        return R.editScene({ host: this.host })
      case 'render_view':
        return R.renderView(this.#task('render'))
      case 'export_shots':
        return R.exportShots()
      case 'save_to_library':
        return R.saveToLibrary(R.GEN.count)
      default:
        return { isError: true, content: [{ type: 'text', text: `未知工具 ${name}` }] }
    }
  }
}
