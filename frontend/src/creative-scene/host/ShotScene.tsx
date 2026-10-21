import { Component, useEffect, type ReactNode } from 'react'
import type { Scene } from '../api.ts'
import type { SceneModel } from '../models/cache.ts'
import { SceneObjects } from '../objects/SceneObjects.tsx'
import { Lighting } from '../objects/Lighting.tsx'
import { engineOf } from '../render/engines.ts'
import type { Look } from './render.ts'

// What a screenshot draws: the scene as the viewport shows it, nothing
// selected, no light or camera icons. It reports its commit, so the one
// manual frame comes after every effect, or the error that stopped it.

const nothingSelected: ReadonlySet<string> = new Set()

type Props = { scene: Scene; models: Readonly<Record<string, SceneModel>>; look: Look; onCommit(): void; onError(err: unknown): void }

export function ShotScene({ scene, models, look, onCommit, onError }: Props) {
  // The viewport's solid or rendered shading.
  const display = look === 'lit' ? 'rendered' : 'solid'
  return (
    <Failed onError={onError}>
      <Lighting environment={scene.environment} display={display} engine={engineOf(scene.render)} />
      <SceneObjects scene={scene} models={models} selected={nothingSelected} active={null} helpers={false} display={display} />
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
