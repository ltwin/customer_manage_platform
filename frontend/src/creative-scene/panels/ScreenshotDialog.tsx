import { useEffect, useRef, useState } from 'react'
import { Download, ImagePlus } from 'lucide-react'
import StudioDialog from '../../creative-canvas/editor/StudioDialog.tsx'
import type { CaptureViewport, ViewportImage } from '../screenshot.ts'

export function ScreenshotDialog({ capture, title, onPlace, onClose }: {
  capture: CaptureViewport
  title: string
  /** Hands the image to the canvas's existing import tray (including retry and undo). */
  onPlace?: (file: File) => void
  onClose(): void
}) {
  const [image, setImage] = useState<(ViewportImage & { url: string }) | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const placed = useRef(false)
  useEffect(() => {
    let live = true
    let url: string | undefined
    const controller = new AbortController()
    void capture(controller.signal).then((result) => {
      if (!live) return
      url = URL.createObjectURL(result.blob)
      setImage({ ...result, url })
    }).catch((cause: unknown) => {
      if (live) setError(cause instanceof Error ? cause.message : '截图失败，请重试')
    })
    return () => {
      live = false
      controller.abort()
      if (url) URL.revokeObjectURL(url)
    }
  }, [capture, attempt])
  const filename = `${title.replace(/[\\/:*?"<>|]/g, '_').slice(0, 100) || '3D 场景'}-视口截图.png`
  const place = () => {
    if (!image || !onPlace || placed.current) return
    placed.current = true
    try {
      onPlace(new File([image.blob], filename, { type: 'image/png' }))
    } catch (cause) {
      placed.current = false
      setError(cause instanceof Error ? cause.message : '无法导出到画布，请重试')
    }
  }
  return (
    <StudioDialog title="视口截图" onClose={onClose} overlayClassName="cc-scene-screenshot">
      <div className="cc-scene-screenshot-preview" aria-busy={!image && !error}>
        {image ? <img src={image.url} alt="当前 3D 视口截图" /> : <p role="status">{error ? '暂无截图' : '正在截取视口…'}</p>}
      </div>
      <p className="cc-scene-screenshot-caption">{image ? `${image.width} × ${image.height} · PNG · ` : ''}当前视口画面，包含已显示的辅助线。</p>
      {error && <p role="alert">{error}</p>}
      <footer>
        {!image && error && <button className="btn" onClick={() => { setError(''); setAttempt((n) => n + 1) }}>重新截图</button>}
        {image && <a className="btn" href={image.url} download={filename}><Download size={16} />下载 PNG</a>}
        {onPlace && <button className="btn btn-primary" disabled={!image} onClick={place}><ImagePlus size={16} />导出到画布</button>}
      </footer>
    </StudioDialog>
  )
}
