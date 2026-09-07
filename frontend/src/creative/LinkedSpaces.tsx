import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { linkedSpaces, displayName, type Workspace } from './api'

export default function LinkedSpaces({
  kind,
  id,
}: {
  kind: 'order' | 'customer'
  id: string
}) {
  const [spaces, setSpaces] = useState<Workspace[]>([])
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    void linkedSpaces(kind, id, controller.signal)
      .then((items) => {
        if (!controller.signal.aborted) {
          setSpaces(items)
          setFailed(false)
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true)
      })
    return () => controller.abort()
  }, [kind, id])
  if (failed) return <small>创意空间关联暂未加载</small>
  if (!spaces.length) return null
  return (
    <div aria-label="关联的创意空间">
      {spaces.map((space) => (
        <Link
          key={space.id}
          to={`/creative-workspaces/${space.id}`}
          className="planning-summary-link"
        >
          创意空间 · {displayName(space)}
          {space.archived ? '（已归档）' : ''} ↗
        </Link>
      ))}
    </div>
  )
}
