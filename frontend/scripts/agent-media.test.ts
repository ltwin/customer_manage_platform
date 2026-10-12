import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  linkedMediaKind,
  mediaSource,
} from '../src/creative-canvas/editor/markdownMedia.ts'

test('media links use the path extension, preserving signed queries and fragments', () => {
  assert.equal(
    linkedMediaKind('https://cdn.example/CLIP.MP4?signature=abc#t=1'),
    'video',
  )
  assert.equal(linkedMediaKind('/media/sound.wav?download=1'), 'audio')
  assert.equal(linkedMediaKind('//cdn.example/demo.webm'), 'video')
  assert.equal(
    linkedMediaKind('https://example.com/page?file=video.mp4'),
    undefined,
  )
  assert.equal(linkedMediaKind('https://example.com/no-extension'), undefined)
  assert.equal(linkedMediaKind('https://example.com/video.mp4.html'), undefined)
})

test('embeds reject non-web schemes, credentials and missing destinations', () => {
  for (const source of [
    undefined,
    '',
    ' ',
    'javascript:alert(1)',
    'data:image/svg+xml,test',
    'file:///tmp/a.mp4',
    'blob:https://example.com/id',
    'streamdown:incomplete-link',
    'https://user:secret@example.com/a.mp4',
  ]) {
    assert.equal(mediaSource(source), undefined, source)
    assert.equal(linkedMediaKind(source), undefined, source)
  }
  assert.equal(
    mediaSource('/media/image.png?token=example'),
    '/media/image.png?token=example',
  )
  assert.equal(
    mediaSource('https://cdn.example/picture.png'),
    'https://cdn.example/picture.png',
  )
})
