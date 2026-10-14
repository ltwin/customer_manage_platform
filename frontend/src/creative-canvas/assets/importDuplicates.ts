import type { components } from '../../api/schema'

type Asset = components['schemas']['CreativeLibraryAsset']
type Match = components['schemas']['CreativeAssetDigestMatch']

export type Duplicate = { file: File; asset: Asset }

/** 服务端单次查重的摘要上限。 */
export const digestBatch = 50

export async function fileDigest(file: File): Promise<string> {
  const hash = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
  return Array.from(new Uint8Array(hash), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')
}

/**
 * 找出资产库（含回收站）里已有相同内容的文件。逐个算摘要，避免同时把整批
 * 文件读进内存；查询按上限分批。
 */
export async function findDuplicates(
  files: File[],
  lookup: (digests: string[]) => Promise<Match[]>,
  digest: (file: File) => Promise<string> = fileDigest,
): Promise<Duplicate[]> {
  const digests: string[] = []
  for (const file of files) digests.push(await digest(file))
  const unique = [...new Set(digests)]
  const found = new Map<string, Asset>()
  for (let i = 0; i < unique.length; i += digestBatch)
    for (const match of await lookup(unique.slice(i, i + digestBatch)))
      found.set(match.digest, match.asset)
  return files.flatMap((file, i) => {
    const asset = found.get(digests[i])
    return asset ? [{ file, asset }] : []
  })
}
