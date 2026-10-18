// The module a new code model starts from: the contract's example, a pillar
// with material slots, parts and an anchor.
export const defaultModuleCode = `// 一根柱子：底座、柱身、柱头。单位米，Y 轴向上，原点在底面中心，正面朝 +Z。
export const params = {
  height: { type: 'number', default: 3, min: 1, max: 12, step: 0.1, unit: 'm', label: '高度' },
  radius: { type: 'number', default: 0.25, min: 0.1, max: 1, step: 0.01, unit: 'm', label: '半径' },
  style: { type: 'enum', default: 'round', options: ['round', 'square'], label: '柱身' },
  broken: { type: 'boolean', default: false, label: '断裂' },
}
export const mount = 'floor'
export const lib_version = 1

export default function build({ params, THREE, lib }) {
  const { height, radius, style, broken } = params
  const stone = lib.material('stone', 'stone')
  const trim = lib.material('trim', 'stone')
  const pillar = new THREE.Group()

  const plinth = Math.min(radius * 0.5, height * 0.1)
  const base = new THREE.Mesh(new THREE.BoxGeometry(radius * 3, plinth, radius * 3), trim)
  base.name = 'part:base'
  base.position.y = plinth / 2
  pillar.add(base)

  const shaftHeight = height - plinth * 2
  let shaft = new THREE.Mesh(
    style === 'square'
      ? new THREE.BoxGeometry(radius * 2, shaftHeight, radius * 2)
      : new THREE.CylinderGeometry(radius * 0.9, radius, shaftHeight, 32),
    stone,
  )
  shaft.position.y = plinth + shaftHeight / 2
  if (broken) {
    // 斜切掉柱身上段
    const cut = new THREE.Mesh(new THREE.BoxGeometry(radius * 6, shaftHeight, radius * 6), stone)
    cut.position.y = plinth + shaftHeight * 1.15
    cut.rotation.z = 0.35
    shaft = lib.subtract(shaft, cut)
  }
  shaft.name = 'part:shaft'
  pillar.add(shaft)

  if (!broken) {
    const capital = new THREE.Mesh(new THREE.BoxGeometry(radius * 2.6, plinth, radius * 2.6), trim)
    capital.name = 'part:capital'
    capital.position.y = height - plinth / 2
    pillar.add(capital)

    const top = new THREE.Object3D()
    top.name = 'anchor:top'
    top.position.y = height
    pillar.add(top)
  }
  return pillar
}
`
