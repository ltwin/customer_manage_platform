import { onCanvasInvalidate } from '../creative-canvas/editor/canvasEvents.ts'
import { currentAccount } from '../creative-canvas/editor/api.ts'
import { commandScene, readScene } from './api.ts'
import { modelCache } from './models/cache.ts'
import { SceneSession, sessionFor, type Clock, type Transport } from './state/sync.ts'

const browserClock: Clock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => window.setTimeout(fn, ms),
  clearTimeout: (handle) => window.clearTimeout(handle as number),
  online: () => navigator.onLine,
  hidden: () => document.hidden,
}

function transportFor(account: string | null): Transport {
  return {
    read: (id) => readScene(id),
    command: (id, intent, payload, keepalive) =>
      account
        ? commandScene(account, id, intent.id, intent.createdAt, payload, keepalive)
        : Promise.reject(new Error('请先登录')),
  }
}

/**
 * The session for a scene document, shared by every view of it. A view
 * calls attach() and detach() from an effect; after the last detach saving
 * continues until the queue is empty. It re-reads on the canvas page's
 * notifications.
 */
export function openScene(documentID: string): SceneSession {
  return sessionFor(documentID, () => {
    const session = new SceneSession(documentID, transportFor(currentAccount()), browserClock)
    session.onDispose.push(onCanvasInvalidate(() => session.invalidate()))
    // Baked models stay drawable under their real IDs once a receipt names them.
    session.onDispose.push(modelCache.follow(session))
    void session.start()
    return session
  })
}
