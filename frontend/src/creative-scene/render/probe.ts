// Whether this browser can draw a scene (design「渲染器创建入口」), apart
// from render/gpu.ts so the canvas page's job launcher can ask without
// loading three.

export type Adapter = { limits: Record<string, number> }
type GPUEntry = { requestAdapter(options?: { featureLevel?: string }): Promise<Adapter | null> }

const gpuEntry = (): GPUEntry | undefined => (navigator as Navigator & { gpu?: GPUEntry }).gpu

function webgl2Available(): boolean {
  try {
    const gl = document.createElement('canvas').getContext('webgl2')
    if (!gl) return false
    gl.getExtension('WEBGL_lose_context')?.loseContext()
    return true
  } catch {
    return false
  }
}

/** The WebGPU adapter, asked for the way three asks (WebGPUBackend.init); null without one. */
export async function adapter(): Promise<Adapter | null> {
  try {
    return (await gpuEntry()?.requestAdapter({ featureLevel: 'compatibility' })) ?? null
  } catch {
    return null
  }
}

/**
 * WebGL2, or a WebGPU adapter. Chrome exposes navigator.gpu even with no
 * adapter to give, so its presence says nothing.
 */
export async function probeRender(): Promise<boolean> {
  return webgl2Available() || (await adapter()) !== null
}

/**
 * Whether a device can be had again after one was lost: a WebGPU device made
 * and given back at once, or a WebGL2 context.
 */
export async function recoverDevice(): Promise<boolean> {
  const found = await adapter()
  if (found) {
    try {
      const device = await (found as Adapter & { requestDevice(): Promise<{ destroy(): void }> }).requestDevice()
      device.destroy()
      return true
    } catch {
      return false
    }
  }
  return webgl2Available()
}
