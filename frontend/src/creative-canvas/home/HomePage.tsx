import { ChevronRight } from 'lucide-react'
import { useEffect } from 'react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import { useOptionalAccountCenter } from '../../account/AccountCenterContext.tsx'
import * as api from '../editor/api.ts'
import '../editor/workspace.css'
import AppSidebar from './AppSidebar.tsx'
import ProjectCover from './ProjectCover.tsx'
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
  return account ? <Home key={account} /> : <main>请先登录</main>
}

function Home() {
  // 侧栏「最近打开」与「继续创作」共用这一次读取：前 5 个给侧栏，前 4 个做卡片。
  const { page, error, reload } = useProjectPage(recentPath)
  const center = useOptionalAccountCenter()
  const profile = center?.profile.kind === 'ready' ? center.profile.data : null
  const name = profile?.display_name?.trim() ?? ''

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
          {/* 一句话开始（V6-062）在后续提交里补上 */}
          <p className="hp-hello">
            {greeting(
              new Date().getHours(),
              name,
              page?.activity_counts ?? { running: 0, waiting: 0 },
            )}
          </p>
          <section className="hp-block" aria-labelledby="homeContinueTitle">
            <div className="hp-block-head">
              <h1 id="homeContinueTitle">继续创作</h1>
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
        </div>
      </main>
    </div>
  )
}
