export type PageReadNotice =
  | { kind: 'loading'; message: string }
  | { kind: 'empty'; message: string }
  | { kind: 'error'; message: string; retryable: false }
  | { kind: 'error'; message: string; retryable: true; onRetry: () => void }
  | { kind: 'refresh-error'; message: string; onRetry: () => void }

export type PageReadState<T> =
  | { kind: 'unauthorized' }
  | { kind: 'loading'; message: string }
  | { kind: 'empty'; message: string }
  | { kind: 'error'; message: string; retryable: false }
  | { kind: 'error'; message: string; retryable: true; onRetry: () => void }
  | { kind: 'ready'; data: T; freshness: 'current' }
  | {
      kind: 'ready'
      data: T
      freshness: 'stale'
      refreshError: string
      onRetry: () => void
    }

export interface PageReadPresentation {
  redirectToLogin: boolean
  showReadyData: boolean
  notice: PageReadNotice | null
}

export function beginPageRead<T>(
  current: PageReadState<T>,
  message: string,
  preserveReadyData: boolean,
): PageReadState<T> {
  if (preserveReadyData && current.kind === 'ready') return current
  return { kind: 'loading', message }
}

export function completePageRead<T>(data: T, empty: boolean, emptyMessage: string): PageReadState<T> {
  if (empty) return { kind: 'empty', message: emptyMessage }
  return { kind: 'ready', data, freshness: 'current' }
}

export function failPageRead<T>(
  current: PageReadState<T>,
  message: string,
  onRetry: () => void,
): PageReadState<T> {
  if (current.kind === 'ready') {
    return {
      kind: 'ready',
      data: current.data,
      freshness: 'stale',
      refreshError: message,
      onRetry,
    }
  }
  return { kind: 'error', message, retryable: true, onRetry }
}

export function terminalPageReadError<T>(message: string): PageReadState<T> {
  return { kind: 'error', message, retryable: false }
}

export function readyPageData<T>(state: PageReadState<T>): T | null {
  return state.kind === 'ready' ? state.data : null
}

export function pageReadPresentation<T>(state: PageReadState<T>): PageReadPresentation {
  switch (state.kind) {
    case 'unauthorized':
      return { redirectToLogin: true, showReadyData: false, notice: null }
    case 'loading':
    case 'empty':
    case 'error':
      return { redirectToLogin: false, showReadyData: false, notice: state }
    case 'ready':
      if (state.freshness === 'current') {
        return { redirectToLogin: false, showReadyData: true, notice: null }
      }
      return {
        redirectToLogin: false,
        showReadyData: true,
        notice: {
          kind: 'refresh-error',
          message: state.refreshError,
          onRetry: state.onRetry,
        },
      }
  }
}
