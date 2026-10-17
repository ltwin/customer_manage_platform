import { LoadingManager, type Group } from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'

// The only way model GLBs reach GLTFLoader, for freshly baked ones and for
// those loaded from the server alike. Callers inspect the bytes first
// (inspect.ts), which already rejects every uri; the URL modifier is the second
// line: a uri left in the file would otherwise make the editor page, with the
// account's cookies, request it on the module's behalf.

const allowed = /^(blob|data):/i

export async function parseGLB(glb: ArrayBuffer): Promise<Group> {
  let blocked: string | null = null
  const manager = new LoadingManager()
  manager.setURLModifier((url) => {
    if (allowed.test(url)) return url
    blocked ??= url
    return 'data:,'
  })
  const gltf = await new GLTFLoader(manager).parseAsync(glb, '')
  if (blocked !== null) throw new Error(`GLB 引用了外部地址 ${blocked}`)
  return gltf.scene
}
