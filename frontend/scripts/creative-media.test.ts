import test from 'node:test'
import assert from 'node:assert/strict'
import {
  classifyFile,
  describeFailure,
  pickRendition,
  type MediaCapabilities,
  type Upload,
} from '../src/creative-canvas/editor/media.ts'
import { contentText, payload } from '../src/creative-canvas/editor/content.ts'

const capabilities: MediaCapabilities = {
  schema_version: 1,
  formats: [
    { kind: 'image', mime: 'image/png', extensions: ['.png'] },
    { kind: 'video', mime: 'video/webm', extensions: ['.webm'] },
  ],
  image_max_bytes: 1000,
  av_max_bytes: 5000,
  part_size: 8,
  batch_limit: 50,
}
const file = (name: string, type: string, size: number) =>
  new File([new Uint8Array(size)], name, { type })

test('files are classified by enabled formats and per-kind size limits', () => {
  assert.deepEqual(classifyFile(file('a.png', 'image/png', 10), capabilities), {
    kind: 'image',
    mime: 'image/png',
  })
  // Unknown declared MIME falls back to the extension; the server verifies bytes.
  assert.deepEqual(classifyFile(file('b.webm', '', 10), capabilities), {
    kind: 'video',
    mime: 'video/webm',
  })
  assert.deepEqual(classifyFile(file('c.gif', 'image/gif', 10), capabilities), {
    error: '暂不支持此文件格式',
  })
  assert.deepEqual(classifyFile(file('d.png', 'image/png', 1001), capabilities), {
    error: '文件超过 0 MB 上限',
  })
  assert.deepEqual(classifyFile(file('e.png', 'image/png', 0), capabilities), {
    error: '文件为空',
  })
})

test('failure codes map to actionable messages and ready uploads have none', () => {
  const base = {
    id: 'ccup_1',
    io_phase: 'none',
    revision: '2',
    kind: 'image',
    mime: 'image/png',
    size: 1,
    file_name: 'a.png',
    part_size: 8,
    part_count: 1,
    uploaded_parts: [],
    target_kind: 'asset',
    expires_at: '2026-01-01T00:00:00Z',
    publication: null,
    binding: null,
    content_revision_id: null,
    created_at: '2026-01-01T00:00:00Z',
  } as const
  const failed: Upload = { ...base, state: 'failed', error_code: 'media_unsupported: detected audio/mpeg' }
  assert.equal(describeFailure(failed), '文件内容不是支持的媒体格式')
  assert.equal(describeFailure({ ...base, state: 'ready', error_code: '' }), null)
  assert.equal(describeFailure({ ...base, state: 'failed', error_code: 'weird' }), '上传失败')
})

test('media payloads keep captions separate from text and link bodies', () => {
  assert.deepEqual(payload('text', '正文'), { body: '正文' })
  assert.deepEqual(payload('link', 'https://a'), { url: 'https://a' })
  assert.deepEqual(payload('image', ' '), {})
  assert.deepEqual(payload('video', '说明'), { caption: '说明' })
  assert.equal(contentText({ caption: '说明' }), '说明')
  assert.equal(contentText({ body: 'x' }), 'x')
  assert.equal(contentText(undefined), '')
})

test('rendition choice prefers display for viewing and original when absent', () => {
  const original = { role: 'original', blob_id: 'o', mime: 'image/png', byte_size: 1, width: 1, height: 1, duration_ms: null } as const
  const display = { ...original, role: 'display', blob_id: 'd' } as const
  assert.equal(pickRendition([original, display], 'display')?.blob_id, 'd')
  assert.equal(pickRendition([original], 'display')?.blob_id, 'o')
  assert.equal(pickRendition([original, display], 'original')?.blob_id, 'o')
  assert.equal(pickRendition([], 'display'), undefined)
})
