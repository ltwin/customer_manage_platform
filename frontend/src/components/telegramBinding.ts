export type TelegramWindowOpener = (
  url?: string | URL,
  target?: string,
  features?: string,
) => Window | null

export function openTelegramDeepLink(
  deepLink: string,
  opener: TelegramWindowOpener = window.open.bind(window),
): boolean {
  return opener(deepLink, '_blank', 'noopener,noreferrer') !== null
}
