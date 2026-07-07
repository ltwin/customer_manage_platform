import { useOutletContext } from 'react-router-dom'

export interface ShellContext {
  notify(message: string): void
}

export function useShell() {
  return useOutletContext<ShellContext>()
}
