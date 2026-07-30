const TOKEN_KEY = 'crm_token'

let generation = 0
const listeners = new Set<() => void>()

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
	localStorage.setItem(TOKEN_KEY, token)
	bumpGeneration()
}

export function clearToken(): void {
	localStorage.removeItem(TOKEN_KEY)
	bumpGeneration()
}

export function getAuthGeneration(): number {
	return generation
}

export function subscribeToken(listener: () => void): () => void {
	listeners.add(listener)
	return () => listeners.delete(listener)
}

export function getTokenSnapshot(): string {
	return `${generation}:${getToken() ?? ''}`
}

function bumpGeneration(): void {
	generation += 1
	listeners.forEach((listener) => listener())
}
