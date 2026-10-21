import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { RenderTarget } from 'three'
import { NodeFrame, ViewportTextureNode } from 'three/webgpu'
import { advance, DeviceLostError, deviceState, reset, type FrameState } from '../src/creative-scene/render/gpu.ts'

// The renderer entry steps three's private frame state (render/gpu.ts). These
// checks fail first when a three upgrade moves it.

const three = (path: string) => readFileSync(new URL(`../node_modules/three/src/${path}`, import.meta.url), 'utf8')

test('three keeps the frame state and the loop where the entry reaches for them', () => {
  const renderer = three('renderers/common/Renderer.js')
  assert.match(renderer, /this\._nodes = new NodeManager\(/)
  assert.match(renderer, /this\._animation = new Animation\(/)
  assert.match(three('renderers/common/Animation.js'), /\n\tstop\(\) \{/)
  assert.match(three('renderers/common/nodes/NodeManager.js'), /this\.nodeFrame = new NodeFrame\(\)/)
  const frame = new NodeFrame() as unknown as FrameState
  for (const key of ['frameId', 'time', 'deltaTime'] as const) assert.equal(typeof frame[key], 'number', key)
  for (const key of ['updateMap', 'updateBeforeMap', 'updateAfterMap'] as const) assert.ok(frame[key] instanceof WeakMap, key)
})

test('a lost WebGPU device and a lost WebGL context both reach onDeviceLost, dispose does not', () => {
  const webgpu = three('renderers/webgpu/WebGPUBackend.js')
  assert.match(webgpu, /if \( info\.reason === 'destroyed' \) return;/)
  assert.match(webgpu, /renderer\.onDeviceLost\( deviceLossInfo \)/)
  assert.match(three('renderers/webgl-fallback/WebGLBackend.js'), /renderer\.onDeviceLost\( contextLossInfo \)/)
})

/** A node that updates once per frame and counts its runs. */
function frameNode() {
  const node = {
    runs: 0,
    getUpdateBeforeType: () => 'frame',
    updateReference: () => node,
    updateBefore() {
      node.runs++
    },
  }
  return node
}

test('advancing steps the frame by a fixed time; a node runs once per frame', () => {
  const frame = new NodeFrame()
  const state = frame as unknown as FrameState
  const node = frameNode()
  advance(state)
  frame.updateBeforeNode(node as never)
  frame.updateBeforeNode(node as never)
  assert.equal(node.runs, 1)
  advance(state)
  frame.updateBeforeNode(node as never)
  assert.equal(node.runs, 2)
  assert.equal(state.frameId, 2)
  assert.ok(Math.abs(state.time - 2 / 60) < 1e-12)
})

test('after a reset the next picture’s frame 1 runs a node kept from the last picture again', () => {
  const frame = new NodeFrame()
  const state = frame as unknown as FrameState
  const node = frameNode()
  advance(state)
  frame.updateBeforeNode(node as never)
  reset(state)
  assert.equal(state.frameId, 0)
  assert.equal(state.time, 0)
  advance(state)
  frame.updateBeforeNode(node as never)
  assert.equal(node.runs, 2, 'the second shot draws its own pass')
})

test('resetting only the count would skip it: the records are what the reset is for', () => {
  const frame = new NodeFrame()
  const state = frame as unknown as FrameState
  const node = frameNode()
  advance(state)
  frame.updateBeforeNode(node as never)
  state.frameId = 0
  advance(state)
  frame.updateBeforeNode(node as never)
  assert.equal(node.runs, 1)
})

test('a capture on a lost device throws, and one that loses the device while drawing gives no image', () => {
  const device = deviceState()
  const encode = () => new Blob(['png'])
  assert.equal(device.capture(() => {}, encode).size, 3)
  device.loseAfter(2, () => device.markLost())
  device.capture(() => {}, encode)
  assert.throws(() => device.capture(() => {}, encode), DeviceLostError, 'the loss after the second capture discards it')
  assert.throws(() => device.capture(() => assert.fail('nothing is drawn on a lost device'), encode), DeviceLostError)
})

test('a WebGL context marked lost fails the capture before onDeviceLost arrives', () => {
  let contextLost = false
  const device = deviceState(() => contextLost)
  assert.throws(() =>
    device.capture(
      () => (contextLost = true),
      () => assert.fail('not encoded'),
    ),
  DeviceLostError)
  assert.equal(device.isLost(), true)
})

test('resources cached on a device go with it, lost or disposed; the loss settles once', async () => {
  const disposed: string[] = []
  const res = (name: string) => ({ dispose: () => disposed.push(name) })
  const device = deviceState()
  const room = device.cache('room', () => res('room'))
  assert.equal(device.cache('room', () => res('other')), room)
  device.markLost()
  device.markLost()
  assert.deepEqual(disposed, ['room'])
  assert.ok((await device.lost) instanceof DeviceLostError)
  assert.throws(() => device.cache('room', () => res('late')), DeviceLostError)
  const other = deviceState()
  other.cache('sky', () => res('sky'))
  other.dispose()
  assert.deepEqual(disposed, ['room', 'sky'])
})

test('each render target’s viewport texture has a size of its own, as glass in the viewport and an export need', () => {
  // three clones them from one texture, source and all; the entry gives each its own (render/gpu.ts).
  assert.match(three('nodes/display/ViewportTextureNode.js'), /const framebufferTexture = defaultFramebuffer\.clone\(\);/)
  const node = new ViewportTextureNode()
  const viewport = node.getTextureForReference(new RenderTarget(2394, 1692))
  const shot = node.getTextureForReference(new RenderTarget(2048, 1365))
  shot.image.width = 2048
  assert.notEqual(viewport.source, shot.source)
  assert.notEqual(viewport.image.width, 2048)
})

test('a disposed render target’s viewport texture goes to the next target: none piles up, none is destroyed', () => {
  const node = new ViewportTextureNode()
  const first = new RenderTarget(64, 64)
  const old = node.getTextureForReference(first)
  let disposed = 0
  old.addEventListener('dispose', () => disposed++)
  const live = node.getTextureForReference(new RenderTarget(64, 64))
  first.dispose()
  const next = node.getTextureForReference(new RenderTarget(64, 64))
  assert.equal(next, old, 'taken over')
  assert.notEqual(next, live)
  assert.notEqual(node.getTextureForReference(new RenderTarget(64, 64)), old, 'a live one is never shared')
  assert.equal(disposed, 0)
})
