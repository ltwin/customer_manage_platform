// scene-world-space-gi S0's stress scene (scene-trace-spike design「压力场景生成器」): about 20 million
// triangles of distinct geometry in 2000 instances, a furnished hall of procedural surfaces. Generated, not
// stored: the geometry is hundreds of megabytes. The same seed always gives the same bytes. Plain ES, no three
// and no DOM, so the spike page and the Node tests both run it.
//
// Every surface is a grid over (u, v) in [0, 1]², two triangles a cell, wound counter-clockwise seen from
// outside (the normal is du × dv): blobs (spheres pushed out by noise), torus knots, fluted columns and
// rolling terrain tiles, each about half a metre across and standing on y = 0.

/** Grid cells across u for one along v, by surface: long knots, round blobs and columns, square tiles. */
const aspect = { blob: 2, knot: 4, column: 2, terrain: 1 }

/** The grid of a surface with about this many triangles: [cells along u, cells along v]. */
export function cells(type, triangles) {
  const nv = Math.max(2, Math.round(Math.sqrt(triangles / (2 * aspect[type]))))
  return [aspect[type] * nv, nv]
}

/** The triangle and instance counts a spec gives, without drawing anything. */
export function counts(spec) {
  let triangles = 12 // the room's box
  let geometries = 1
  for (const family of spec.geometries) {
    const [nu, nv] = cells(family.type, family.triangles)
    triangles += family.count * 2 * nu * nv
    geometries += family.count
  }
  const heroes = spec.geometries.filter((f) => f.hero).reduce((n, f) => n + f.count, 0)
  return { triangles, geometries, instances: 5 + heroes + spec.layout.instances }
}

const gcd = (a, b) => (b ? gcd(b, a % b) : a)

/** mulberry32: a small, fast, seeded generator of numbers in [0, 1). */
function random(seed) {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** Value noise in [0, 1] at a point, from a lattice hashed with seed; three octaves summed (fBm). */
function noise(seed) {
  const lattice = (x, y, z) => {
    let h = Math.imul(x, 0x8da6b343) ^ Math.imul(y, 0xd8163841) ^ Math.imul(z, 0xcb1ab31f) ^ seed
    h = Math.imul(h ^ (h >>> 13), 0x5bd1e995)
    return ((h ^ (h >>> 15)) >>> 0) / 4294967296
  }
  const fade = (t) => t * t * (3 - 2 * t)
  const at = (x, y, z) => {
    const ix = Math.floor(x), iy = Math.floor(y), iz = Math.floor(z)
    const fx = fade(x - ix), fy = fade(y - iy), fz = fade(z - iz)
    const mix = (a, b, t) => a + (b - a) * t
    const corner = (dx, dy, dz) => lattice(ix + dx, iy + dy, iz + dz)
    return mix(
      mix(mix(corner(0, 0, 0), corner(1, 0, 0), fx), mix(corner(0, 1, 0), corner(1, 1, 0), fx), fy),
      mix(mix(corner(0, 0, 1), corner(1, 0, 1), fx), mix(corner(0, 1, 1), corner(1, 1, 1), fx), fy),
      fz,
    )
  }
  return (x, y, z) => (at(x, y, z) + 0.5 * at(2 * x, 2 * y, 2 * z) + 0.25 * at(4 * x, 4 * y, 4 * z)) / 1.75
}

/**
 * A surface's point at (u, v) and, for knots, the centre of its tube there, which says which side is out;
 * its parameters drawn from rand. Closed along u: all but the tiles; along v: the knots.
 */
function surface(type, rand) {
  const n = noise(Math.floor(rand() * 2 ** 31))
  const tau = 2 * Math.PI
  if (type === 'blob') {
    const r = 0.18 + 0.08 * rand(), amp = 0.25 + 0.25 * rand(), freq = 2 + 4 * rand()
    return {
      closedU: true, closedV: false,
      at(u, v) {
        const th = tau * u, ph = Math.PI * v
        const d = [Math.sin(ph) * Math.cos(th), Math.cos(ph), Math.sin(ph) * Math.sin(th)]
        const k = r * (1 + amp * (n(d[0] * freq + 7, d[1] * freq, d[2] * freq) - 0.5))
        return [d[0] * k, d[1] * k, d[2] * k]
      },
      out: (p) => p,
    }
  }
  if (type === 'knot') {
    // p and q coprime: otherwise the curve runs the same path more than once and the tube lies over itself.
    const p = 2 + Math.floor(rand() * 3)
    let q = 3 + Math.floor(rand() * 3)
    while (gcd(p, q) !== 1) q++
    const R = 0.17, rk = 0.07, tube = 0.025 + 0.02 * rand()
    const curve = (t) => [(R + rk * Math.cos(q * t)) * Math.cos(p * t), (R + rk * Math.cos(q * t)) * Math.sin(p * t), rk * Math.sin(q * t)]
    return {
      closedU: true, closedV: true,
      at(u, v) {
        const t = tau * u
        const a = curve(t), b = curve(t + 0.001)
        const T = norm(sub(b, a)), N0 = norm(add(a, b)), B = norm(cross(T, N0)), N = cross(B, T)
        const c = Math.cos(tau * v) * tube, s = Math.sin(tau * v) * tube
        return [a[0] + c * N[0] + s * B[0], a[1] + c * N[1] + s * B[1], a[2] + c * N[2] + s * B[2]]
      },
      out(pt, u) {
        return sub(pt, curve(tau * u))
      },
    }
  }
  if (type === 'column') {
    const r = 0.1 + 0.05 * rand(), h = 0.6 + 0.5 * rand(), grooves = 8 + Math.floor(rand() * 17), depth = 0.06 + 0.06 * rand()
    return {
      closedU: true, closedV: false,
      at(u, v) {
        const th = tau * u
        const k = r * (1 - depth * (0.5 + 0.5 * Math.cos(grooves * th))) * (1 + 0.15 * (n(Math.cos(th) * 2, v * 3, Math.sin(th) * 2) - 0.5))
        return [Math.cos(th) * k, h * (1 - v), Math.sin(th) * k]
      },
      out: (pt) => [pt[0], 0, pt[2]],
    }
  }
  // terrain: a tile of rolling ground; v runs towards -z so that du × dv is up.
  const size = 0.5 + 0.2 * rand(), amp = 0.06 + 0.08 * rand(), freq = 3 + 4 * rand()
  return {
    closedU: false, closedV: false,
    at(u, v) {
      const x = size * (u - 0.5), z = size * (0.5 - v)
      return [x, amp * n(x * freq, 0.5, z * freq), z]
    },
    out: () => [0, 1, 0],
  }
}

const sub = (a, b) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]]
const add = (a, b) => [a[0] + b[0], a[1] + b[1], a[2] + b[2]]
const cross = (a, b) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]]
const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
const norm = (a) => {
  const l = Math.hypot(a[0], a[1], a[2]) || 1
  return [a[0] / l, a[1] / l, a[2] / l]
}

/** One surface as a grid: positions, normals (central differences, wrapped where it is closed) and indices; lifted to stand on y = 0. */
function grid(type, triangles, rand) {
  const s = surface(type, rand)
  const [nu, nv] = cells(type, triangles)
  const row = nu + 1
  const positions = new Float32Array(row * (nv + 1) * 3)
  for (let j = 0; j <= nv; j++) {
    for (let i = 0; i <= nu; i++) {
      // The seam's last column repeats the first exactly, so the surface closes without a crack.
      const p = s.at(s.closedU && i === nu ? 0 : i / nu, s.closedV && j === nv ? 0 : j / nv)
      positions.set(p, (j * row + i) * 3)
    }
  }
  const P = (i, j) => {
    const k = (j * row + i) * 3
    return [positions[k], positions[k + 1], positions[k + 2]]
  }
  // Which way du × dv points, from the middle of the grid: the indices are wound to match the outside.
  const mid = [Math.floor(nu / 2), Math.floor(nv / 2)]
  const flip = dot(cross(sub(P(mid[0] + 1, mid[1]), P(mid[0], mid[1])), sub(P(mid[0], mid[1] + 1), P(mid[0], mid[1]))), s.out(P(...mid), mid[0] / nu)) < 0
  const normals = new Float32Array(positions.length)
  for (let j = 0; j <= nv; j++) {
    for (let i = 0; i <= nu; i++) {
      const i0 = i > 0 ? i - 1 : s.closedU ? nu - 1 : i, i1 = i < nu ? i + 1 : s.closedU ? 1 : i
      const j0 = j > 0 ? j - 1 : s.closedV ? nv - 1 : j, j1 = j < nv ? j + 1 : s.closedV ? 1 : j
      let nrm = cross(sub(P(i1, j), P(i0, j)), sub(P(i, j1), P(i, j0)))
      if (flip) nrm = [-nrm[0], -nrm[1], -nrm[2]]
      // At a blob's poles the u-difference vanishes: the way out is the normal.
      if (Math.hypot(...nrm) < 1e-12) nrm = s.out(P(i, j), i / nu)
      normals.set(norm(nrm), (j * row + i) * 3)
    }
  }
  const indices = new Uint32Array(nu * nv * 6)
  let k = 0
  for (let j = 0; j < nv; j++) {
    for (let i = 0; i < nu; i++) {
      const a = j * row + i, b = a + 1, c = a + row, d = c + 1
      if (flip) indices.set([a, c, b, b, c, d], k)
      else indices.set([a, b, c, b, d, c], k)
      k += 6
    }
  }
  let minY = Infinity
  for (let i = 1; i < positions.length; i += 3) minY = Math.min(minY, positions[i])
  for (let i = 1; i < positions.length; i += 3) positions[i] -= minY
  return { positions, normals, indices }
}

/** The room's box: a unit cube of 12 triangles, flat-shaded, scaled into the floor and the walls. */
function box() {
  const positions = [], normals = [], indices = []
  const faces = [[0, 1], [0, -1], [1, 1], [1, -1], [2, 1], [2, -1]]
  for (const [axis, sign] of faces) {
    const n = [0, 0, 0]
    n[axis] = sign
    const u = [0, 0, 0], v = [0, 0, 0]
    u[(axis + 1) % 3] = 1
    v[(axis + 2) % 3] = 1
    const base = positions.length / 3
    for (const [a, b] of [[-0.5, -0.5], [0.5, -0.5], [-0.5, 0.5], [0.5, 0.5]]) {
      positions.push(n[0] * 0.5 + a * u[0] + b * v[0], n[1] * 0.5 + a * u[1] + b * v[1], n[2] * 0.5 + a * u[2] + b * v[2])
      normals.push(...n)
    }
    // u × v is the face's normal for the positive faces; the negative ones are wound the other way.
    if (sign > 0) indices.push(base, base + 1, base + 2, base + 1, base + 3, base + 2)
    else indices.push(base, base + 2, base + 1, base + 1, base + 2, base + 3)
  }
  return { positions: new Float32Array(positions), normals: new Float32Array(normals), indices: new Uint32Array(indices) }
}

/** A column-major 4 × 4 matrix (three's Matrix4.elements): scale, turn about y, then move. */
function matrix([x, y, z], yaw, scale) {
  const c = Math.cos(yaw) * scale, s = Math.sin(yaw) * scale
  return [c, 0, -s, 0, 0, scale, 0, 0, s, 0, c, 0, x, y, z, 1]
}
const boxMatrix = ([x, y, z], [w, h, d]) => [w, 0, 0, 0, 0, h, 0, 0, 0, 0, d, 0, x, y, z, 1]

/**
 * The stress scene of a spec: geometries [{id, type, positions, normals, indices}] and instances
 * [{geometry, matrix}], matrices column-major. only (a list of geometry ids) draws just those geometries;
 * the instances are the same either way.
 */
export function generate(spec, { only } = {}) {
  const geometries = []
  const plain = []
  const heroes = []
  const shell = { id: 'room', type: 'box', ...box() }
  if (!only || only.includes('room')) geometries.push(shell)
  let serial = 0
  for (const family of spec.geometries) {
    for (let k = 0; k < family.count; k++) {
      const id = `${family.type}-${String(serial).padStart(2, '0')}`
      // Each geometry draws from its own stream: drawing some leaves the others as they are.
      const rand = random(spec.seed ^ Math.imul(serial + 1, 0x9e3779b1))
      if (!only || only.includes(id)) geometries.push({ id, type: family.type, ...grid(family.type, family.triangles, rand) })
      ;(family.hero ? heroes : plain).push(id)
      serial++
    }
  }
  const { width, depth, height, wall } = spec.room
  const instances = [
    { geometry: 'room', matrix: boxMatrix([0, -wall / 2, 0], [width, wall, depth]) },
    { geometry: 'room', matrix: boxMatrix([0, height / 2, -depth / 2 - wall / 2], [width, height, wall]) },
    { geometry: 'room', matrix: boxMatrix([0, height / 2, depth / 2 + wall / 2], [width, height, wall]) },
    { geometry: 'room', matrix: boxMatrix([-width / 2 - wall / 2, height / 2, 0], [wall, height, depth]) },
    { geometry: 'room', matrix: boxMatrix([width / 2 + wall / 2, height / 2, 0], [wall, height, depth]) },
  ]
  const rand = random(spec.seed)
  const { margin, scale: [s0, s1], heroScale } = spec.layout
  // The heroes stand near the camera, a metre and a half either side of its line of sight.
  const [cx, , cz] = spec.camera.position
  heroes.forEach((id, i) => instances.push({ geometry: id, matrix: matrix([cx + (i % 2 ? 1.5 : -1.5), 0, cz - 3 - i], rand() * 2 * Math.PI, heroScale) }))
  // The rest on a jittered grid over the floor, as many cells as instances, laid out at the room's proportions.
  const n = spec.layout.instances
  const across = Math.ceil(Math.sqrt((n * (width - 2 * margin)) / (depth - 2 * margin)))
  const along = Math.ceil(n / across)
  const dx = (width - 2 * margin) / across, dz = (depth - 2 * margin) / along
  for (let k = 0; k < n; k++) {
    const i = k % across, j = Math.floor(k / across)
    const x = -width / 2 + margin + (i + 0.25 + 0.5 * rand()) * dx
    const z = -depth / 2 + margin + (j + 0.25 + 0.5 * rand()) * dz
    instances.push({ geometry: plain[Math.floor(rand() * plain.length)], matrix: matrix([x, 0, z], rand() * 2 * Math.PI, s0 + (s1 - s0) * rand()) })
  }
  return { geometries, instances }
}
