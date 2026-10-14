import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  digestBatch,
  fileDigest,
  findDuplicates,
} from '../src/creative-canvas/assets/importDuplicates.ts'
import type { components } from '../src/api/schema'

type Asset = components['schemas']['CreativeLibraryAsset']
const asset = (id: string) => ({ id }) as Asset

test('fileDigest is the lowercase hex SHA-256 the server stores', async () => {
  assert.equal(
    await fileDigest(new File(['abc'], 'a.txt')),
    'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
  )
})

test('duplicates map back to every file with library content, in order', async () => {
  const files = ['a', 'b', 'a', 'c'].map((x, i) => new File([x], `${i}.png`))
  const asked: string[][] = []
  const found = await findDuplicates(
    files,
    async (digests) => {
      asked.push(digests)
      return digests
        .filter((d) => d === 'a' || d === 'c')
        .map((d) => ({ digest: d, asset: asset(`asset-${d}`) }))
    },
    async (file) => file.text(),
  )
  assert.deepEqual(asked, [['a', 'b', 'c']], 'each digest is asked once')
  assert.deepEqual(
    found.map((d) => [d.file.name, d.asset.id]),
    [
      ['0.png', 'asset-a'],
      ['2.png', 'asset-a'],
      ['3.png', 'asset-c'],
    ],
  )
})

test('lookups stay within the server limit per request', async () => {
  const files = Array.from(
    { length: digestBatch * 2 + 1 },
    (_, i) => new File([String(i)], `${i}.png`),
  )
  const sizes: number[] = []
  const found = await findDuplicates(
    files,
    async (digests) => {
      sizes.push(digests.length)
      return []
    },
    async (file) => file.text(),
  )
  assert.deepEqual(sizes, [digestBatch, digestBatch, 1])
  assert.deepEqual(found, [])
})
