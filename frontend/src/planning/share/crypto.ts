const SHARE_SECRET_BYTES = 32
const RECEIPT_SECRET_BYTES = 32

export function isWebCryptoAvailable(): boolean {
  return typeof globalThis.crypto?.subtle?.digest === 'function'
    && typeof globalThis.crypto?.getRandomValues === 'function'
}

function toBase64URL(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/u, '')
}

async function sha256Raw(bytes: Uint8Array): Promise<Uint8Array> {
  const copy = new Uint8Array(bytes.byteLength)
  copy.set(bytes)
  const digest = await crypto.subtle.digest('SHA-256', copy)
  return new Uint8Array(digest)
}

export async function generateShareSecretMaterial(): Promise<{ secret: Uint8Array; commitment: string }> {
  if (!isWebCryptoAvailable()) {
    throw new Error('当前浏览器不支持安全随机数，无法签发分享链接。')
  }
  const secret = new Uint8Array(SHARE_SECRET_BYTES)
  crypto.getRandomValues(secret)
  const commitment = toBase64URL(await sha256Raw(secret))
  return { secret, commitment }
}

export function composeShareTokenWire(selector: string, secret: Uint8Array): string {
  return `sp1.${selector}.${toBase64URL(secret)}`
}

export function composeShareURL(selector: string, secret: Uint8Array): string {
  return `${window.location.origin}/shared/plans/${composeShareTokenWire(selector, secret)}`
}

export async function generateClaimReceiptMaterial(): Promise<{ receiptWire: string; commitment: string }> {
  if (!isWebCryptoAvailable()) {
    throw new Error('当前浏览器不支持安全随机数，无法认领。')
  }
  const secret = new Uint8Array(RECEIPT_SECRET_BYTES)
  crypto.getRandomValues(secret)
  const commitment = toBase64URL(await sha256Raw(secret))
  return { receiptWire: `cr1.${toBase64URL(secret)}`, commitment }
}

const CLAIM_RECEIPT_WIRE_PATTERN = /^cr1\.[A-Za-z0-9_-]{20,180}$/

// 仅做输入形态预检，减少无谓请求；真伪由服务端 constant-time 比对裁决。
export function isClaimReceiptWireFormat(value: string): boolean {
  return CLAIM_RECEIPT_WIRE_PATTERN.test(value.trim())
}
