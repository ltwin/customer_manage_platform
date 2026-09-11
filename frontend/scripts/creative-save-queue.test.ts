import test from 'node:test'
import assert from 'node:assert/strict'
import { SaveQueue } from '../src/creative-canvas/editor/queue.ts'
import { emptyJournal } from '../src/creative-canvas/editor/journal.ts'
import type { CanvasNode, Canvas } from '../src/creative-canvas/editor/api.ts'
import type { Journal, Job } from '../src/creative-canvas/editor/journal.ts'
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

test('positions coalesce while a request is in flight and advance only on its receipt', async () => {
  const storage = memory()
  const sent: Job[] = []
  let release!: (result: unknown) => void
  const q = new SaveQueue(
    storage,
    {
      send: async (job) => {
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
  await q.stagePosition('a', positionNode, 10, 20)
  const first = q.sendNextPosition('a')
  while (!release) await new Promise((resolve) => setImmediate(resolve))
  const immutable = q.value.job!.body
  await q.stagePosition('a', positionNode, 30, 40)
  await q.stagePosition('a', positionNode, 50, 60)
  await q.stagePosition('a', { ...positionNode, id: 'other' }, 70, 80)
  assert.equal(q.value.job!.body, immutable)
  assert.equal(sent.length, 1)
  release({ placement_revision: '2' })
  await first
  assert.equal(q.value.positions!['a:n'].synced, false)
  const second = q.sendNextPosition('a')
  while (sent.length < 2) await new Promise((resolve) => setImmediate(resolve))
  assert.deepEqual(JSON.parse(sent[1].body).payload, {
    type: 'move_node',
    node_id: 'n',
    x: 50,
    y: 60,
    expected_placement_revision: '2',
  })
  release({ placement_revision: '3' })
  await second
  assert.equal(storage.get().positions!['a:n'].synced, true)
  await q.reconcilePositions(positionCanvas)
  assert.equal(
    q.value.positions!['a:n'].x,
    50,
    'old snapshot must not discard acknowledged position',
  )
  await q.reconcilePositions({
    ...positionCanvas,
    nodes: [
      {
        ...positionNode,
        metadata: { ...positionNode.metadata, x: 50, y: 60 },
        placement_revision: '3',
      },
    ],
  })
  assert.equal(q.value.positions!['a:n'], undefined)
  assert.equal(q.value.positions!['a:other'].x, 70)
  await q.stagePosition('a', positionNode, 0, 0)
  assert.equal(
    q.value.positions!['a:n'].revision,
    '3',
    'a drag begun before our receipt must still use our confirmed revision',
  )
})
test('reload recovers the immutable move then sends the latest local destination', async () => {
  const storage = memory()
  const q = new SaveQueue(
    storage,
    {
      send: async () => {
        throw new Error('lost move receipt')
      },
      lookup: async () => ({}),
    },
    () => {},
  )
  await q.stagePosition('a', positionNode, 10, 20)
  await q.sendNextPosition('a')
  const operation = q.value.job!.operation
  await q.stagePosition('a', positionNode, 80, 90)
  await q.close()
  const recovered = new SaveQueue(
    storage,
    {
      lookup: async (id) => {
        assert.equal(id, operation)
        return { placement_revision: '2' }
      },
      send: async (job) => {
        assert.deepEqual(JSON.parse(job.body).payload, {
          type: 'move_node',
          node_id: 'n',
          x: 80,
          y: 90,
          expected_placement_revision: '2',
        })
        return { placement_revision: '3' }
      },
    },
    () => {},
  )
  await recovered.load()
  await recovered.flush()
  assert.equal(recovered.value.positions!['a:n'].x, 80)
  await recovered.sendNextPosition('a')
  assert.equal(recovered.value.positions!['a:n'].synced, true)
})
test('a rejected placement retains local intent until explicitly discarded without retrying conflicts', async () => {
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
  await q.stagePosition('a', positionNode, 15, 25)
  await q.sendNextPosition('a')
  assert.equal(q.value.job?.state, 'rejected')
  assert.equal(q.value.positions!['a:n'].x, 15)
  await q.dismissRejected()
  assert.equal(q.value.positions!['a:n'], undefined)
  assert.equal(q.value.job, null)
})

test('dragging back to the old coordinates after our receipt still sends the second move', async () => {
  const sent: Job[] = []
  const q = new SaveQueue(
    memory(),
    {
      send: async (job) => {
        sent.push(job)
        return { placement_revision: String(sent.length + 1) }
      },
      lookup: async () => {
        throw error(404)
      },
    },
    () => {},
  )
  await q.stagePosition('a', positionNode, 10, 20)
  await q.sendNextPosition('a')
  await q.reconcilePositions({
    ...positionCanvas,
    nodes: [
      {
        ...positionNode,
        metadata: { ...positionNode.metadata, x: 10, y: 20 },
        placement_revision: '2',
      },
    ],
  })
  await q.stagePosition('a', positionNode, 0, 0)
  await q.sendNextPosition('a')
  assert.equal(sent.length, 2)
  assert.deepEqual(JSON.parse(sent[1].body).payload, {
    type: 'move_node',
    node_id: 'n',
    x: 0,
    y: 0,
    expected_placement_revision: '2',
  })
})

test('group move receipts carry only their own versions into the next gesture', async () => {
  let release!: (result: unknown) => void
  const sent: Job[] = []
  const group: CanvasNode = {
    ...positionNode,
    id: 'g',
    metadata: { ...positionNode.metadata, type_key: 'core.group' },
  }
  const child: CanvasNode = { ...positionNode, id: 'child', parent_id: 'g' }
  const canvas: Canvas = { ...positionCanvas, nodes: [group, child] }
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => {
        throw error(404)
      },
      send: async (job) => {
        sent.push(job)
        return new Promise((resolve) => {
          release = resolve
        })
      },
    },
    () => {},
  )
  await q.stagePositions(canvas, [{ node: group, x: 20, y: 10 }])
  const first = q.sendNextPosition('a')
  while (!release) await new Promise((resolve) => setImmediate(resolve))
  await q.stagePositions(canvas, [{ node: group, x: 50, y: 20 }])
  release({
    object_results: [
      {
        kind: 'node',
        id: 'g',
        is_live: true,
        placement_revision: '2',
        data_revision: '1',
      },
    ],
  })
  await first
  const second = q.sendNextPosition('a')
  while (sent.length < 2) await new Promise((resolve) => setImmediate(resolve))
  const payload = JSON.parse(sent[1].body).payload
  assert.equal(
    payload.read_set.find((r: { id: string }) => r.id === 'g')
      .placement_revision,
    '2',
  )
  assert.equal(
    payload.read_set.find((r: { id: string }) => r.id === 'child')
      .placement_revision,
    '1',
  )
  assert.equal(payload.actions[0].x, 50)
  release({
    object_results: [
      {
        kind: 'node',
        id: 'g',
        is_live: true,
        placement_revision: '3',
        data_revision: '1',
      },
    ],
  })
  await second
})
test('incomplete batch receipt remains recoverable instead of acknowledging a missing node', async () => {
  const group: CanvasNode = {
    ...positionNode,
    id: 'g',
    metadata: { ...positionNode.metadata, type_key: 'core.group' },
  }
  const canvas: Canvas = { ...positionCanvas, nodes: [group] }
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => {
        throw error(404)
      },
      send: async () => ({ object_results: [] }),
    },
    () => {},
  )
  await q.stagePositions(canvas, [{ node: group, x: 30, y: 40 }])
  await q.sendNextPosition('a')
  assert.equal(q.value.job?.state, 'unknown')
  assert.equal(q.value.positions?.['a:g'].synced, false)
})

test('unchanged group gesture does not enqueue an empty operation', async () => {
  let sends = 0
  const group: CanvasNode = {
    ...positionNode,
    id: 'g',
    metadata: { ...positionNode.metadata, type_key: 'core.group' },
  }
  const canvas: Canvas = { ...positionCanvas, nodes: [group] }
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => {
        throw error(404)
      },
      send: async () => {
        sends++
        return {}
      },
    },
    () => {},
  )
  await q.stagePositions(canvas, [
    { node: group, x: group.metadata.x, y: group.metadata.y },
  ])
  await q.sendNextPosition(canvas.id)
  assert.equal(sends, 0)
  assert.equal(q.value.job, null)
})
test('normal graph reads exclude unrelated permanent identities', async () => {
  const { graphReadSet } = await import(
    '../src/creative-canvas/editor/graph.ts'
  )
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
  const { graphActionReadSet } = await import(
    '../src/creative-canvas/editor/graph.ts'
  )
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

const connectionCommand = {
  type: 'batch',
  actions: [
    {
      type: 'connect_reference',
      source_node_id: positionNode.id,
      target_node_id: 'target',
      source_port: 'output',
      target_port: 'reference',
      role: 'reference',
    },
  ],
}
const connectionCanvas: Canvas = {
  ...positionCanvas,
  nodes: [positionNode, { ...positionNode, id: 'target' }],
}

test('connection paints before dispatch and survives receipt until authoritative revision', async () => {
  let release!: (value: unknown) => void
  const q = new SaveQueue(
    memory(),
    {
      lookup: async () => ({}),
      send: () =>
        new Promise((resolve) => {
          release = resolve
        }),
    },
    () => {},
  )
  const saving = q.enqueue('/canvases/a/commands', connectionCommand)
  assert.equal(q.connections.visible(connectionCanvas).length, 1)
  while (!release) await new Promise((resolve) => setImmediate(resolve))
  release({ result_revision: '3' })
  await saving
  assert.equal(q.value.job, null)
  assert.equal(
    q.connections.visible({ ...connectionCanvas, revision: '2' }).length,
    1,
  )
  // A newer snapshot can legitimately omit an edge deleted by another window.
  assert.equal(
    q.connections.visible({ ...connectionCanvas, revision: '4' }).length,
    0,
  )
  assert.equal(
    q.connections.visible({ ...connectionCanvas, id: 'other' }).length,
    0,
  )
  assert.equal(
    q.connections.visible({ ...connectionCanvas, nodes: [positionNode] })
      .length,
    0,
  )
  q.connections.reconcile({ ...connectionCanvas, revision: '4' })
  assert.equal(q.connections.visible(connectionCanvas).length, 0)
})

test('unknown connection restores from journal, deduplicates a polling snapshot and recovers same operation', async () => {
  const storage = memory(),
    sent: Job[] = []
  const transport = {
    send: async (job: Job) => {
      sent.push(job)
      throw new Error('lost receipt')
    },
    lookup: async () => ({ result_revision: '3' }),
  }
  const q = new SaveQueue(storage, transport, () => {})
  await q.enqueue('/canvases/a/commands', connectionCommand)
  assert.equal(q.connections.visible(connectionCanvas).length, 1)
  const recovered = new SaveQueue(storage, transport, () => {})
  await recovered.load()
  assert.equal(recovered.connections.visible(connectionCanvas).length, 1)
  const edge = {
    id: 'server-edge',
    source_node_id: positionNode.id,
    target_node_id: 'target',
    source_port: 'output',
    target_port: 'reference',
    role: 'reference',
    revision: '1',
    ordinal: 0,
  }
  assert.equal(
    recovered.connections.visible({ ...connectionCanvas, edges: [edge] })
      .length,
    0,
  )
  await recovered.flush()
  assert.equal(sent.length, 1)
  assert.equal(recovered.connections.visible(connectionCanvas).length, 1)
  assert.equal(
    recovered.connections.visible({
      ...connectionCanvas,
      revision: '3',
      edges: [edge],
    }).length,
    0,
  )
})

test('rejected connections roll back without losing unknown outcomes', async () => {
  for (const failure of [
    error(422),
    error(409, 'creative_revision_conflict'),
    error(403),
  ]) {
    const storage = memory()
    const q = new SaveQueue(
      storage,
      {
        send: async () => {
          throw failure
        },
        lookup: async () => ({}),
      },
      () => {},
    )
    await q.enqueue('/canvases/a/commands', connectionCommand)
    assert.equal(q.value.job?.state, 'rejected')
    assert.equal(q.connections.visible(connectionCanvas).length, 0)
    const restored = new SaveQueue(
      storage,
      { send: async () => ({}), lookup: async () => ({}) },
      () => {},
    )
    await restored.load()
    assert.equal(restored.connections.visible(connectionCanvas).length, 0)
  }
  const q = new SaveQueue(
    memory(),
    { send: async () => ({}), lookup: async () => ({}) },
    () => {},
  )
  await q.enqueue('/canvases/a/commands', connectionCommand)
  assert.equal(
    q.value.job?.state,
    'unknown',
    'malformed receipt cannot confirm the preview',
  )
  assert.equal(q.connections.visible(connectionCanvas).length, 1)
})

test('failed local journal writes remove unsent connection preview', async () => {
  let sends = 0
  const q = new SaveQueue(
    {
      load: async () => emptyJournal(),
      save: async () => {
        throw new Error('disk full')
      },
    },
    {
      send: async () => {
        sends++
        return {}
      },
      lookup: async () => ({}),
    },
    () => {},
  )
  await assert.rejects(
    q.enqueue('/canvases/a/commands', connectionCommand),
    /disk full/,
  )
  assert.equal(sends, 0)
  assert.equal(q.connections.visible(connectionCanvas).length, 0)
})
