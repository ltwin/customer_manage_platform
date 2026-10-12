import test from 'node:test'
import assert from 'node:assert/strict'
import {
  admitAttachments,
  attachmentCap,
  savedAttachments,
  type ChatAttachment,
} from '../src/creative-canvas/home/chatAttachments.ts'
import {
  homeChain,
  projectNameFrom,
  readHomeStart,
  recoverHomeStart,
  type HomeStart,
} from '../src/creative-canvas/home/startProject.ts'
import {
  advanceChain,
  pendingAgentChain,
} from '../src/creative-canvas/editor/agentState.ts'

test('attachments cap at six and at what the catalog allows', () => {
  assert.equal(attachmentCap(), 6)
  assert.equal(attachmentCap({ attachments: 10, image_preview_count: 8 }), 6)
  assert.equal(attachmentCap({ attachments: 4, image_preview_count: 8 }), 4)
  assert.equal(attachmentCap({ attachments: 10, image_preview_count: 3 }), 3)
})

test('duplicates are skipped; a batch past the cap is refused whole', () => {
  const existing = [{ digest: 'a' }, { revisionID: 'ccrv_1' }]
  assert.deepEqual(
    admitAttachments(existing, [{ digest: 'b' }, { digest: 'c' }], 6),
    { accepted: [0, 1], message: '' },
  )
  // 上传按内容摘要判重，资产库按修订判重，同一批里重复的也算。
  assert.deepEqual(
    admitAttachments(
      existing,
      [
        { digest: 'a' },
        { digest: 'd' },
        { digest: 'd' },
        { revisionID: 'ccrv_1' },
      ],
      6,
    ),
    { accepted: [1], message: '这张图已经添加过' },
  )
  // 已有 2 张，再加 5 张超过 6：整批拒绝，一张也不加。
  const five = ['p', 'q', 'r', 's', 't'].map((digest) => ({ digest }))
  assert.deepEqual(admitAttachments(existing, five, 6), {
    accepted: [],
    message: '最多 6 张',
  })
  assert.deepEqual(admitAttachments(existing, five.slice(0, 4), 6), {
    accepted: [0, 1, 2, 3],
    message: '',
  })
  // 第 7 张被拒；上限按目录收紧时文案跟着变。
  const six = Array.from({ length: 6 }, (_, i) => ({ digest: `x${i}` }))
  assert.equal(
    admitAttachments(six, [{ digest: 'seventh' }], 6).message,
    '最多 6 张',
  )
  assert.equal(admitAttachments([], six, 4).message, '最多 4 张')
})

test('only finished attachments are kept for refresh', () => {
  const items: ChatAttachment[] = [
    { key: 'a', title: '上传中', source: 'upload', state: 'uploading' },
    {
      key: 'b',
      title: '失败',
      source: 'upload',
      state: 'failed',
      error: '上传失败',
    },
    {
      key: 'c',
      title: '海边',
      source: 'upload',
      state: 'ready',
      revisionID: 'ccrv_c',
    },
    {
      key: 'ccrv_d',
      title: '库里的',
      source: 'library',
      state: 'ready',
      revisionID: 'ccrv_d',
    },
  ]
  assert.deepEqual(savedAttachments(items), [
    { id: 'ccrv_c', title: '海边', kind: 'image', source: 'upload' },
    { id: 'ccrv_d', title: '库里的', kind: 'image', source: 'library' },
  ])
})

test('the project is named after the first line, cut at 30 characters', () => {
  assert.equal(projectNameFrom('  海边婚纱  \n第二行'), '海边婚纱')
  assert.equal(projectNameFrom('\n\n'), '未命名项目')
  const long = '📷'.repeat(31)
  assert.equal(projectNameFrom(long), `${'📷'.repeat(30)}…`)
  assert.equal(projectNameFrom('📷'.repeat(30)), '📷'.repeat(30))
})

const base: HomeStart = {
  text: '海边婚纱外景',
  model: 'vision',
  skill: 'version',
  attachments: [
    { id: 'ccrv_a', title: '参考一', kind: 'image', source: 'upload' },
    { id: 'ccrv_b', title: '参考二', kind: 'image', source: 'library' },
  ],
}
const project = {
  ...base,
  projectID: 'project',
  canvasID: 'canvas',
  name: '海边婚纱外景',
}

test('the home record is read back strictly', () => {
  assert.deepEqual(readHomeStart(JSON.stringify(project)), project)
  assert.equal(readHomeStart(null), null)
  assert.equal(readHomeStart('{'), null)
  assert.equal(readHomeStart(JSON.stringify({ ...base, text: 1 })), null)
  assert.equal(
    readHomeStart(
      JSON.stringify({ ...base, attachments: [{ id: 'x', title: 'x' }] }),
    ),
    null,
  )
  // 有项目却没有画布，无法接着发。
  assert.equal(
    readHomeStart(JSON.stringify({ ...base, projectID: 'project' })),
    null,
  )
  assert.equal(
    readHomeStart(JSON.stringify({ ...project, chain: { operations: [1] } })),
    null,
  )
})

test('four recoveries after a refresh', () => {
  assert.deepEqual(recoverHomeStart(null, null), { kind: 'none' })
  // 项目还没建：恢复输入框。
  assert.deepEqual(recoverHomeStart(base, null), { kind: 'draft', start: base })
  // 项目建好、链还没组装：再发送时用这个项目。
  assert.deepEqual(recoverHomeStart(project, null), {
    kind: 'project',
    start: project,
  })
  const chain = homeChain(
    project,
    { imageInput: true },
    {
      skill_id: 'skill',
      skill_version_id: 'version',
    },
  )
  const sent = {
    ...project,
    chain: { operations: chain.steps.map((step) => step.operation) },
  }
  // 槽位里还是这条链（哪怕已经走了几步）：结果未知，只能原样重放。
  assert.deepEqual(recoverHomeStart(sent, chain), {
    kind: 'unknown',
    start: sent,
    chain,
  })
  const moved = advanceChain(advanceChain(chain, { id: 'conv' })!, {
    id: 'consent',
  })!
  assert.equal(recoverHomeStart(sent, moved).kind, 'unknown')
  // 槽位空了，或者已经换成别的链：在别处处理掉了，清掉记录。
  assert.deepEqual(recoverHomeStart(sent, null), { kind: 'settled' })
  const other = homeChain(project, { imageInput: true })
  assert.deepEqual(recoverHomeStart(sent, other), { kind: 'settled' })
})

test('the home chain matches the canvas assistant and keeps a draft', () => {
  const chain = homeChain(
    project,
    { imageInput: true },
    {
      skill_id: 'skill',
      skill_version_id: 'version',
    },
  )
  assert.deepEqual(
    chain.steps.map((step) => step.path),
    [
      '/canvases/canvas/conversations',
      '/conversations/{conversation}/egress-consents',
      '/conversations/{conversation}/runs',
    ],
  )
  assert.deepEqual(chain.steps[1].payload, {
    purpose: 'creative_assistance',
    mode: 'selected_revisions',
    data_classes: ['text', 'canvas_metadata', 'image_preview'],
    selected_revision_ids: ['ccrv_a', 'ccrv_b'],
  })
  // 指令：先 Skill，再文字，参考图按添加顺序排在后面。
  assert.deepEqual(chain.steps[2].payload, {
    model_key: 'vision',
    egress_consent_id: '',
    instruction: {
      schema_version: 1,
      instruction_segments: [
        { type: 'skill_ref', skill_id: 'skill', skill_version_id: 'version' },
        { type: 'text', text: '海边婚纱外景' },
        { type: 'content_ref', content_revision_id: 'ccrv_a' },
        { type: 'content_ref', content_revision_id: 'ccrv_b' },
      ],
    },
  })
  assert.deepEqual(chain.draft, {
    conversation: '',
    model: 'vision',
    skill: 'version',
    text: '海边婚纱外景 @[ccrv_a] @[ccrv_b]',
    materials: [
      { id: 'ccrv_a', title: '参考一', kind: 'image' },
      { id: 'ccrv_b', title: '参考二', kind: 'image' },
    ],
  })
  // 链能原样存进画布槽位、被面板读回。
  assert.deepEqual(pendingAgentChain(JSON.stringify(chain)), chain)
  // 被拒前已经建好对话：改了再发时直接接着用，不再建一个。
  const again = homeChain(
    { ...project, conversation: 'conv', attachments: [] },
    { imageInput: false },
  )
  assert.deepEqual(
    again.steps.map((step) => step.path),
    ['/conversations/conv/egress-consents', '/conversations/conv/runs'],
  )
  assert.deepEqual(again.steps[0].payload.data_classes, [
    'text',
    'canvas_metadata',
  ])
})


test('upload digests survive storage; older attachments without digests still load', () => {
  const digest = 'a'.repeat(64)
  const attachments = savedAttachments([{
    key: 'upload', title: 'photo.png', source: 'upload', state: 'ready',
    revisionID: 'ccrv_photo', digest,
  }])
  const saved = readHomeStart(JSON.stringify({ ...base, attachments }))!
  assert.equal(saved.attachments[0].digest, digest)
  assert.deepEqual(readHomeStart(JSON.stringify(base)), base)
  assert.equal(readHomeStart(JSON.stringify({
    ...base, attachments: [{ ...attachments[0], digest: 123 }],
  })), null)
})
