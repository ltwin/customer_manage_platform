type ActionLocation = Pick<Location, 'hash' | 'pathname' | 'search'>
type ActionHistory = Pick<History, 'state' | 'replaceState'>

export function consumeActionToken(
  location: ActionLocation = window.location,
  history: ActionHistory = window.history,
): string | null {
  const fragment = new URLSearchParams(location.hash.replace(/^#/, ''))
  const token = fragment.get('token')
  history.replaceState(history.state, '', `${location.pathname}${location.search}`)
  return token && token.trim() !== '' ? token : null
}
