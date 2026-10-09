import { useSyncExternalStore } from 'react'

// 拖入或上传到节点的文件，在服务器内容出现前直接用本机文件显示。
// 卡片按节点订阅；服务器内容加载出来后再撤掉，避免中间闪空。
export type LocalMedia = {
  url: string
  kind: 'image' | 'video' | 'audio'
  width: number | null
  height: number | null
  /** 0–1 的上传进度；null 表示还没开始。 */
  progress: number | null
  failed: boolean
}

const entries = new Map<string, LocalMedia>()
const listeners = new Set<() => void>()
let version = 0
const emit = () => {
  version++
  listeners.forEach((l) => l())
}

export const localMedia = {
  get: (nodeID: string) => entries.get(nodeID) ?? null,
  set(nodeID: string, file: File, kind: LocalMedia['kind']) {
    localMedia.clear(nodeID)
    const entry: LocalMedia = {
      url: URL.createObjectURL(file),
      kind,
      width: null,
      height: null,
      progress: null,
      failed: false,
    }
    entries.set(nodeID, entry)
    emit()
    return entry
  },
  patch(nodeID: string, value: Partial<LocalMedia>) {
    const old = entries.get(nodeID)
    if (!old) return
    entries.set(nodeID, { ...old, ...value })
    emit()
  },
  clear(nodeID: string) {
    const old = entries.get(nodeID)
    if (!old) return
    URL.revokeObjectURL(old.url)
    entries.delete(nodeID)
    emit()
  },
  subscribe(listener: () => void) {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  },
}

// 画布据此重算节点尺寸：本机读出的比例在服务器内容到达前就生效。
export function useLocalMediaVersion(): number {
  return useSyncExternalStore(localMedia.subscribe, () => version)
}

export function useLocalMedia(nodeID: string): LocalMedia | null {
  return useSyncExternalStore(localMedia.subscribe, () => localMedia.get(nodeID))
}

// 本机读出图片/视频的像素尺寸，节点高度不必等服务器。
export function readMediaSize(
  url: string,
  kind: LocalMedia['kind'],
): Promise<{ width: number; height: number } | null> {
  if (kind === 'audio') return Promise.resolve(null)
  return new Promise((resolve) => {
    if (kind === 'image') {
      const img = new Image()
      img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight })
      img.onerror = () => resolve(null)
      img.src = url
      return
    }
    const video = document.createElement('video')
    video.preload = 'metadata'
    video.onloadedmetadata = () =>
      resolve(video.videoWidth ? { width: video.videoWidth, height: video.videoHeight } : null)
    video.onerror = () => resolve(null)
    video.src = url
  })
}
