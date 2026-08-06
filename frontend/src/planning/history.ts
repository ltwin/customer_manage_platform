type PlanExecutionHistory = {
  execution_history?: ReadonlyArray<{ shot_id: string }>
}

export function shotHasExecutionHistory(plan: PlanExecutionHistory, shotID: string): boolean {
  return plan.execution_history?.some((fact) => fact.shot_id === shotID) ?? false
}
