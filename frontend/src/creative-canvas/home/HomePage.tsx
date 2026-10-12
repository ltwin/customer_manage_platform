import { ChevronRight, Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import { useOptionalAccountCenter } from '../../account/AccountCenterContext.tsx'
import * as api from '../editor/api.ts'
import '../editor/workspace.css'
import AppSidebar from './AppSidebar.tsx'
import HomeComposer, { type ComposerControls } from './HomeComposer.tsx'
import ProjectCover, { CoverMedia } from './ProjectCover.tsx'
import {
  canvasPath,
  greeting,
  projectState,
  recentPath,
  useProjectPage,
} from './projectList.ts'
import './home.css'

export default function HomePage() {
  const [params] = useSearchParams()
  const account = api.currentAccount()
  // 旧的已归档链接指向首页，现在归档列表在项目页。
  if (params.get('view') === 'archived')
    return <Navigate to="/creative/projects?view=archived" replace />
  return account ? (
    <Home key={account} account={account} />
  ) : (
    <main>请先登录</main>
  )
}

function Home({ account }: { account: string }) {
  // 侧栏「最近打开」与「继续创作」共用这一次读取：前 5 个给侧栏，前 4 个做卡片。
  const { page, error, reload } = useProjectPage(recentPath)
  const center = useOptionalAccountCenter()
  const profile = center?.profile.kind === 'ready' ? center.profile.data : null
  const name = profile?.display_name?.trim() ?? ''
  const composer = useRef<ComposerControls>(null)

  useEffect(() => {
    const previous = document.title
    document.title = '创意空间'
    return () => {
      document.title = previous
    }
  }, [])

  const continuing = page?.items.slice(0, 4) ?? []
  return (
    <div className="cc-workspace cs-app">
      <AppSidebar current="home" recent={page?.items ?? []} />
      <main className="cs-main">
        <div className="hp-view">
          <section className="hp-hero">
            <p className="hp-hello">
              {greeting(
                new Date().getHours(),
                name,
                page?.activity_counts ?? { running: 0, waiting: 0 },
              )}
            </p>
            <h1>这次想拍点什么？</h1>
            <HomeComposer account={account} controls={composer} />
          </section>
          <section className="hp-block" aria-labelledby="homeContinueTitle">
            <div className="hp-block-head">
              <h2 id="homeContinueTitle">继续创作</h2>
              <Link className="hp-more" to="/creative/projects">
                全部项目
                <ChevronRight size={14} />
              </Link>
            </div>
            {error ? (
              <div className="hp-state" role="alert">
                <p>{error}</p>
                <button className="btn" type="button" onClick={reload}>
                  重试
                </button>
              </div>
            ) : !page ? (
              <div className="hp-state" role="status">
                正在加载项目…
              </div>
            ) : continuing.length ? (
              <div className="hp-grid">
                {continuing.map((project) => (
                  <Link
                    key={project.id}
                    className="hp-card"
                    to={canvasPath(project)}
                    aria-label={`打开项目：${project.name}`}
                  >
                    <ProjectCover
                      project={project}
                      state={projectState(project)}
                    />
                    <strong>{project.name}</strong>
                  </Link>
                ))}
              </div>
            ) : (
              <div className="hp-state">
                <p>还没有项目</p>
                <Link className="btn" to="/creative/projects">
                  去项目页新建
                </Link>
              </div>
            )}
          </section>
          <RecentAssets
            onAdd={(id, title) => composer.current?.addReference(id, title)}
          />
        </div>
      </main>
    </div>
  )
}

// 资产库最近添加的图片：等高一行、保持原比例；资产库为空时整行不出现。
function RecentAssets({
  onAdd,
}: {
  onAdd: (revisionID: string, title: string) => void
}) {
  const [items, setItems] = useState<api.Asset[]>([])
  useEffect(() => {
    const abort = new AbortController()
    void api
      .read<api.AssetPage>(
        '/assets?sort=recent&kind=image&limit=12',
        abort.signal,
      )
      .then((page) => {
        if (!abort.signal.aborted)
          setItems(page.items.filter((item) => !item.unavailable))
      })
      .catch(() => undefined)
    return () => abort.abort()
  }, [])
  if (!items.length) return null
  return (
    <section className="hp-block" aria-labelledby="homeAssetsTitle">
      <div className="hp-block-head">
        <h2 id="homeAssetsTitle">资产库 · 最近添加</h2>
        <Link className="hp-more" to="/creative/library">
          打开资产库
          <ChevronRight size={14} />
        </Link>
      </div>
      <ul className="hp-assets">
        {items.map((item) => (
          <li key={item.id} className="hp-asset" title={item.title}>
            <CoverMedia revision={item.content_revision_id} />
            <button
              type="button"
              aria-label={`加为参考：${item.title}`}
              onClick={() => onAdd(item.content_revision_id, item.title)}
            >
              <Plus size={13} />
              参考
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}
