import test from 'node:test'
import assert from 'node:assert/strict'
import { previewID, resolvePreviewID, remapActions } from '../src/creative-canvas/editor/structuralPreview.ts'
import { SaveQueue } from '../src/creative-canvas/editor/queue.ts'
import { emptyJournal } from '../src/creative-canvas/editor/journal.ts'
import type {
  CanvasNode,
  Canvas,
  GraphAction,
} from '../src/creative-canvas/editor/api.ts'
import type { Journal, Job } from '../src/creative-canvas/editor/journal.ts'
import {
  fitMediaHeight,
  projectCanvas,
  type Receipt,
} from '../src/creative-canvas/editor/outbox.ts'
const error = (status: number, code = 'internal') =>
  Object.assign(new Error(code), { status, code })
function memory(initial = emptyJournal()) {
  let saved = structuredClone(initial)
  return {
    load: async () => structuredClone(saved),
    save: async (v: Journal) => {
      saved = structuredClone(v)
    },
    get: () => saved,
  }
}
test('persist before dispatch, unknown replay keeps exact operation and body', async () => {
  const storage = memory()
  const sent: Job[] = []
  const transport = {
    lookup: async () => {
      throw error(404)
    },
    send: async (job: Job) => {
      assert.equal(storage.get().job?.state, 'unknown')
      sent.push(job)
      if (sent.length === 1) throw new Error('lost receipt')
      return {}
    },
  }
  const q = new SaveQueue(storage, transport, () => {})
  await q.enqueue('/canvases/one/commands', {
    type: 'add_node',
    node_id: 'fixed',
  })
  assert.equal(q.value.job?.state, 'unknown')
  const next = new SaveQueue(storage, transport, () => {})
  await next.load()
  await next.flush()
  assert.equal(sent.length, 2)
  assert.equal(sent[0].body, sent[1].body)
  assert.equal(sent[0].operation, sent[1].operation)
  assert.equal(next.value.job, null)
})
test('a recovered receipt never dispatches again and clears only the submitted draft', async () => {
  const storage = memory()
  let sends = 0
  const q = new SaveQueue(
    storage,
    {
      lookup: async () => ({}),
      send: async () => {
        sends++
        throw new Error('offline')
      },
    },
    () => {},
  )
  await q.update({
    ...q.value,
    drafts: { n: { kind: 'text', title: '', value: 'submitted' } },
  })
  await q.enqueue('/canvases/a/commands', {}, 'n')
  await q.update({
    ...q.value,
    drafts: { n: { kind: 'text', title: '', value: 'new typing' } },
  })
  await q.flush()
  assert.equal(sends, 1)
  assert.equal(q.value.drafts.n.value, 'new typing')
})
test('conflict blocks subsequent commands and keeps draft; only explicit dismissal releases queue', async () => {
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => {
        throw error(404)
      },
      send: async () => {
        throw error(409, 'creative_revision_conflict')
      },
    },
    () => {},
  )
  await q.update({
    ...q.value,
    drafts: {
      n: { kind: 'text', title: '', value: 'mine', dataRevision: '1' },
    },
  })
  await q.enqueue('/canvases/a/commands', {}, 'n')
  assert.equal(q.value.job?.state, 'rejected')
  await assert.rejects(q.enqueue('/canvases/a/commands', {}))
  await q.dismissRejected()
  assert.equal(q.value.job, null)
  assert.equal(q.value.drafts.n.value, 'mine')
})
test('storage failure prevents any dispatch', async () => {
  let sends = 0
  const q = new SaveQueue(
    {
      load: async () => emptyJournal(),
      save: async () => {
        throw new Error('quota')
      },
    },
    {
      lookup: async () => ({}),
      send: async () => {
        sends++
      },
    },
    () => {},
  )
  await assert.rejects(q.enqueue('/assets', {}))
  assert.equal(sends, 0)
})
test('access denied during recovery cannot erase an uncertain operation', async () => {
  const storage = memory()
  const q = new SaveQueue(
    storage,
    {
      lookup: async () => {
        throw error(403)
      },
      send: async () => {
        throw new Error('network')
      },
    },
    () => {},
  )
  await q.enqueue('/assets', {})
  await q.flush()
  await q.dismissRejected()
  assert.equal(q.value.job?.state, 'unknown')
})

test('SPA teardown stops an old response from overwriting the next editor journal', async () => {
  const storage = memory()
  let finish!: () => void
  let started!: () => void
  const dispatched = new Promise<void>((resolve) => {
    started = resolve
  })
  const old = new SaveQueue(
    storage,
    {
      lookup: async () => ({}),
      send: async () => {
        started()
        await new Promise<void>((resolve) => {
          finish = resolve
        })
        return {}
      },
    },
    () => {},
  )
  await old.update({
    ...old.value,
    drafts: { n: { kind: 'text', title: '', value: 'old' } },
  })
  const pending = old.enqueue('/canvases/a/commands', {}, 'n')
  await dispatched
  await old.close()
  const fresh = new SaveQueue(
    storage,
    { lookup: async () => ({}), send: async () => ({}) },
    () => {},
  )
  await fresh.load()
  await fresh.flush()
  await fresh.update({
    ...fresh.value,
    drafts: { n: { kind: 'text', title: '', value: 'new after return' } },
  })
  finish()
  await pending
  assert.equal(storage.get().drafts.n.value, 'new after return')
})

test('completion returns the committed result for creation and recovered navigation', async () => {
  const result = { default_canvas_id: 'created-canvas' }
  const storage = memory()
  let offline = false
  const q = new SaveQueue(
    storage,
    {
      send: async () => {
        if (offline) throw new Error('lost response')
        return result
      },
      lookup: async () => result,
    },
    () => {},
  )
  assert.deepEqual(await q.enqueue('/projects', { name: '未命名项目' }), result)
  assert.equal(storage.get().job, null)
  offline = true
  assert.equal(await q.enqueue('/projects', { name: '未命名项目' }), undefined)
  assert.equal(storage.get().job?.state, 'unknown')
  assert.deepEqual(await q.flush(), result)
  assert.equal(storage.get().job, null)
})

const positionNode: CanvasNode = {
  id: 'n',
  parent_id: null,
  metadata: {
    type_key: 'core.text',
    type_version: 1,
    title: '',
    intent: '',
    x: 0,
    y: 0,
    width: 280,
    height: 180,
    z_order: 0,
  },
  prompt: null,
  capabilities: { actions: [], prompt_mode: 'draft', disabled_reason: null },
  status: {
    content_state: 'ready',
    generation_state: 'idle',
    active_execution_id: null,
    latest_execution_id: null,
    apply_state: null,
    error: null,
    status_revision: '1',
  },
  placement_revision: '1',
  data_revision: '1',
  data: {
    schema_version: 1,
    config: {},
    content_id: null,
    content_revision_id: null,
    selected_version_id: null,
    document_id: null,
  },
}
const positionCanvas: Canvas = {
  id: 'a',
  project_id: 'p',
  project_name: 'test',
  project_revision: '1',
  revision: '1',
  topology_revision: '1',
  archived: false,
  nodes: [positionNode],
  edges: [],
  node_inputs: [],
  changes: [],
  object_states: [],
}

const nodeResult = (id: string, placement: string, data = '1') => ({
  kind: 'node' as const,
  id,
  is_live: true,
  placement_revision: placement,
  data_revision: data,
})
const moveOf = (job: Job) =>
  JSON.parse(job.body).payload.actions.filter(
    (a: { type: string }) => a.type === 'move_node',
  )

test('drops during an in-flight move merge into one following command with the latest targets', async () => {
  const storage = memory()
  const sent: Job[] = []
  let release!: (value: unknown) => void
  const q = new SaveQueue(
    storage,
    {
      send: (job) => {
        sent.push(job)
        return new Promise((resolve) => {
          release = resolve
        })
      },
      lookup: async () => {
        throw error(404)
      },
    },
    () => {},
  )
  q.stageMoves(positionCanvas, [{ node: positionNode, x: 10, y: 20 }])
  await sleep()
  const first = q.sendNextIntent('a', positionCanvas)
  while (!release) await sleep()
  const immutable = q.value.job!.body
  const other = { ...positionNode, id: 'other' }
  const later = { ...positionCanvas, nodes: [positionNode, other] }
  q.stageMoves(later, [{ node: positionNode, x: 30, y: 40 }])
  q.stageMoves(later, [{ node: positionNode, x: 50, y: 60 }])
  q.stageMoves(later, [{ node: other, x: 70, y: 80 }])
  await sleep()
  assert.equal(
    q.value.job!.body,
    immutable,
    'in-flight request stays immutable',
  )
  assert.equal(sent.length, 1)
  assert.equal(q.outbox.length, 2, 'later drops share one pending intent')
  release(receipt('2', { object_results: [nodeResult('n', '2')] }))
  await first
  const view = projectCanvas(positionCanvas, q.outbox)
  assert.deepEqual(
    view.nodes.map((n) => [n.id, n.metadata.x, n.metadata.y]),
    [['n', 50, 60]],
    'the projection shows the latest drop before it is sent',
  )
  const second = q.sendNextIntent('a', positionCanvas)
  while (sent.length < 2) await sleep()
  const body = JSON.parse(sent[1].body).payload
  assert.deepEqual(moveOf(sent[1]), [
    { type: 'move_node', node_id: 'n', x: 50, y: 60 },
    { type: 'move_node', node_id: 'other', x: 70, y: 80 },
  ])
  assert.equal(
    body.read_set.find((r: { id: string }) => r.id === 'n').placement_revision,
    '2',
    'the second move reads the revision its own receipt produced',
  )
  release(
    receipt('3', {
      object_results: [nodeResult('n', '3'), nodeResult('other', '2')],
    }),
  )
  await second
  await q.pruneOutbox({ ...positionCanvas, revision: '3' })
  assert.equal(q.outbox.length, 0)
})

test('an unchanged drop stages nothing; dragging back after a receipt is a real move', async () => {
  const sent: Job[] = []
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => ({}),
      send: async (job) => (
        sent.push(job),
        receipt('2', { object_results: [nodeResult('n', '2')] })
      ),
    },
    () => {},
  )
  assert.equal(
    q.stageMoves(positionCanvas, [{ node: positionNode, x: 0, y: 0 }]),
    null,
  )
  const moved = q.stageMoves(positionCanvas, [
    { node: positionNode, x: 10, y: 20 },
  ])!
  await sleep()
  await q.sendNextIntent('a', positionCanvas)
  await moved.done
  // The polled snapshot still says (0, 0); the photographer sees (10, 20).
  const view = projectCanvas(positionCanvas, q.outbox)
  const back = q.stageMoves(view, [{ node: positionNode, x: 0, y: 0 }])
  assert(back, 'returning to the server coordinates is still a change')
  await sleep()
  await q.sendNextIntent('a', positionCanvas)
  await back.done
  assert.deepEqual(moveOf(sent[1]), [
    { type: 'move_node', node_id: 'n', x: 0, y: 0 },
  ])
})

test('a group drop moves only selection roots', async () => {
  const q = new SaveQueue(
    memory(),
    { lookup: async () => ({}), send: async () => receipt('2') },
    () => {},
  )
  const group = {
    ...positionNode,
    id: 'g',
    metadata: { ...positionNode.metadata, type_key: 'core.group' },
  }
  const child = { ...positionNode, id: 'child', parent_id: 'g' }
  const canvas = { ...positionCanvas, nodes: [group, child] }
  q.stageMoves(canvas, [
    { node: group, x: 100, y: 100 },
    { node: child, x: 5, y: 5 },
  ])
  await sleep()
  assert.deepEqual(q.outbox[0].actions, [
    { type: 'move_node', node_id: 'g', x: 100, y: 100 },
  ])
})

test('a move of a node that only exists locally follows its creation in order', async () => {
  const sent: Job[] = []
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => ({}),
      send: async (job) => {
        sent.push(job)
        return receipt(String(sent.length + 1), {
          created_ids: sent.length === 1 ? ['cwnode_new'] : [],
          object_results: [nodeResult('cwnode_new', String(sent.length))],
        })
      },
    },
    () => {},
  )
  const creation = q.stageIntent('a', {
    kind: 'actions',
    actions: [
      {
        type: 'add_node',
        node_id: 'cwnode_new',
        type_key: 'core.text',
        x: 1,
        y: 2,
      },
    ],
  })
  await sleep()
  const view = projectCanvas(positionCanvas, q.outbox)
  const created = view.nodes.find((n) => n.id === 'cwnode_new')!
  const move = q.stageMoves(view, [{ node: created, x: 40, y: 50 }])!
  await sleep()
  assert.equal(q.outbox.length, 2, 'a move never merges into a creation')
  await q.sendNextIntent('a', positionCanvas)
  await creation.done
  assert.equal(JSON.parse(sent[0].body).payload.actions[0].type, 'add_node')
  await q.sendNextIntent('a', positionCanvas)
  await move.done
  const body = JSON.parse(sent[1].body).payload
  assert.deepEqual(moveOf(sent[1]), [
    { type: 'move_node', node_id: 'cwnode_new', x: 40, y: 50 },
  ])
  assert.deepEqual(
    body.read_set.find((r: { id: string }) => r.id === 'cwnode_new'),
    {
      kind: 'node',
      id: 'cwnode_new',
      placement_revision: '1',
      data_revision: '1',
    },
  )
})

test('an unsynced drag from a journal written before moves joined the outbox is replayed', async () => {
  const sent: Job[] = []
  const legacy = {
    ...emptyJournal(),
    positions: {
      'a:n': {
        canvasID: 'a',
        nodeID: 'n',
        x: 9,
        y: 8,
        revision: '1',
        token: 't',
        synced: false,
      },
      'a:old': {
        canvasID: 'a',
        nodeID: 'old',
        x: 1,
        y: 1,
        revision: '1',
        token: 'u',
        synced: true,
      },
    },
  } as unknown as Journal
  const storage = memory(legacy)
  const q = new SaveQueue(
    storage,
    {
      lookup: async () => ({}),
      send: async (job) => (sent.push(job), receipt('2')),
    },
    () => {},
  )
  await q.load()
  assert.equal('positions' in storage.get(), false)
  assert.deepEqual(
    q.outbox.map((i) => i.actions),
    [[{ type: 'move_node', node_id: 'n', x: 9, y: 8 }]],
  )
  await q.sendNextIntent('a', positionCanvas)
  assert.equal(sent.length, 1)
})

test('normal graph reads exclude unrelated permanent identities', async () => {
  const { graphReadSet } =
    await import('../src/creative-canvas/editor/graph.ts')
  const canvas: Canvas = {
    ...positionCanvas,
    object_states: Array.from({ length: 5100 }, (_, i) => ({
      id: `deleted-${i}`,
      kind: 'node' as const,
      is_live: false,
      placement_revision: '1',
      data_revision: '1',
    })),
  }
  assert.deepEqual(
    graphReadSet(canvas).map((r) => r.id),
    canvas.nodes.map((n) => n.id),
  )
})

test('copy and remove read only the versions owned by their selected subtree', async () => {
  const { graphActionReadSet } =
    await import('../src/creative-canvas/editor/graph.ts')
  const group: CanvasNode = {
    ...positionNode,
    id: 'g',
    metadata: { ...positionNode.metadata, type_key: 'core.group' },
  }
  const child: CanvasNode = {
    ...positionNode,
    id: 'child',
    parent_id: 'g',
    data: { ...positionNode.data, selected_version_id: 'selected' },
  }
  const canvas: Canvas = {
    ...positionCanvas,
    nodes: [group, child],
    object_states: [
      {
        kind: 'version',
        id: 'selected',
        node_id: 'child',
        is_live: true,
        revision: '1',
      },
      {
        kind: 'version',
        id: 'history',
        node_id: 'child',
        is_live: true,
        revision: '1',
      },
      {
        kind: 'version',
        id: 'unrelated',
        node_id: 'elsewhere',
        is_live: true,
        revision: '1',
      },
      {
        kind: 'version',
        id: 'deleted',
        node_id: 'child',
        is_live: false,
        revision: '2',
      },
    ],
  }
  assert.deepEqual(
    graphActionReadSet(canvas, [
      { type: 'duplicate_selection', node_ids: ['g'] },
    ])
      .filter((r) => r.kind === 'version')
      .map((r) => r.id),
    ['selected'],
  )
  assert.deepEqual(
    graphActionReadSet(canvas, [{ type: 'remove_nodes', node_ids: ['g'] }])
      .filter((r) => r.kind === 'version')
      .map((r) => r.id),
    ['selected', 'history'],
  )
})

const connect: GraphAction = {
  type: 'connect_reference',
  source_node_id: positionNode.id,
  target_node_id: 'target',
  source_port: 'output',
  target_port: 'reference',
  role: 'reference',
}
const twoNodes: Canvas = {
  ...positionCanvas,
  nodes: [positionNode, { ...positionNode, id: 'target' }],
}
const receipt = (revision: string, extra: Partial<Receipt> = {}): Receipt => ({
  change_id: `ch${revision}`,
  inverse_of: null,
  result_revision: revision,
  result_topology_revision: revision,
  object_results: [],
  created_ids: [],
  omitted_reference_ids: [],
  ...extra,
})
const sleep = () => new Promise((resolve) => setImmediate(resolve))

test('a staged edit paints at once, sends in order and stays painted until the snapshot includes it', async () => {
  const storage = memory()
  const sent: Job[] = []
  let release!: (value: unknown) => void
  const q = new SaveQueue(
    storage,
    {
      lookup: async () => ({}),
      send: (job) => {
        sent.push(job)
        return new Promise((resolve) => {
          release = resolve
        })
      },
    },
    () => {},
  )
  const staged = q.stageIntent('a', { kind: 'actions', actions: [connect] })
  const removal = q.stageIntent('a', {
    kind: 'actions',
    actions: [
      { type: 'remove_nodes', node_ids: ['target'], group_mode: 'subtree' },
    ],
  })
  await sleep()
  const painted = projectCanvas(twoNodes, q.outbox)
  assert.equal(painted.edges.length, 0, 'the later removal also hides the edge')
  assert.equal(painted.nodes.length, 1)
  assert.equal(
    storage.get().outbox?.length,
    2,
    'both intents persisted before dispatch',
  )
  const sending = q.sendNextIntent('a', twoNodes)
  while (!release) await sleep()
  assert.equal(sent.length, 1)
  const body = JSON.parse(sent[0].body).payload
  assert.equal(body.type, 'batch')
  assert.deepEqual(body.actions, [connect])
  assert.equal(q.outbox[0].state, 'sent')
  assert.equal(
    await q.sendNextIntent('a', twoNodes),
    undefined,
    'one request at a time',
  )
  release(receipt('2'))
  await sending
  assert.equal((await staged.done).result_revision, '2')
  assert.equal(q.outbox[0].state, 'confirmed')
  const withReceipt = projectCanvas(twoNodes, q.outbox)
  assert.equal(
    withReceipt.changes[0]?.id,
    'ch2',
    'receipt joins the local history',
  )
  await q.pruneOutbox(twoNodes)
  assert.equal(
    q.outbox.length,
    2,
    'snapshot at revision 1 does not know the edit',
  )
  await q.pruneOutbox({ ...twoNodes, revision: '2' })
  assert.equal(q.outbox.length, 1)
  assert.equal(q.outbox[0].kind, 'actions')
  removal.done.catch(() => {})
})

test('the read set of an edit comes from the state before it, including earlier receipts', async () => {
  const sent: Job[] = []
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => ({}),
      send: async (job) => {
        sent.push(job)
        return receipt('2', {
          object_results: [
            {
              kind: 'node',
              id: 'n',
              is_live: true,
              placement_revision: '1',
              data_revision: '2',
            },
          ],
        })
      },
    },
    () => {},
  )
  const rename = q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'update_metadata', node_id: 'n', title: '新标题' }],
  })
  const removal = q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'remove_nodes', node_ids: ['n'], group_mode: 'subtree' }],
  })
  await sleep()
  await q.sendNextIntent('a', positionCanvas)
  await rename.done
  await q.sendNextIntent('a', positionCanvas)
  await removal.done
  const reads = JSON.parse(sent[1].body).payload.read_set
  assert.deepEqual(
    reads.find((r: { id: string }) => r.id === 'n'),
    { kind: 'node', id: 'n', placement_revision: '1', data_revision: '2' },
    'the removed node is read at the revision its own earlier edit produced',
  )
})

test('layout bursts merge into one command; delete then undo cancels locally without a request', async () => {
  const sent: Job[] = []
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => ({}),
      send: async (job) => (sent.push(job), receipt('2')),
    },
    () => {},
  )
  q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'resize_node', node_id: 'n', width: 300, height: 200 }],
  })
  q.stageIntent('a', {
    kind: 'actions',
    actions: [
      { type: 'move_node', node_id: 'n', x: 5, y: 6 },
      { type: 'resize_node', node_id: 'n', width: 320, height: 210 },
    ],
  })
  await sleep()
  assert.equal(q.outbox.length, 1)
  assert.deepEqual(q.outbox[0].actions, [
    { type: 'resize_node', node_id: 'n', width: 320, height: 210 },
    { type: 'move_node', node_id: 'n', x: 5, y: 6 },
  ])
  const removal = q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'remove_nodes', node_ids: ['n'], group_mode: 'subtree' }],
  })
  await sleep()
  assert.equal(projectCanvas(positionCanvas, q.outbox).nodes.length, 0)
  const cancelled = q.cancelTail('a')
  assert.equal(cancelled?.actions?.[0].type, 'remove_nodes')
  await assert.rejects(removal.done, /已在本机撤销/)
  await sleep()
  assert.equal(projectCanvas(positionCanvas, q.outbox).nodes.length, 1)
  assert.equal(sent.length, 0)
})

test('a rejected edit and everything after it on that canvas are withdrawn from the projection', async () => {
  const storage = memory()
  const q = new SaveQueue(
    storage,
    {
      lookup: async () => {
        throw error(404)
      },
      send: async () => {
        throw error(409, 'creative_revision_conflict')
      },
    },
    () => {},
  )
  const first = q.stageIntent('a', { kind: 'actions', actions: [connect] })
  const second = q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'update_metadata', node_id: 'n', title: '后来' }],
  })
  const elsewhere = q.stageIntent('b', { kind: 'actions', actions: [connect] })
  await sleep()
  await q.sendNextIntent('a', twoNodes)
  await assert.rejects(first.done, /撤回/)
  await assert.rejects(second.done, /撤回/)
  assert.equal(q.value.job?.state, 'rejected')
  assert.equal(q.outbox.length, 1)
  assert.equal(q.outbox[0].canvasID, 'b')
  assert.equal(projectCanvas(twoNodes, q.outbox).edges.length, 0)
  await q.dismissRejected()
  assert.equal(storage.get().job, null)
  elsewhere.done.catch(() => {})
})

test('an unsent intent survives reload and is sent by the next session', async () => {
  const storage = memory()
  const sent: Job[] = []
  const q = new SaveQueue(
    storage,
    {
      lookup: async () => ({}),
      send: async (job) => (sent.push(job), receipt('2')),
    },
    () => {},
  )
  q.stageIntent('a', { kind: 'actions', actions: [connect] })
  await sleep()
  await q.close()
  const next = new SaveQueue(
    storage,
    {
      lookup: async () => ({}),
      send: async (job) => (sent.push(job), receipt('2')),
    },
    () => {},
  )
  await next.load()
  assert.equal(projectCanvas(twoNodes, next.outbox).edges.length, 1)
  await next.sendNextIntent('a', twoNodes)
  assert.equal(sent.length, 1)
  assert.equal(next.outbox[0].state, 'confirmed')
})

test('media nodes from assets take the picture aspect ratio at the default width', () => {
  const content = {
    id: 'ccrv_1',
    content_id: 'ccnt_1',
    kind: 'image' as const,
    sequence: '1',
    payload: {},
    truncated: false,
    media: [
      {
        role: 'original',
        blob_id: 'b',
        mime: 'image/jpeg',
        byte_size: 1,
        width: 1000,
        height: 1500,
        duration_ms: null,
      },
    ],
  }
  assert.equal(fitMediaHeight(content), 74 + 420)
  assert.equal(fitMediaHeight({ ...content, kind: 'audio' }), 180)
  assert.equal(fitMediaHeight(undefined), 180)
  const view = projectCanvas(positionCanvas, [
    {
      id: 'i',
      canvasID: 'a',
      kind: 'actions',
      state: 'pending',
      preview: { cwnode_img: content },
      actions: [
        {
          type: 'add_node',
          node_id: 'cwnode_img',
          type_key: 'core.image',
          x: 0,
          y: 0,
        },
      ],
    },
  ])
  assert.equal(
    view.nodes.find((n) => n.id === 'cwnode_img')?.metadata.height,
    494,
  )
})

test('group preview preserves world positions and ungroup restores the layout before any request', () => {
  const canvas = {
    ...positionCanvas,
    nodes: [
      positionNode,
      {
        ...positionNode,
        id: 'b',
        metadata: { ...positionNode.metadata, x: 400, y: 100 },
      },
    ],
  }
  const q = new SaveQueue(
    memory(),
    {
      send: async () => {
        throw new Error('must stay local')
      },
      lookup: async () => ({}),
    },
    () => {},
  )
  q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'group_nodes', node_ids: ['n', 'b'] }],
  })
  const grouped = projectCanvas(canvas, q.outbox)
  const group = grouped.nodes.find(
    (n) => n.metadata.type_key === 'core.group',
  )!
  assert(group)
  assert.equal(group.metadata.x, -24)
  assert.equal(group.metadata.y, -52)
  for (const original of canvas.nodes) {
    const child = grouped.nodes.find((n) => n.id === original.id)!
    assert.equal(child.parent_id, group.id)
    assert.equal(child.metadata.x + group.metadata.x, original.metadata.x)
    assert.equal(child.metadata.y + group.metadata.y, original.metadata.y)
  }
  q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'ungroup_nodes', node_id: group.id }],
  })
  assert.deepEqual(projectCanvas(canvas, q.outbox).nodes, canvas.nodes)
})

test('duplicate preview copies the subtree and internal references immediately, then remaps later moves', async () => {
  const group = {
    ...positionNode,
    id: 'g',
    metadata: {
      ...positionNode.metadata,
      type_key: 'core.group',
      x: 100,
      y: 100,
    },
  }
  const child = { ...positionNode, id: 'child', parent_id: 'g' }
  const sibling = { ...positionNode, id: 'sibling', parent_id: 'g' }
  const edge = {
    id: 'edge',
    source_node_id: 'child',
    target_node_id: 'sibling',
    source_port: 'output',
    target_port: 'reference',
    role: 'reference',
    ordinal: 0,
    revision: '1',
  }
  const canvas = {
    ...positionCanvas,
    nodes: [group, child, sibling],
    edges: [edge, { ...edge, id: 'external', source_node_id: 'outside' }],
  }
  const sent: Job[] = []
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => ({}),
      send: async (job) => {
        sent.push(job)
        return receipt(String(sent.length + 1), {
          created_ids: ['cwnode_g', 'cwnode_child', 'cwnode_sibling'],
          id_mapping: {
            g: 'cwnode_g',
            child: 'cwnode_child',
            sibling: 'cwnode_sibling',
          },
        })
      },
    },
    () => {},
  )
  q.stageIntent('a', {
    kind: 'actions',
    actions: [
      {
        type: 'duplicate_selection',
        node_ids: ['g', 'child'],
        dx: 36,
        dy: 36,
      },
    ],
  })
  const preview = projectCanvas(canvas, q.outbox)
  assert.equal(sent.length, 0)
  assert.equal(preview.nodes.length, 6)
  assert.equal(preview.edges.length, 3, 'only the internal edge is copied')
  const copy = preview.nodes.find(
    (n) =>
      n.id.startsWith('pending:') && n.metadata.type_key === 'core.group',
  )!
  assert.equal(copy.metadata.x, 136)
  const copiedChild = preview.nodes.find((n) => n.parent_id === copy.id)!
  assert.equal(copiedChild.metadata.x, child.metadata.x)
  q.stageMoves(preview, [{ node: copy, x: 250, y: 300 }])
  await q.sendNextIntent('a', canvas)
  assert.deepEqual(q.outbox[1].actions, [
    { type: 'move_node', node_id: 'cwnode_g', x: 250, y: 300 },
  ])
  assert.equal(
    projectCanvas(canvas, q.outbox).nodes.find((n) => n.id === 'cwnode_g')
      ?.metadata.x,
    250,
  )
  await q.sendNextIntent('a', canvas)
  assert.equal(
    sent.length,
    1,
    'dependent command waits for authoritative copied versions and inputs',
  )
  const confirmed = {
    ...projectCanvas(canvas, q.outbox.slice(0, 1)),
    revision: '2',
  }
  await q.pruneOutbox(confirmed)
  await q.sendNextIntent('a', confirmed)
  assert.equal(sent.length, 2)
  assert(
    !sent[1].body.includes('pending:'),
    'temporary identifiers must never leave the browser',
  )
})

test('redo cancellation cannot drop an unrelated pending action; undo cannot cancel an undo', () => {
  const q = new SaveQueue(
    memory(),
    { send: async () => ({}), lookup: async () => ({}) },
    () => {},
  )
  q.stageMoves(positionCanvas, [{ node: positionNode, x: 10, y: 20 }])
  assert.equal(q.cancelTail('a', 'undo'), null)
  assert.equal(q.outbox.length, 1)
  assert(q.cancelTail('a'))
  q.stageIntent('a', { kind: 'undo' })
  assert.equal(q.cancelTail('a'), null)
  assert.equal(q.outbox.length, 1)
  assert(q.cancelTail('a', 'undo'))
})

test('group receipt remaps an immediate ungroup and rejects the entire dependent preview on conflict', async () => {
  let fail = false
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => ({}),
      send: async () => {
        if (fail) throw error(409, 'creative_revision_conflict')
        return receipt('2', { created_ids: ['cwnode_group'] })
      },
    },
    () => {},
  )
  q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'group_nodes', node_ids: ['n'] }],
  })
  const group = projectCanvas(positionCanvas, q.outbox).nodes.find(
    (n) => n.id !== 'n',
  )!
  q.stageIntent('a', {
    kind: 'actions',
    actions: [{ type: 'ungroup_nodes', node_id: group.id }],
  })
  await q.sendNextIntent('a', positionCanvas)
  assert.deepEqual(q.outbox[1].actions, [
    { type: 'ungroup_nodes', node_id: 'cwnode_group' },
  ])
  assert.deepEqual(
    projectCanvas(positionCanvas, q.outbox).nodes,
    positionCanvas.nodes,
  )
  const confirmed = {
    ...projectCanvas(positionCanvas, q.outbox.slice(0, 1)),
    revision: '2',
  }
  await q.pruneOutbox(confirmed)
  fail = true
  await q.sendNextIntent('a', confirmed)
  assert.equal(q.value.job?.state, 'rejected')
  assert.equal(q.outbox.length, 0)
  assert.equal(projectCanvas(confirmed, q.outbox), confirmed)
})


test('successive copies resolve nested provisional identities even when receipts share a render', () => {
  const first = previewID('first', 0, 'n')
  const second = previewID('second', 0, first)
  const updatedSecond = previewID('second', 0, 'cwnode_first')
  const aliases = { [first]: 'cwnode_first', [updatedSecond]: 'cwnode_second' }
  assert.equal(resolvePreviewID(second, aliases), 'cwnode_second')
  assert.deepEqual(remapActions([
    { type: 'move_node', node_id: second, x: 100, y: 200 },
    { type: 'update_metadata', node_id: first, title: first },
  ], aliases), [
    { type: 'move_node', node_id: 'cwnode_second', x: 100, y: 200 },
    { type: 'update_metadata', node_id: 'cwnode_first', title: first },
  ], 'identifier remapping must never rewrite literal text')
})
