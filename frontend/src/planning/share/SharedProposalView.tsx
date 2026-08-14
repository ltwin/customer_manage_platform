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
        <p className="share-locked">当前链接只开放创作概览与整案反馈。逐条镜头、分工认领需要摄影师签发完整档后才能查看。</p>
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
