import { ModelCache, type Download, type ModelParts } from '../models/cache.ts'

// The asset viewer's models. Not the editor's cache, which is never emptied:
// models opened one after another on the library page would pile up there.
// The eight most recently opened stay; an older one is dropped and its
// geometries freed. Only one viewer is open at a time, so a dropped model is
// never on screen.

export class PreviewModels {
  private readonly cache: ModelCache
  private readonly max: number
  /** Least recently opened first. */
  private readonly recent: string[] = []

  constructor(max = 8, download?: Download) {
    this.cache = new ModelCache(download)
    this.max = max
  }

  load(revision: string): Promise<ModelParts> {
    const at = this.recent.indexOf(revision)
    if (at >= 0) this.recent.splice(at, 1)
    this.recent.push(revision)
    while (this.recent.length > this.max) this.cache.forget(this.recent.shift()!)
    return this.cache.load(revision)
  }
}

export const previewModels = new PreviewModels()
