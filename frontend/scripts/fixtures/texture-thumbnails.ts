// The page scripts/render-texture-thumbnails.mjs drives: renders a built-in
// textured material on the material ball, as a material asset's card is
// rendered (library/thumbnail.ts), and gives it back as WebP.
import { setAuthenticated } from '../../src/auth/session.ts'
import { renderMaterialBall } from '../../src/creative-scene/library/thumbnail.ts'
import { texturedEntries } from '../../src/creative-scene/materials/catalog.ts'

setAuthenticated({ access_token: 'fixture', token_type: 'Bearer', expires_in: 600 }, {
  id: 'thumbnail-account', email: 'thumbnails@example.invalid', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai',
})

const size = 256

async function webp(png: Blob): Promise<string> {
  const bitmap = await createImageBitmap(png, { resizeWidth: size, resizeHeight: size, resizeQuality: 'high' })
  const canvas = new OffscreenCanvas(size, size)
  canvas.getContext('2d')!.drawImage(bitmap, 0, 0)
  bitmap.close()
  const bytes = new Uint8Array(await (await canvas.convertToBlob({ type: 'image/webp', quality: 0.9 })).arrayBuffer())
  let binary = ''
  for (const b of bytes) binary += String.fromCharCode(b)
  return btoa(binary)
}

const page = window as unknown as { builtinIDs: string[]; renderBuiltin: (id: string) => Promise<string> }
page.builtinIDs = texturedEntries.map((m) => m.id)
page.renderBuiltin = async (id) => webp(await renderMaterialBall({ preset: id, params: {} }))
document.body.dataset.ready = '1'
