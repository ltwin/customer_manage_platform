import { LoadingManager } from 'three'
import { DRACOLoader } from 'three/examples/jsm/loaders/DRACOLoader.js'
import dracoWrapper from 'three/examples/jsm/libs/draco/gltf/draco_wasm_wrapper.js?url'
import dracoWasm from 'three/examples/jsm/libs/draco/gltf/draco_decoder.wasm?url'
import type { Decoders } from './parse.ts'

// The Draco decoder ships with the frontend and is fetched only when a file
// needs it. Its own manager: the GLB's refuses every URL but data: and blob:.

export const bundledDecoders: Decoders = {
  draco: () => new DRACOLoader(new LoadingManager()).setDecoderPath({ js: dracoWrapper, wasm: dracoWasm }),
}
