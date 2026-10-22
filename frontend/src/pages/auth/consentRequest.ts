// GET /oauth/authorize 签出的请求包：v1.<payload>.<签名>。payload 不保密，这里只解码来显示。
export type ConsentRequest = {
  client_id: string
  client_name: string
  registration: 'cimd' | 'dcr'
  redirect_uri: string
  display_name: string
  exp: number
}

export function decodeRequest(raw: string | null): ConsentRequest | null {
  const part = raw?.split('.')[1]
  if (!part) return null
  try {
    const json = atob(part.replace(/-/g, '+').replace(/_/g, '/'))
    const bytes = Uint8Array.from(json, (c) => c.charCodeAt(0))
    const value = JSON.parse(new TextDecoder().decode(bytes)) as Partial<ConsentRequest>
    if (typeof value.client_name !== 'string' || typeof value.redirect_uri !== 'string' || typeof value.exp !== 'number') return null
    return {
      client_id: typeof value.client_id === 'string' ? value.client_id : '',
      client_name: value.client_name,
      registration: value.registration === 'cimd' ? 'cimd' : 'dcr',
      redirect_uri: value.redirect_uri,
      display_name: typeof value.display_name === 'string' ? value.display_name : value.client_name,
      exp: value.exp,
    }
  } catch {
    return null
  }
}

export function callbackHost(uri: string): { host: string; loopback: boolean } {
  try {
    const u = new URL(uri)
    const loopback = u.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(u.hostname)
    return { host: u.host, loopback }
  } catch {
    return { host: uri, loopback: false }
  }
}
