// Equal keys numbered once: an open-addressing table on three numbers. On
// the 900k corners of a 300k-triangle mesh, string keys in a Map took
// hundreds of milliseconds (scene-face-materials design「性能评估」).

/**
 * Each of `count` items' number among the distinct keys, in order of first
 * appearance; `key` writes an item's three numbers. firsts holds each
 * number's first item.
 */
export function weld(count: number, key: (i: number, out: Float64Array) => void): { ids: Uint32Array; firsts: Uint32Array; size: number } {
  let capacity = 16
  while (capacity < count * 2) capacity *= 2
  const mask = capacity - 1
  const table = new Int32Array(capacity).fill(-1)
  const keys = new Float64Array(count * 3)
  const firsts = new Uint32Array(count)
  const ids = new Uint32Array(count)
  const k = new Float64Array(3)
  let size = 0
  for (let i = 0; i < count; i++) {
    key(i, k)
    let h = hash(k[0], k[1], k[2]) & mask
    for (;;) {
      const n = table[h]
      if (n < 0) {
        table[h] = size
        keys[3 * size] = k[0]
        keys[3 * size + 1] = k[1]
        keys[3 * size + 2] = k[2]
        firsts[size] = i
        ids[i] = size++
        break
      }
      if (keys[3 * n] === k[0] && keys[3 * n + 1] === k[1] && keys[3 * n + 2] === k[2]) {
        ids[i] = n
        break
      }
      h = (h + 1) & mask
    }
  }
  return { ids, firsts: firsts.subarray(0, size), size }
}

/**
 * The three numbers mixed into 32 bits, every input bit reaching the low
 * ones the table uses. Float32 bit patterns of round coordinates end in
 * zeros: multiplied and masked as they were, a 256 × 256 grid collided
 * into a few slots and took seconds. Only the hash wraps the numbers to 32
 * bits; equality reads them whole.
 */
function hash(x: number, y: number, z: number): number {
  let h = fmix(x | 0)
  h = fmix(h ^ (y | 0))
  return fmix(h ^ (z | 0))
}

/** MurmurHash3's finalizer. */
function fmix(h: number): number {
  h ^= h >>> 16
  h = Math.imul(h, 0x85ebca6b)
  h ^= h >>> 13
  h = Math.imul(h, 0xc2b2ae35)
  return h ^ (h >>> 16)
}
