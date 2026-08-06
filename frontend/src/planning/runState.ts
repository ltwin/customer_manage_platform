import type { AppendShotResultResponse, RunInputSnapshot } from './api'

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
