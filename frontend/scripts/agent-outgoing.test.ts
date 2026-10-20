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
  const parts = outgoingParts(
    [
      { type: 'skill_ref', skill_id: 's', skill_version_id: 'v' },
      { type: 'text', text: '参考' },
      { type: 'content_ref', content_revision_id: 'rev-1' },
      { type: 'text', text: '出三版' },
      { type: 'content_ref', content_revision_id: 'rev-9' },
    ],
    titleOf,
    '人像修图',
  )
  assert.deepEqual(parts, [
    { kind: 'chip', title: '人像修图' },
    { kind: 'text', text: '参考' },
    { kind: 'chip', title: '秋日样片' },
    { kind: 'text', text: '出三版' },
    { kind: 'chip', title: '引用素材' },
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
        { type: 'text', text: '参考' },
        { type: 'content_ref', ref_id: 'rev-2' },
        { type: 'command', command: 'compact' },
      ],
      titleOf,
    ),
    [
      { kind: 'text', text: '参考' },
      { kind: 'chip', title: '上传的参考图' },
      { kind: 'command', command: 'compact', text: undefined },
    ],
  )
})

test('the objects selected in the scene editor ride on the message', () => {
  const parts = outgoingParts(
    [{ type: 'text', text: '把它们加高' }],
    titleOf,
    undefined,
    ['柱 A', ''],
  )
  assert.deepEqual(parts, [
    { kind: 'text', text: '把它们加高' },
    { kind: 'scene', names: ['柱 A', ''] },
  ])
  // The stored message reads back the same: the server puts the selection last.
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
      imageInput: false,
      revisionIDs: [],
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
        imageInput: false,
        revisionIDs: [],
        segments: [{ type: 'text', text: 'x' }],
      }),
    ],
  }
  assert.equal(refreshAfterSend(created, run), 'all')
  // A reply that is not a run says nothing about the thread: reread all.
  assert.equal(refreshAfterSend(into, { id: 'consent-1' }), 'all')
})
