import { useEffect, useRef, useState } from 'react'
import { Box, Minimize2 } from 'lucide-react'
import { Canvas } from '@react-three/fiber'
import { Grid } from '@react-three/drei'
import { errorMessage } from '../creative-canvas/editor/queue.ts'
import { readScene, type SceneDocument } from './api.ts'
import './scene.css'

function webgl2Available(): boolean {
  try {
    return !!document.createElement('canvas').getContext('webgl2')
  } catch {
    return false
  }
}

export default function SceneEditor({
  documentID,
  onClose,
}: {
  documentID: string
  onClose: () => void
}) {
  const [scene, setScene] = useState<SceneDocument | null>(null),
    [error, setError] = useState(''),
    [attempt, setAttempt] = useState(0),
    [webgl] = useState(webgl2Available)
  const root = useRef<HTMLElement>(null)
  useEffect(() => {
    const previous = window.document.activeElement
    root.current?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus()
    }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    setScene(null)
    setError('')
    readScene(documentID, controller.signal)
      .then((d) => {
        if (!controller.signal.aborted) setScene(d)
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
    return () => controller.abort()
  }, [documentID, attempt])
  return (
    <section
      ref={root}
      tabIndex={-1}
      className="cc-maximized-document cc-surface cc-scene-editor"
      aria-label="3D 场景编辑"
    >
      <header>
        <Box size={18} />
        <h2>{scene?.title ?? '3D 场景'}</h2>
        <button className="btn" onClick={onClose}>
          <Minimize2 size={16} />
          还原节点
        </button>
      </header>
      <div className="cc-scene-body">
        {!webgl ? (
          <p role="alert" className="cc-scene-message">
            当前浏览器无法显示 3D 场景，请使用最新版 Chrome、Edge 或 Safari
          </p>
        ) : error ? (
          <div role="alert" className="cc-scene-message">
            <p>{error}</p>
            <button className="btn" onClick={() => setAttempt((n) => n + 1)}>
              重新读取
            </button>
          </div>
        ) : !scene ? (
          <p role="status" className="cc-scene-message">
            正在读取场景…
          </p>
        ) : (
          <Canvas
            className="cc-scene-viewport"
            camera={{ position: [8, 6, 8], fov: 50 }}
          >
            <Grid infiniteGrid cellSize={1} sectionSize={5} fadeDistance={60} />
          </Canvas>
        )}
      </div>
    </section>
  )
}
