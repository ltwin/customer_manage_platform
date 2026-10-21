import { StrictMode, useLayoutEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Canvas, useThree } from '@react-three/fiber'
import { ACESFilmicToneMapping, PCFSoftShadowMap, WebGPURenderer } from 'three/webgpu'
import { referenceScene, sceneNames, shotCamera } from './doc.ts'
import { SceneContent } from './scene.tsx'
import { ViewportCase, exposeShooter } from './integration.tsx'
import { exposeMaterials } from './materialsCase.ts'
import { exposeDof } from './dofCase.ts'
import { exposeAO } from './aoCase.ts'
import { exposeSky } from './skyCase.ts'
import { exposeShadow } from './shadowCase.ts'
import { exposeOutline } from './outlineCase.ts'
import { exposeGBuffer } from './gbufferCase.ts'
import { exposeGI } from './giCase.ts'
import { exposeHires } from './hiresCase.ts'
import { autoReport, exposeReadback } from './readbackCase.ts'
import { exposeParity } from './parityCase.ts'

// R0 spike page, served by the dev server only (frontend/spike-webgpu.html;
// vite.config.ts builds index.html alone). ?scene=<reference scene>,
// ?case=<prototype>, ?backend=webgl forces three's WebGL2 backend.
// window.__spike tells measurement scripts the backend and when it drew.

declare global {
  interface Window {
    __spike?: { backend: string; ready: boolean; frames: number; renderer: WebGPURenderer }
  }
}

const query = new URLSearchParams(location.search)
const sceneName = query.get('scene') ?? 'noon'
const forceWebGL = query.get('backend') === 'webgl'
const caseName = query.get('case') ?? 'basic'

async function createRenderer(props: { canvas: HTMLCanvasElement | OffscreenCanvas }) {
  const renderer = new WebGPURenderer({ canvas: props.canvas as HTMLCanvasElement, antialias: query.get('aa') !== '0', forceWebGL })
  await renderer.init()
  renderer.toneMapping = ACESFilmicToneMapping
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = PCFSoftShadowMap
  // drei's GizmoViewport asks gl.capabilities, which only WebGLRenderer has.
  ;(renderer as unknown as { capabilities: unknown }).capabilities = { getMaxAnisotropy: () => renderer.getMaxAnisotropy() }
  const backend = (renderer.backend as { isWebGPUBackend?: boolean }).isWebGPUBackend ? 'webgpu' : 'webgl2'
  window.__spike = { backend, ready: false, frames: 0, renderer }
  return renderer
}

function Shot({ doc }: { doc: ReturnType<typeof referenceScene> }) {
  const size = useThree((s) => s.size)
  const set = useThree((s) => s.set)
  const { camera } = useMemo(() => shotCamera(doc, query.get('camera'), size.width / size.height), [doc, size])
  useLayoutEffect(() => set({ camera }), [camera, set])
  return null
}

function Ready() {
  const gl = useThree((s) => s.gl)
  const invalidate = useThree((s) => s.invalidate)
  useLayoutEffect(() => {
    let frames = 0
    const tick = () => {
      if (window.__spike) {
        window.__spike.frames = ++frames
        if (frames > 3) window.__spike.ready = true
      }
      if (frames <= 3) {
        invalidate()
        requestAnimationFrame(tick)
      }
    }
    requestAnimationFrame(tick)
  }, [gl, invalidate])
  return null
}

function App() {
  const doc = useMemo(() => referenceScene(sceneName), [])
  const [backend, setBackend] = useState('…')
  return (
    <>
      <Canvas
        frameloop="demand"
        shadows
        dpr={[1, 2]}
        gl={createRenderer}
        onCreated={() => setBackend(window.__spike?.backend ?? '?')}
      >
        {caseName === 'viewport' ? (
          <ViewportCase doc={doc} />
        ) : (
          <>
            <Shot doc={doc} />
            <SceneContent doc={doc} />
          </>
        )}
        <Ready />
      </Canvas>
      <div style={{ position: 'fixed', left: 8, top: 8, background: '#000a', padding: '4px 8px', borderRadius: 4 }}>
        {sceneName} · backend <b id="backend">{backend}</b> · scenes: {sceneNames.join(' ')}
      </div>
    </>
  )
}

if (caseName === 'shooter') {
  exposeShooter()
  document.getElementById('root')!.textContent = 'shooter: window.__shooter.run({...})'
} else if (caseName === 'materials') {
  exposeMaterials()
  document.getElementById('root')!.textContent = 'materials: window.__materials.renderLegacy / renderTSL'
} else if (caseName === 'dof') {
  exposeDof()
  document.getElementById('root')!.textContent = 'dof: window.__dof.dofLegacy / dofTSL'
} else if (caseName === 'gi') {
  exposeGI()
  document.getElementById('root')!.textContent = 'gi: window.__gi.renderGI({...})'
} else if (caseName === 'readback') {
  // ?report=<url>: run groups 1, 5 and 7 and post the results there (Safari).
  exposeReadback()
  exposeShooter()
  const report = new URLSearchParams(location.search).get('report')
  document.getElementById('root')!.textContent = report ? 'running…' : 'readback: window.__readback'
  if (report) void autoReport(report)
} else if (caseName === 'parity') {
  exposeParity()
  document.getElementById('root')!.textContent = 'parity: window.__parity'
} else if (caseName === 'hires') {
  exposeHires()
  document.getElementById('root')!.textContent = 'hires: window.__hires.renderHires({...})'
} else if (caseName === 'gbuffer') {
  exposeGBuffer()
  document.getElementById('root')!.textContent = 'gbuffer: window.__gbuffer'
} else if (caseName === 'outline') {
  exposeOutline()
  document.getElementById('root')!.textContent = 'outline: window.__outline.renderOutline(variant, w, h)'
} else if (caseName === 'shadow') {
  exposeShadow()
  document.getElementById('root')!.textContent = 'shadow: window.__shadow.shadowLegacy / shadowTSL'
} else if (caseName === 'sky') {
  exposeSky()
  document.getElementById('root')!.textContent = 'sky: window.__sky.skyLegacy / skyTSL'
} else if (caseName === 'ao') {
  exposeAO()
  document.getElementById('root')!.textContent = 'ao: window.__ao.renderAO(kind, node, w, h)'
} else createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
