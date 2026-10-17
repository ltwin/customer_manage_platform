import * as THREE from 'three'
import { GLTFExporter } from 'three/examples/jsm/exporters/GLTFExporter.js'
import {
  CompileFailure,
  checkSyntax,
  fillParams,
  invalid,
  readModule,
  runtimeError,
  seeded,
} from './compile.ts'
import { createLib1 } from './lib.ts'
import {
  CONTRACT_VERSION,
  LIB_VERSIONS,
  MAX_CODE_BYTES,
  MAX_GLB_BYTES,
  compileError,
  isCompileRequest,
  utf8Length,
  type CompileRequest,
  type CompileResultMessage,
} from './protocol.ts'

// The compile Worker. The sandbox page creates one per compile and destroys
// it after the first message, so whatever the module changes in this global
// (three.js prototypes, lib, Math) never reaches another compile.

type WorkerScope = {
  onmessage: ((e: MessageEvent<unknown>) => void) | null
  postMessage(message: unknown, options?: { transfer?: Transferable[] }): void
}
const scope = globalThis as unknown as WorkerScope
// Taken before the module runs, so overriding postMessage cannot hide a result.
const post = scope.postMessage.bind(scope)

const libraries: Record<number, () => unknown> = { 1: createLib1 }

// Clearer errors for what the static GLB subset rejects anyway; the host and
// the server still check the GLB itself.
function checkExportable(root: THREE.Object3D): void {
  root.traverse((o) => {
    if (o instanceof THREE.Light || o instanceof THREE.Camera) throw invalid(`不能包含灯光或摄像机（${o.type}）`)
    if (o instanceof THREE.Line || o instanceof THREE.Points || o instanceof THREE.Sprite) {
      throw invalid(`只能包含三角网格，不能用 ${o.type}`)
    }
    if (o instanceof THREE.SkinnedMesh || o instanceof THREE.InstancedMesh || o instanceof THREE.BatchedMesh) {
      throw invalid(`不能使用 ${o.type}`)
    }
    if (!(o instanceof THREE.Mesh)) return
    if (Object.keys(o.geometry.morphAttributes).length > 0) throw invalid('不能使用形变目标')
    const materials: unknown[] = Array.isArray(o.material) ? o.material : [o.material]
    for (const m of materials) {
      if (!(m instanceof THREE.MeshStandardMaterial) || m instanceof THREE.MeshPhysicalMaterial) {
        const type = m instanceof THREE.Material ? m.type : typeof m
        throw invalid(`材质只能用 MeshStandardMaterial 或 lib.material（这里是 ${type}）`)
      }
      if (Object.values(m).some((v) => v instanceof THREE.Texture)) throw invalid('材质不能使用纹理')
    }
  })
}

async function compile(request: CompileRequest): Promise<CompileResultMessage> {
  const { id, code } = request
  if (utf8Length(code) > MAX_CODE_BYTES) {
    return { type: 'result', id, ok: false, error: compileError('limit', '代码超过 64 KiB') }
  }
  let moduleURL = ''
  try {
    checkSyntax(code)
    // Step 2: three.js draws object UUIDs from Math.random, so it cannot be
    // removed; a fixed seed keeps the bake reproducible.
    Math.random = seeded(0x3d5ce4e)
    Reflect.deleteProperty(globalThis, 'Date')

    moduleURL = URL.createObjectURL(new Blob([code], { type: 'text/javascript' }))
    const exports = (await import(/* @vite-ignore */ moduleURL)) as Record<string, unknown>
    const info = readModule(exports, request.lib_version, LIB_VERSIONS, CONTRACT_VERSION)
    const params = fillParams(info.params_schema, request.params)
    const lib = libraries[info.lib_version]()
    const build = exports.default as (input: unknown) => unknown
    const root = await build(Object.freeze({ params: Object.freeze({ ...params }), THREE, lib, rng: seeded(1) }))
    if (!(root instanceof THREE.Object3D)) {
      throw invalid('build 须返回 THREE.Object3D')
    }
    checkExportable(root)
    const glb = await new GLTFExporter().parseAsync(root, { binary: true })
    if (!(glb instanceof ArrayBuffer)) {
      throw new CompileFailure(compileError('runtime', '导出结果不是 GLB'))
    }
    if (glb.byteLength > MAX_GLB_BYTES) {
      throw new CompileFailure(compileError('limit', 'GLB 超过 16 MiB'))
    }
    return { type: 'result', id, ok: true, glb, module: { ...info, params } }
  } catch (err) {
    return { type: 'result', id, ok: false, error: runtimeError(err, moduleURL || '\0') }
  }
}

scope.onmessage = (e) => {
  scope.onmessage = null
  const request = e.data
  if (!isCompileRequest(request)) return
  void compile(request)
    .then((result) => post(result, result.ok ? { transfer: [result.glb] } : undefined))
    .catch(() => post({ type: 'result', id: request.id, ok: false, error: compileError('runtime', '编译结果无法传回') }))
}
