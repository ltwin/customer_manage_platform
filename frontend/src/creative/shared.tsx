import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { authorizedFetch } from '../auth/session'
import { assetURL } from './api'

const MediaCache = createContext<Map<string, string> | null>(null)
export function WorkspaceMediaCache({ children }: { children: ReactNode }) {
  const cache = useRef(new Map<string, string>())
  useEffect(() => {
    const map = cache.current
    return () => {
      for (const url of map.values()) URL.revokeObjectURL(url)
      map.clear()
    }
  }, [])
  return (
    <MediaCache.Provider value={cache.current}>{children}</MediaCache.Provider>
  )
}
export function AssetImage({
  workspaceID,
  assetID,
  checksum,
  alt,
}: {
  workspaceID: string
  assetID?: string
  checksum?: string
  alt: string
}) {
  const cache = useContext(MediaCache)
  const cacheKey = `${assetID ?? ''}:${checksum ?? ''}`
  const [src, setSrc] = useState(cache?.get(cacheKey) ?? '')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    let objectURL = ''
    const cached = cache?.get(cacheKey)
    setSrc(cached ?? '')
    setFailed(false)
    if (cached) return
    if (assetID && checksum) {
      void authorizedFetch(assetURL(workspaceID, assetID, checksum), {
        signal: controller.signal,
      })
        .then(async (res) => {
          if (!res.ok) throw new Error('图片暂不可用')
          return res.blob()
        })
        .then((blob) => {
          if (!controller.signal.aborted) {
            objectURL = URL.createObjectURL(blob)
            cache?.set(cacheKey, objectURL)
            setSrc(objectURL)
          }
        })
        .catch(() => {
          if (!controller.signal.aborted) setFailed(true)
        })
    }
    return () => {
      controller.abort()
      if (objectURL && !cache) URL.revokeObjectURL(objectURL)
    }
  }, [workspaceID, assetID, checksum, cache, cacheKey])
  return src ? (
    <img src={src} alt={alt} />
  ) : (
    <div className="cw-image-fallback" role="img" aria-label={alt}>
      {failed
        ? '图片暂不可用'
        : assetID
          ? '正在加载图片…'
          : '在这里收集下一次拍摄的灵感'}
    </div>
  )
}
