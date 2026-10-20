import { createStore, type StoreApi } from 'zustand/vanilla'
import type { components } from '../api/schema'
import type { Upload } from '../creative-canvas/editor/media.ts'
import type { Scene, SceneObject } from './api.ts'
import { nextOrder } from './editing.ts'
import { bake, type BakeDeps, type Compiled } from './host/bake.ts'
import { newIntentID, newObjectID } from './ids.ts'
import { modelSummaries, type ModelCache, type SceneModel } from './models/cache.ts'
import type { CompileInput } from './sandbox/host.ts'
import type { CompileError } from './sandbox/protocol.ts'
import { contentUnit, localContent, localRevision, type WireOp } from './state/apply.ts'
import type { Intent } from './state/intents.ts'
import type { SceneSession } from './state/sync.ts'

// One model edit, from a parameter change or 「生成」 to a queued intent:
// freeze the baseline, compile in the sandbox, check and thumbnail the GLB,
// upload both as staged files, then one command (R5). Nothing reaches the
// queue before every step has worked; from there saving, conflicts and undo
// are the session's.

type Generator = components['schemas']['CreativeModelGeneratorInput']

export type ModelEditDeps = BakeDeps & {
  session: SceneSession
  cache: ModelCache
  /** Releases a ready staged upload that will never be committed. */
  discard(upload: Upload): Promise<void>
  makeObjectID?: () => string
}

export type EditStage = 'compile' | 'thumbnail' | 'upload' | 'commit'
export type EditStatus =
  | { state: 'baking' | 'uploading' }
  | { state: 'queued'; intent: string }
  | { state: 'failed'; stage: EditStage; message: string; error?: CompileError }

/** The model being created, drawn translucent at its drop point until it is queued. */
export type Ghost = { revision: string; position: [number, number, number] }

export type ModelEditsState = {
  /** By target: an object ID, or 'new' for a model being created. */
  status: Record<string, EditStatus>
  /** Baked but not queued yet: the revision key each object shows meanwhile. */
  previews: Record<string, string>
  ghost: Ghost | null
}

export type EditOutcome =
  | { ok: true; intent: Intent }
  | { ok: false; superseded: true }
  | { ok: false; superseded?: false; stage: EditStage; message: string; error?: CompileError }

/** R4: what an edit of one instance does to the other instances of its model. */
export type Fork = { baseContentID?: string; scope: 'this' | 'all_instances'; objectIDs: string[] }

/**
 * The scene as the viewport shows it while bakes wait to be queued: those
 * instances draw the fresh GLB. Nothing else (outliner, saving) sees this.
 */
export function withPreviews(scene: Scene, previews: Readonly<Record<string, string>>): Scene {
  if (!Object.keys(previews).length) return scene
  return {
    ...scene,
    objects: scene.objects.map((o) => {
      const shown = previews[o.id]
      return shown && o.model ? { ...o, model: { content_revision_id: shown } } : o
    }),
  }
}

/** Other instances in the scene showing the same model revision as this one. */
export function sharingInstances(objects: readonly SceneObject[], objectID: string): string[] {
  const source = objects.find((o) => o.id === objectID)?.model?.content_revision_id
  if (!source) return []
  return objects.filter((o) => o.id !== objectID && o.kind === 'model_instance' && o.model?.content_revision_id === source).map((o) => o.id)
}

/**
 * R4. Unshared: a new revision of the same model. Shared, this one only
 * (the default): a new model, the others untouched. Shared, all of them: a
 * new revision of the same model, every listed instance switches.
 */
export function forkPlan(objects: readonly SceneObject[], objectID: string, contentID: string | undefined, applyToAll: boolean): Fork {
  const others = sharingInstances(objects, objectID)
  if (!others.length) return { baseContentID: contentID, scope: 'this', objectIDs: [objectID] }
  if (!applyToAll) return { scope: 'this', objectIDs: [objectID] }
  return { baseContentID: contentID, scope: 'all_instances', objectIDs: [objectID, ...others] }
}

const ref = 'model'
const maxNameHint = 80

type Plan = {
  key: string
  label: string
  nameHint: string
  units: string[]
  models: Record<string, string>
  compile: CompileInput
  /** Checked just before queueing; a message means the edit is dropped. */
  stillValid(objects: readonly SceneObject[]): string | null
  ops(commit: WireOp, objects: readonly SceneObject[]): WireOp[]
  /** The fork decided at the start (updates), for the summary's content ID. */
  baseContent(models: Record<string, string>): string | undefined
  preview: { objectIDs: string[] } | { ghost: [number, number, number] }
}

type Running = { edit: number; plan: Plan; abort: AbortController; uploads: Upload[] }

export class ModelEdits {
  readonly store: StoreApi<ModelEditsState> = createStore(() => ({ status: {}, previews: {}, ghost: null }))
  private readonly deps: ModelEditDeps
  private readonly running = new Map<string, Running>()
  private seq = 0

  constructor(deps: ModelEditDeps) {
    this.deps = deps
  }

  /** A new code model at a point on the ground, last among root objects. */
  create(input: { code: string; params?: Record<string, unknown>; position: [number, number, number]; name?: string }): Promise<EditOutcome> {
    const name = input.name?.trim() || '代码模型'
    const id = (this.deps.makeObjectID ?? newObjectID)()
    return this.run({
      key: 'new',
      label: '新建代码模型',
      nameHint: name,
      units: ['objects'],
      models: {},
      compile: { code: input.code, params: input.params ?? {}, libVersion: 1 },
      stillValid: () => null,
      ops: (commit, objects) => [
        commit,
        {
          op: 'add_objects',
          objects: [{
            id, kind: 'model_instance', name, parent_id: null, order: nextOrder(objects, null),
            transform: { position: input.position, rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
            visible: true, locked: false, model: { ref }, material_slots: {},
          }],
        },
      ],
      baseContent: () => undefined,
      preview: { ghost: input.position },
    })
  }

  /**
   * Rebakes a model instance with new code or parameters. The fork (R4) and
   * the expected revisions are decided now, not when saving.
   */
  update(input: { objectID: string; code: string; params: Record<string, unknown>; libVersion: number; applyToAll: boolean; label: string }): Promise<EditOutcome> {
    const view = this.deps.session.state.view
    const target = view?.scene.objects.find((o) => o.id === input.objectID)
    const source = target?.model?.content_revision_id
    if (!view || !target || !source) return Promise.resolve({ ok: false, stage: 'commit', message: '找不到要修改的模型' })
    const content = this.summaries()[source]?.content_id || undefined
    const fork = forkPlan(view.scene.objects, input.objectID, content, input.applyToAll)
    return this.run({
      key: input.objectID,
      label: input.label,
      nameHint: this.summaries()[source]?.name_hint || target.name || '代码模型',
      units: fork.objectIDs.map(contentUnit),
      models: fork.baseContentID ? { source, content: fork.baseContentID } : { source },
      compile: { code: input.code, params: input.params, libVersion: input.libVersion },
      stillValid: (objects) => {
        const present = new Set(objects.map((o) => o.id))
        return fork.objectIDs.every((id) => present.has(id)) ? null : '物件已被删除，修改未保存'
      },
      ops: (commit) => [
        commit,
        {
          op: 'set_instance_model',
          object_id: input.objectID,
          model: { ref },
          scope: fork.scope,
          ...(fork.scope === 'all_instances' ? { object_ids: fork.objectIDs } : {}),
        },
      ],
      // The source content may have been a placeholder; the baseline kept it current.
      baseContent: (models) => models.content,
      preview: { objectIDs: fork.objectIDs },
    })
  }

  /** Stops everything not queued yet; the editor closed. */
  dispose() {
    for (const key of [...this.running.keys()]) this.supersede(key)
  }

  private summaries(): Record<string, SceneModel> {
    return modelSummaries(this.deps.session.state.base?.models, this.deps.cache.local.getState(), this.deps.cache.known.getState())
  }

  private set(patch: (s: ModelEditsState) => Partial<ModelEditsState>) {
    this.store.setState(patch(this.store.getState()))
  }

  private setStatus(key: string, status: EditStatus | null) {
    this.set((s) => {
      const { [key]: _, ...rest } = s.status
      return { status: status ? { ...rest, [key]: status } : rest }
    })
  }

  /**
   * Shows a bake on its objects (or as the ghost), or stops showing it. A
   * superseded edit's preview stays until the newer one replaces it, so the
   * object does not flicker back; a key nobody shows any more is released.
   */
  private showPreview(plan: Plan, revision: string | null) {
    const before = this.store.getState()
    const replaced = new Set<string>()
    if ('ghost' in plan.preview) {
      if (before.ghost) replaced.add(before.ghost.revision)
      this.set(() => ({ ghost: revision ? { revision, position: (plan.preview as { ghost: [number, number, number] }).ghost } : null }))
    } else {
      const previews = { ...before.previews }
      for (const id of plan.preview.objectIDs) {
        if (previews[id]) replaced.add(previews[id])
        if (revision) previews[id] = revision
        else delete previews[id]
      }
      this.set(() => ({ previews }))
    }
    const after = this.store.getState()
    const shown = new Set([...Object.values(after.previews), ...(after.ghost ? [after.ghost.revision] : [])])
    for (const key of replaced) if (!shown.has(key)) this.deps.cache.unregister(key)
  }

  /**
   * A newer edit of the same target replaces this one: stop it and free its
   * uploads. Its preview stays on objects the newer edit will show again.
   */
  private supersede(key: string, next?: Plan) {
    const old = this.running.get(key)
    if (!old) return
    this.running.delete(key)
    old.abort.abort()
    for (const u of old.uploads) void this.deps.discard(u).catch(() => {})
    const kept = next && 'objectIDs' in next.preview ? new Set(next.preview.objectIDs) : new Set<string>()
    if ('objectIDs' in old.plan.preview) {
      const dropped = old.plan.preview.objectIDs.filter((id) => !kept.has(id))
      if (dropped.length) this.showPreview({ ...old.plan, preview: { objectIDs: dropped } }, null)
    } else if (!next || !('ghost' in next.preview)) this.showPreview(old.plan, null)
  }

  private async run(plan: Plan): Promise<EditOutcome> {
    const { session, cache } = this.deps
    this.supersede(plan.key, plan)
    const me: Running = { edit: ++this.seq, plan, abort: new AbortController(), uploads: [] }
    this.running.set(plan.key, me)
    const live = () => this.running.get(plan.key) === me
    const previewKey = `preview:${me.edit}`
    const baseline = session.baseline(plan.units, plan.models)
    const finish = (outcome: EditOutcome): EditOutcome => {
      if (baseline) session.dropBaseline(baseline.id)
      if (live()) {
        this.running.delete(plan.key)
        this.showPreview(plan, null)
        if (!outcome.ok && !outcome.superseded) this.setStatus(plan.key, { state: 'failed', stage: outcome.stage, message: outcome.message, error: outcome.error })
      }
      return outcome
    }
    const failed = (stage: EditStage, message: string, error?: CompileError): EditOutcome => {
      for (const u of me.uploads) void this.deps.discard(u).catch(() => {})
      return finish({ ok: false, stage, message, error })
    }
    const superseded = (): EditOutcome => {
      for (const u of me.uploads) void this.deps.discard(u).catch(() => {})
      return finish({ ok: false, superseded: true })
    }
    if (!baseline) return failed('commit', '场景还没有加载完')

    const nameHint = [...plan.nameHint].slice(0, maxNameHint).join('')
    const described = (compiled: Compiled) => {
      const { module, manifest, codeSHA256 } = compiled
      const generator: Generator = {
        language: 'three-module',
        contract_version: module.contract_version,
        lib_version: module.lib_version,
        code: plan.compile.code,
        code_sha256: codeSHA256,
        params_schema: module.params_schema,
        params: module.params,
        mount: module.mount,
      }
      const { code: _, ...shown } = generator
      const summary = (contentID: string): SceneModel => ({ content_id: contentID, usable: true, manifest, generator: shown, name_hint: nameHint })
      return { generator, summary }
    }

    this.setStatus(plan.key, { state: 'baking' })
    const out = await bake(this.deps, plan.compile, me.abort.signal, {
      live,
      compiled: (compiled) => {
        cache.register(previewKey, { glb: compiled.glb, summary: described(compiled).summary('') })
        this.showPreview(plan, previewKey)
      },
      uploading: () => this.setStatus(plan.key, { state: 'uploading' }),
    })
    me.uploads.push(...out.uploads)
    if (!out.ok) {
      if (out.stopped) return superseded()
      if (out.stage === 'compile') return failed('compile', out.error.message, out.error)
      return failed(out.stage, out.stage === 'thumbnail' ? '缩略图生成失败，模型没有保存' : '模型保存失败，可以重试')
    }
    const { glb, manifest, model, thumbnail } = out.baked
    const { generator, summary } = described(out.baked)

    const frozen = session.baselineOf(baseline.id)
    if (!frozen || frozen.broken) return failed('commit', '之前的修改没有保存，这次修改已放弃')
    const objects = session.state.view?.scene.objects ?? []
    const invalid = plan.stillValid(objects)
    if (invalid) return failed('commit', invalid)
    const intentID = newIntentID()
    const baseContent = plan.baseContent(frozen.models)
    const commit: WireOp = {
      op: 'commit_model', ref, glb_upload_id: model.id, thumbnail_upload_id: thumbnail.id, generator, manifest,
      name_hint: nameHint, ...(baseContent ? { base_content_id: baseContent } : {}),
    }
    // Registered before the view can name it.
    const placeholder = localRevision(intentID, ref)
    cache.register(placeholder, { glb, summary: summary(baseContent ?? localContent(intentID, ref)), generator })
    const intent = session.edit(plan.ops(commit, objects), plan.label, { baseline: baseline.id, id: intentID })
    if (!intent) {
      cache.unregister(placeholder)
      return failed('commit', '场景现在不能编辑，修改没有保存')
    }
    me.uploads = []
    this.setStatus(plan.key, { state: 'queued', intent: intent.id })
    return finish({ ok: true, intent })
  }
}
