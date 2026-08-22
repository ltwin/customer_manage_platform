import type { SharedPlanProposal } from './api'
import { SharedBriefSection, SharedFooter, SharedHero, SharedMoodboardSection, SharedScaleSection, PlanFeedbackForm } from './SharedCommon'

/** Proposal-only shell: must never import or render shot lists or claim opportunities. */
export default function SharedProposalView({
  token,
  plan,
  onProjectionConflict,
}: {
  token: string
  plan: SharedPlanProposal
  onProjectionConflict: () => void
}) {
  return (
    <main className="share-page">
      <SharedHero plan={plan} viewLabel="方案概览" />
      <SharedBriefSection brief={plan.creative_brief} />
      <SharedMoodboardSection token={token} items={plan.moodboard} />
      <SharedScaleSection scale={plan.public_scale} window={plan.public_window} />
      <section className="share-sec">
        <span className="share-eyebrow">锁定说明</span>
        <h2>逐镜与分工暂未开放</h2>
        <p className="share-locked">这一版是方案概览。等我们确认档期、拍摄单排好之后，我会给你一个完整版链接，里面有每个镜头的具体安排和准备分工。</p>
      </section>
      <PlanFeedbackForm
        token={token}
        projectionRevision={plan.projection_revision}
        onConflict={onProjectionConflict}
      />
      <SharedFooter />
    </main>
  )
}
