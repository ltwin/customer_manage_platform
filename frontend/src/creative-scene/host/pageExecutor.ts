import { discardCandidate, uploadGenerated } from '../../creative-canvas/editor/media.ts'
import { iframeFactory, SandboxHost } from '../sandbox/host.ts'
import { renderThumbnail } from '../sandbox/thumbnail.ts'
import { SceneExecutor } from './executor.ts'
import type { JobApi } from './jobs.ts'
import type { PageRunner } from './launcher.ts'
import { readScene } from '../api.ts'
import { renderShots } from './exportShots.ts'
import { loadModels, openShooter, renderView } from './render.ts'
import { backgroundTicker } from './ticker.ts'

// The executor as a page runs it, loaded (with three.js) on the first job:
// its own sandbox frame, started with the first bake, and Worker-paced
// timers for renewals, upload polling and sandbox timeouts, so a hidden tab
// keeps its pace.

export function createPageExecutor(api: JobApi, deviceLost: () => void): PageRunner {
  const ticker = backgroundTicker()
  let sandbox: SandboxHost | null = null
  const executor = new SceneExecutor({
    api,
    ticker,
    compile: (input) => {
      sandbox ??= new SandboxHost(iframeFactory(document.body), undefined, ticker.after)
      return sandbox.compile(input)
    },
    thumbnail: renderThumbnail,
    upload: (file, declared, signal) => uploadGenerated(file, declared, { kind: 'staged' }, signal, ticker.wait),
    discard: (upload) =>
      upload.publication?.kind === 'candidate' ? discardCandidate(upload.publication.id, upload.publication.revision) : Promise.resolve(),
    render: (documentID, request) => renderView(documentID, request),
    exportImages: (documentID, plan) =>
      renderShots({ read: readScene, loadModels: (doc) => loadModels(doc.scene, doc.models), open: (doc, look) => openShooter(doc.scene, doc.models, look) }, documentID, plan),
    deviceLost,
  })
  return {
    run: (job) => executor.run(job),
    dispose: () => {
      sandbox?.dispose()
      sandbox = null
    },
  }
}
