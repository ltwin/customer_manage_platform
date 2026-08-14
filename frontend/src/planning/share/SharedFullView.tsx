import {
  SharedBriefSection,
  SharedFooter,
  SharedHero,
  SharedMoodboardSection,
  SharedScaleSection,
  PlanFeedbackForm,
} from './SharedCommon'
import { FullAssignmentsSection, FullShotsSection } from './SharedFullSections'
import type { SharedPlanFull } from './api'

export default function SharedFullView({
  token,
  plan,
  onReload,
}: {
  token: string
  plan: SharedPlanFull
  onReload: () => void
}) {
  return (
    <main className="share-page">
      <SharedHero plan={plan} viewLabel="完整方案" />
      <SharedBriefSection brief={plan.creative_brief} />
      <SharedMoodboardSection token={token} items={plan.moodboard} />
      <SharedScaleSection scale={plan.public_scale} window={plan.public_window} />
      <FullShotsSection token={token} shots={plan.shots} onConflict={onReload} />
      <FullAssignmentsSection token={token} opportunities={plan.assignment_opportunities} onReload={onReload} />
      <PlanFeedbackForm
        token={token}
        projectionRevision={plan.projection_revision}
        onConflict={onReload}
      />
      <SharedFooter />
    </main>
  )
}
