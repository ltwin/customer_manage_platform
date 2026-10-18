import { Component, useEffect, type ReactNode } from 'react'
import type { Scene } from '../api.ts'
import type { SceneModel } from '../models/cache.ts'
import { SceneObjects } from '../objects/SceneObjects.tsx'
import { Environment } from '../Viewport.tsx'

// What a screenshot draws: the scene as the viewport shows it, nothing
// selected, no light or camera icons. It reports its commit, so the one
// manual frame comes after every effect, or the error that stopped it.

const nothingSelected: ReadonlySet<string> = new Set()

type Props = { scene: Scene; models: Readonly<Record<string, SceneModel>>; onCommit(): void; onError(err: unknown): void }

export function ShotScene({ scene, models, onCommit, onError }: Props) {
  return (
    <Failed onError={onError}>
      <Environment environment={scene.environment} />
      <SceneObjects scene={scene} models={models} selected={nothingSelected} active={null} helpers={false} />
      <Committed onCommit={onCommit} />
    </Failed>
  )
}

/** A drawing error fails the shot instead of leaving it waiting for a commit that never comes. */
class Failed extends Component<{ onError(err: unknown): void; children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  componentDidCatch(err: unknown) {
    this.props.onError(err)
  }
  render() {
    return this.state.failed ? null : this.props.children
  }
}

function Committed({ onCommit }: { onCommit(): void }) {
  // Runs after the effects of the siblings before it (the sky's orientation).
  useEffect(() => onCommit(), [onCommit])
  return null
}
