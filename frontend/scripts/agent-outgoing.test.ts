import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  acceptOutgoing,
  messageParts,
  outgoingIn,
  outgoingParts,
  refreshAfterSend,
  type Outgoing,
} from '../src/creative-canvas/editor/agentOutgoing.ts'
import {
  agentStep,
  conversationStep,
  conversationSlot,
  messageSteps,
} from '../src/creative-canvas/editor/agentState.ts'
import { selectionLabel } from '../src/creative-canvas/editor/agentScene.ts'

const titles = new Map([
  ['rev-1', '秋日样片'],
  ['rev-2', '上传的参考图'],
])
const titleOf = (id: string) => titles.get(id)

test('what was sent is drawn the way the server will draw it', () => {
  const skill = { skill_id: 's', skill_version_id: 'v', display_name: '人像修图', version_number: 2 }
  const parts = outgoingParts(
    [
      { type: 'skill_ref', skill_id: 's', skill_version_id: 'v' },
      { type: 'text', text: '参考' },
      { type: 'content_ref', content_revision_id: 'rev-1' },
      { type: 'text', text: '出三版' },
      { type: 'content_ref', content_revision_id: 'rev-9' },
    ],
    titleOf,
    skill,
  )
  assert.deepEqual(parts, [
    { kind: 'chip', title: '人像修图', skill },
    { kind: 'text', text: '参考' },
    { kind: 'chip', title: '秋日样片', revisionID: 'rev-1' },
    { kind: 'text', text: '出三版' },
    { kind: 'chip', title: '引用素材', revisionID: 'rev-9' },
  ])
  assert.deepEqual(
    outgoingParts(
      [{ type: 'command', command: 'compact', text: '配色' }],
      titleOf,
    ),
    [{ kind: 'command', command: 'compact', text: '配色' }],
  )
  // A stored message reads into the same parts.
  assert.deepEqual(
    messageParts(
      [
        { type: 'skill_ref', skill: { ...skill, digest: 'd' } },
        { type: 'text', text: '参考' },
        { type: 'content_ref', ref_id: 'rev-2' },
        { type: 'command', command: 'compact' },
      ],
      titleOf,
    ),
    [
      { kind: 'chip', title: '人像修图', skill },
      { kind: 'text', text: '参考' },
      { kind: 'chip', title: '上传的参考图', revisionID: 'rev-2' },
      { kind: 'command', command: 'compact', text: undefined },
    ],
  )
})

test('a selection an older page sent still rides on the message', () => {
  const parts = [
    { kind: 'text', text: '把它们加高' },
    { kind: 'scene', names: ['柱 A', ''] },
  ]
  // The server put the selection last.
  assert.deepEqual(
    messageParts(
      [
        { type: 'text', text: '把它们加高' },
        {
          type: 'scene_selection',
          scene: {
            document_id: 'doc',
            title: '中殿',
            objects: [
              { id: 'a', name: '柱 A' },
              { id: 'b', name: '' },
            ],
          },
        },
      ],
      titleOf,
    ),
    parts,
  )
  assert.deepEqual(selectionLabel(['柱 A']), { label: '柱 A', detail: '柱 A' })
  assert.deepEqual(selectionLabel(['柱 A', '', '椅子']), {
    label: '柱 A 等 3 个物体',
    detail: '柱 A、未命名物体、椅子',
  })
})

test('scene references are drawn in place, as sent and as stored', () => {
  const lamp = { documentID: 'doc', title: '客厅', objectIDs: ['a', 'b'], names: ['台灯', ''] }
  const whole = { documentID: 'bed', title: '卧室', objectIDs: [], names: [] }
  const parts = outgoingParts(
    [
      { type: 'text', text: '把' },
      { type: 'scene_ref', document_id: 'doc', object_ids: ['a', 'b'] },
      { type: 'text', text: '改成金属，' },
      { type: 'scene_ref', document_id: 'bed' },
    ],
    titleOf,
    undefined,
    [lamp, whole],
  )
  assert.deepEqual(parts, [
    { kind: 'text', text: '把' },
    { kind: 'chip', title: '客厅 · 台灯、未命名', scene: true, sceneTitle: '客厅', detail: '台灯、未命名' },
    { kind: 'text', text: '改成金属，' },
    { kind: 'chip', title: '卧室', scene: true, sceneTitle: '卧室', detail: '整个场景' },
  ])
  // Stored: the snapshot's names, and an object gone when sent reads as deleted.
  assert.deepEqual(
    messageParts(
      [
        {
          type: 'scene_ref',
          scene: {
            document_id: 'doc',
            title: '客厅',
            revision: 12,
            objects: [
              { id: 'a', name: '台灯' },
              { id: 'b', name: '', missing: true },
            ],
          },
        },
        { type: 'scene_ref', scene: { document_id: 'bed', title: '卧室', revision: 3, objects: [] } },
      ],
      titleOf,
    ),
    [
      { kind: 'chip', title: '客厅 · 台灯、已删除的物件', scene: true, sceneTitle: '客厅', detail: '台灯、已删除的物件' },
      { kind: 'chip', title: '卧室', scene: true, sceneTitle: '卧室', detail: '整个场景' },
    ],
  )
})

const sending: Outgoing = {
  conversation: '',
  parts: [{ kind: 'text', text: '你好' }],
}

test('a sent message shows until the thread holds the real one', () => {
  // Before the server answers: shown in the conversation it was sent from.
  assert.equal(outgoingIn(sending, '', []), sending)
  assert.equal(outgoingIn(sending, 'other', []), null)
  // Accepted: it follows the run into the conversation the server named.
  const accepted = acceptOutgoing(sending, {
    id: 'run-1',
    conversation_id: 'c-1',
    trigger_message_id: 'm-1',
  })
  assert.deepEqual(accepted, {
    ...sending,
    conversation: 'c-1',
    trigger: 'm-1',
    run: 'run-1',
  })
  assert.equal(outgoingIn(accepted, 'c-1', [{ id: 'm-0' }]), accepted)
  // The thread read back the real message: it takes over.
  assert.equal(
    outgoingIn(accepted, 'c-1', [{ id: 'm-0' }, { id: 'm-1' }]),
    null,
  )
  assert.equal(outgoingIn(null, 'c-1', []), null)
})

test('a send into an existing conversation rereads only the thread', () => {
  const into = {
    steps: messageSteps('c-1', {
      model: 'm',
      segments: [{ type: 'text', text: 'x' }],
    }),
  }
  const run = { id: 'run-1', conversation_id: 'c-1', trigger_message_id: 'm-1' }
  assert.equal(refreshAfterSend(into, run), 'thread')
  // A new conversation has to appear in the list too.
  const created = {
    steps: [
      conversationStep('canvas'),
      ...messageSteps(conversationSlot, {
        model: 'm',
        segments: [{ type: 'text', text: 'x' }],
      }),
    ],
  }
  assert.equal(refreshAfterSend(created, run), 'all')
  // A reply that is not a run says nothing about the thread: reread all.
  assert.equal(refreshAfterSend(into, { id: 'consent-1' }), 'all')
  // A steer or a supplement shows up as the run's input block; nothing to reread.
  for (const path of ['/agent-runs/run-1/steers', '/agent-runs/run-1/supplements'])
    assert.equal(
      refreshAfterSend({ steps: [agentStep(path, { instruction_segments: [] })] }, run),
      'none',
    )
})
