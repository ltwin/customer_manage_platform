import { useOutletContext } from 'react-router-dom'

export interface ShellContext {
	timezone: string | null
	timezoneError: string | null
	timezoneLoading: boolean
	retryTimezone(): void
	notify(message: string): void
}

export function useShell() {
  return useOutletContext<ShellContext>()
}
