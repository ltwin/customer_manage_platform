import { ShaderLib } from 'three'
import { commonGLSL, patternGLSL, uniformDeclarations } from './patterns.ts'
import type { MaterialEntry } from './catalog.ts'

// Puts a preset's pattern into MeshPhysicalMaterial's shaders at named
// chunks: the pattern's colour, roughness, metalness, bump and emission
// replace what the material's maps would give. If three renames a chunk the
// anchors are gone; the preset then draws as a plain principled material
// (its colour, roughness and metalness) and says so in the console, as
// lookAlongZ does for the AO. A unit test keeps the anchors in sight.

const main = 'void main() {'
const beginVertex = '#include <begin_vertex>'
const fragmentAnchors = {
  map: '#include <map_fragment>',
  roughness: '#include <roughnessmap_fragment>',
  metalness: '#include <metalnessmap_fragment>',
  normal: '#include <normal_fragment_maps>',
  emissive: '#include <emissivemap_fragment>',
}

export function canInject(vertexShader: string, fragmentShader: string): boolean {
  return vertexShader.includes(main) && vertexShader.includes(beginVertex) && fragmentShader.includes(main) && Object.values(fragmentAnchors).every((a) => fragmentShader.includes(a))
}

/** Whether this three.js has the anchors; checked once against its physical shader. */
export const patternsSupported = canInject(ShaderLib.physical.vertexShader, ShaderLib.physical.fragmentShader)

/** Rewrites the shaders for a preset; false, leaving them alone, when an anchor is missing. */
export function injectPattern(shader: { vertexShader: string; fragmentShader: string }, entry: MaterialEntry): boolean {
  const body = patternGLSL[entry.id]
  if (!body || !canInject(shader.vertexShader, shader.fragmentShader)) return false
  shader.vertexShader = shader.vertexShader
    .replace(main, `varying vec3 vPatPos;\nvarying vec3 vPatNormal;\n${main}`)
    .replace(
      beginVertex,
      `${beginVertex}
	vec3 patScale = vec3(length(modelMatrix[0].xyz), length(modelMatrix[1].xyz), length(modelMatrix[2].xyz));
	vPatPos = position * patScale;
	vPatNormal = normal / patScale;`,
    )
  const a = fragmentAnchors
  shader.fragmentShader = shader.fragmentShader
    .replace(main, `${uniformDeclarations(entry)}\n${commonGLSL}\n${body}\n${main}`)
    .replace(a.map, `${a.map}\n\tPatOut pat = patternEval();\n\tdiffuseColor.rgb = pat.albedo;`)
    .replace(a.roughness, `${a.roughness}\n\troughnessFactor = pat.rough;`)
    .replace(a.metalness, `${a.metalness}\n\tmetalnessFactor = pat.metal;`)
    .replace(a.normal, `${a.normal}\n\tnormal = patBumpNormal(-vViewPosition, normal, pat.height);`)
    .replace(a.emissive, `${a.emissive}\n\ttotalEmissiveRadiance += pat.emission;`)
  return true
}
