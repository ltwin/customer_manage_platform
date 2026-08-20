import type { AppendShotResultResponse, RunInputSnapshot, ShootPlanSkipReason } from './api'

export function normalizeRunNotes(value: string): string | undefined {
  const trimmed = value.trim()
  return trimmed === '' ? undefined : trimmed
}

export function validateSkipSubmission(reason: ShootPlanSkipReason | '', note: string): string | null {
  if (reason === '') return '请先选择跳过原因，再确认跳过。'
  if (reason === 'other' && note.trim() === '') return '选了「其他」时，需要写明补充说明。'
  return null
}

export function applySavedShotResult(
  input: RunInputSnapshot,
  shotID: string,
  response: AppendShotResultResponse,
): RunInputSnapshot {
  return {
    ...input,
    execution_fact_revision: response.execution_fact_revision,
    shots: input.shots.map((shot) => shot.id === shotID
      ? {
          ...shot,
          execution_revision: response.execution_revision,
          current_outcome: response.current_outcome ?? null,
        }
      : shot),
  }
}

export function nextPendingShotIndex(input: RunInputSnapshot, currentIndex: number): number {
  if (input.shots.length === 0) return 0
  for (let offset = 1; offset <= input.shots.length; offset += 1) {
    const index = (currentIndex + offset) % input.shots.length
    if (!input.shots[index].current_outcome) return index
  }
  return Math.min(currentIndex, input.shots.length - 1)
}

export function completedShotCount(input: RunInputSnapshot): number {
  return input.shots.filter((shot) => shot.current_outcome?.result === 'captured' || shot.current_outcome?.result === 'skipped').length
}
