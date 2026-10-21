// The models this browser last put into a scene, newest first, per account.
// Only ids are kept: the asset panel reads each one back, so a renamed,
// trashed or purged asset shows as it is now, or not at all.

const maxRecent = 8

const recentKey = (account: string | null) => `creative-scene-assets:recent:${account ?? 'anonymous'}`

export function readRecent(storage: Storage | undefined, account: string | null): string[] {
  try {
    const raw: unknown = JSON.parse(storage?.getItem(recentKey(account)) ?? 'null')
    return Array.isArray(raw) ? raw.filter((id): id is string => typeof id === 'string').slice(0, maxRecent) : []
  } catch {
    // A broken list starts empty; it is only a convenience.
    return []
  }
}

/** Moves the asset to the front and writes the list back; returns it. */
export function pushRecent(storage: Storage | undefined, account: string | null, assetID: string): string[] {
  const next = [assetID, ...readRecent(storage, account).filter((id) => id !== assetID)].slice(0, maxRecent)
  try {
    storage?.setItem(recentKey(account), JSON.stringify(next))
  } catch {
    // Storage full or blocked: the list lives for this session only.
  }
  return next
}
