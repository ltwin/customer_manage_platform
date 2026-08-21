// 状态流转被拒时的引导派生：服务端只回 readiness_incomplete / shots_incomplete
// 错误码，具体差什么由客户端从刷新后的策划数据推导（与服务端判定口径一致：
// 必需项 = requirement=required 且 preflight_status=unchecked；未完成镜头 =
// current_outcome 为空，cleared 结果会把投影指回空）。

export type TransitionGuidance =
  | { kind: 'readiness'; missingTitles: string[] }
  | { kind: 'shots'; total: number; remaining: number }
  | { kind: 'retry' }

export function isTransitionGuidanceCode(code: string): boolean {
  return code === 'readiness_incomplete' || code === 'shots_incomplete'
}

export function deriveTransitionGuidance(
  code: string,
  plan: {
    readiness_items: Array<{ requirement: string; preflight_status: string; title: string }>
    shots: Array<{ current_outcome: unknown }>
  },
): TransitionGuidance | null {
  if (code === 'readiness_incomplete') {
    const missingTitles = plan.readiness_items
      .filter((item) => item.requirement === 'required' && item.preflight_status === 'unchecked')
      .map((item) => item.title)
    return missingTitles.length > 0 ? { kind: 'readiness', missingTitles } : { kind: 'retry' }
  }
  if (code === 'shots_incomplete') {
    if (plan.shots.length === 0) return { kind: 'shots', total: 0, remaining: 0 }
    const remaining = plan.shots.filter((shot) => shot.current_outcome == null).length
    return remaining > 0 ? { kind: 'shots', total: plan.shots.length, remaining } : { kind: 'retry' }
  }
  return null
}
