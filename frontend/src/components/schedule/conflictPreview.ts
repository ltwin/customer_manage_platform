interface ConflictPreviewRequestVersion {
  key: string
  generation: number
}

export function conflictPreviewRequestKey(
  startAt: string,
  endAt: string,
  slotID?: string,
): string {
  return `${startAt}:${endAt}:${slotID ?? ''}`
}

export function isCurrentConflictPreviewRequest(
  current: ConflictPreviewRequestVersion,
  request: ConflictPreviewRequestVersion,
): boolean {
  return current.key === request.key && current.generation === request.generation
}
