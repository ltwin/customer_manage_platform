const alphabet =
  'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-'

/** A 21-character nanoid; random IDs keep deleted objects from being reused. */
export function newObjectID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(21))
  let id = 'sobj_'
  for (const b of bytes) id += alphabet[b & 63]
  return id
}

export function newIntentID(): string {
  return crypto.randomUUID()
}
