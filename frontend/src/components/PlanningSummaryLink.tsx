import { Link } from 'react-router-dom'
import type { components } from '../api/schema'

type PlanningSummary = components['schemas']['PlanningSummary']

export default function PlanningSummaryLink({ summary }: { summary?: PlanningSummary }) {
  if (!summary || summary.plan_count < 1 || !summary.primary_plan) return null
  const primary = summary.primary_plan
  return (
    <p className="planning-summary-link">
      策划 {summary.plan_count} 份
      {' · '}
      <Link to={`/shoot-plans/${primary.id}`}>{primary.title}</Link>
    </p>
  )
}
