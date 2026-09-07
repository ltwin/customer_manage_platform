import { useEffect, useState } from 'react'
import { pilotState } from './api'
export function useLegacyReadOnly() {
 const [state, setState] = useState<'loading' | 'write' | 'readonly' | 'error'>('loading')
 useEffect(() => {
  const controller = new AbortController()
  void pilotState(controller.signal).then(p => { if (!controller.signal.aborted) setState(p.state === 'legacy_write' ? 'write' : 'readonly') }).catch(() => { if (!controller.signal.aborted) setState('error') })
  return () => controller.abort()
 }, [])
 return state
}
