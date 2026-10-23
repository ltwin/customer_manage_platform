import test from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

// One shading source (Epic design「验证与交付」, G2): everything the app
// draws goes through WebGPURenderer and TSL. The source is parsed, not
// searched as text, so a comment naming ShaderMaterial is no hit.

const src = fileURLToPath(new URL('../src/', import.meta.url))

/** three's nodes forked into the project, compared with upstream on every upgrade; a file cannot exempt itself. */
const exempt = new Set(['creative-scene/render/nodes/GTAONode.js'])

const identifiers = new Set(['WebGLRenderer', 'WebGLRenderTarget', 'ShaderMaterial', 'RawShaderMaterial', 'onBeforeCompile', 'ShaderLib', 'ShaderChunk', 'glslFn', 'wgslFn'])
const drei = new Set(['CameraControls', 'CameraControlsImpl', 'OrbitControls', 'GizmoHelper', 'GizmoViewport', 'TransformControls'])
const glsl = ['gl_FragColor', 'gl_Position', '#include <', 'precision highp', 'precision mediump', 'void main(']
// The fat-line geometries carry no shader; lines/webgpu/ builds on them.
const lineGeometries = /lines\/(LineSegmentsGeometry|LineGeometry|WireframeGeometry2)\.js$/

function forbiddenImport(path: string): string | null {
  if (/^three\/(examples\/jsm|addons)\/postprocessing\//.test(path)) return 'WebGL post-processing'
  if (/^three\/(examples\/jsm|addons)\/objects\/Sky\.js$/.test(path)) return 'the GLSL Sky'
  if (path === 'three-stdlib' || path.startsWith('three-stdlib/')) return 'three-stdlib'
  if (/^three\/(examples\/jsm|addons)\/lines\//.test(path) && !/\/lines\/webgpu\//.test(path) && !lineGeometries.test(path)) return 'WebGL lines'
  return null
}

// What a picture shows must not depend on when it is taken (W2, patterns:
// 场景着色写法): the sky's time comes from the scene, never from the clock.
const clockless = /^creative-scene\/(world|render)\//

export type Hit = { line: number; rule: string; text: string }

export function check(fileName: string, source: string): Hit[] {
  const file = ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true, fileName.endsWith('x') ? ts.ScriptKind.TSX : undefined)
  const hits: Hit[] = []
  const hit = (node: ts.Node, rule: string, text: string) => hits.push({ line: file.getLineAndCharacterOfPosition(node.getStart(file)).line + 1, rule, text })
  const visit = (node: ts.Node) => {
    if (ts.isIdentifier(node) && identifiers.has(node.text)) hit(node, 'WebGL identifier', node.text)
    else if (ts.isPrivateIdentifier(node) && identifiers.has(node.text.slice(1))) hit(node, 'WebGL identifier', node.text)
    if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      const path = node.moduleSpecifier.text
      const why = forbiddenImport(path)
      if (why) hit(node, why, path)
      const named = ts.isImportDeclaration(node) ? node.importClause?.namedBindings : undefined
      if (path === '@react-three/drei') {
        if (named && ts.isNamedImports(named)) for (const e of named.elements) if (!drei.has((e.propertyName ?? e.name).text)) hit(e, 'drei outside the list', (e.propertyName ?? e.name).text)
        if (!named || !ts.isNamedImports(named)) hit(node, 'drei outside the list', node.getText(file))
      }
    }
    if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword && node.arguments[0] && ts.isStringLiteral(node.arguments[0])) {
      const why = forbiddenImport(node.arguments[0].text)
      if (why) hit(node, why, node.arguments[0].text)
    }
    if (clockless.test(fileName)) {
      if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression) && ts.isIdentifier(node.expression.expression) && node.expression.expression.text === 'Date' && node.expression.name.text === 'now') hit(node, 'wall clock', 'Date.now()')
      if (ts.isNewExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'Date' && (node.arguments ?? []).length === 0) hit(node, 'wall clock', 'new Date()')
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) || ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node)) {
      for (const g of glsl) if (node.text.includes(g)) hit(node, 'GLSL', g)
    }
    ts.forEachChild(node, visit)
  }
  visit(file)
  return hits
}

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name)
    if (e.isDirectory()) return sources(path)
    return /\.(ts|tsx|js)$/.test(e.name) && !e.name.endsWith('.d.ts') ? [path] : []
  })
}

test('frontend/src draws only through WebGPURenderer and TSL', () => {
  const found: string[] = []
  for (const path of sources(src)) {
    const name = relative(src, path).split('\\').join('/')
    if (exempt.has(name)) continue
    for (const h of check(name, readFileSync(path, 'utf8'))) found.push(`${name}:${h.line} ${h.rule}: ${h.text}`)
  }
  assert.deepEqual(found, [])
})

test('the check finds each kind of breach, and not a name in a comment', () => {
  const rules = (source: string, name = 'sample.ts') => check(name, source).map((h) => h.rule)
  assert.deepEqual(rules(`import { ShaderMaterial } from 'three'\nnew ShaderMaterial({})`), ['WebGL identifier', 'WebGL identifier'])
  assert.deepEqual(rules(`material.onBeforeCompile = () => {}`), ['WebGL identifier'])
  assert.deepEqual(rules(`import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'`), ['WebGL post-processing'])
  assert.deepEqual(rules(`import { Sky } from 'three/addons/objects/Sky.js'`), ['the GLSL Sky'])
  assert.deepEqual(rules(`import { Sky } from 'three-stdlib'`), ['three-stdlib'])
  assert.deepEqual(rules(`import { LineMaterial } from 'three/addons/lines/LineMaterial.js'`), ['WebGL lines'])
  assert.deepEqual(rules(`const m = await import('three/addons/postprocessing/OutputPass.js')`), ['WebGL post-processing'])
  assert.deepEqual(rules(`import { Edges, CameraControls } from '@react-three/drei'`, 'a.tsx'), ['drei outside the list'])
  assert.deepEqual(rules('const fragment = `void main() { gl_FragColor = vec4(1.0); }`'), ['GLSL', 'GLSL'])
  assert.deepEqual(rules(`const s = '#include <common>'`), ['GLSL'])
  assert.deepEqual(rules(`const t = Date.now()\nconst d = new Date()\nconst e = new Date`, 'creative-scene/world/astro.ts'), ['wall clock', 'wall clock', 'wall clock'])
  assert.deepEqual(rules(`const t = Date.now()`, 'creative-scene/render/sky.ts'), ['wall clock'])
  // Not breaches.
  assert.deepEqual(rules(`// three's ShaderMaterial and onBeforeCompile are gone; void main( lives in no string.\n/* WebGLRenderer */\nconst x = 1`), [])
  assert.deepEqual(rules(`import { LineSegments2 } from 'three/addons/lines/webgpu/LineSegments2.js'\nimport { LineSegmentsGeometry } from 'three/addons/lines/LineSegmentsGeometry.js'`), [])
  assert.deepEqual(rules(`import { GizmoHelper, GizmoViewport } from '@react-three/drei'`, 'a.tsx'), [])
  assert.deepEqual(rules(`const label = 'ShaderMaterial'`), [], 'a string naming the class is no use of it')
  assert.deepEqual(rules(`const d = new Date(utcMs)\nconst t = performance.now()`, 'creative-scene/world/astro.ts'), [], 'an instant from the scene, and timing')
  assert.deepEqual(rules(`const t = Date.now()`, 'creative-scene/objects/Lighting.tsx'), [], 'the clock is for the editor, outside world/ and render/')
})
