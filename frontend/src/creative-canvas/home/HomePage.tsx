import { ChevronRight } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import * as api from '../editor/api.ts'
import { errorMessage } from '../editor/queue.ts'
import '../editor/workspace.css'
import AppSidebar from './AppSidebar.tsx'
import {
  canvasPath,
  emptyProjectPage,
  projectState,
  readRecentProjects,
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
  const [page, setPage] = useState<api.ProjectPage>(emptyProjectPage)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)

  useEffect(() => {
    const previous = document.title
    document.title = '创意空间'
    return () => {
      document.title = previous
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    readRecentProjects(controller.signal)
      .then((result) => {
        setPage(result)
        setError('')
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [tick])

  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === 'visible') setTick((value) => value + 1)
    }
    document.addEventListener('visibilitychange', visible)
    return () => document.removeEventListener('visibilitychange', visible)
  }, [])

  const continuing = page.items.slice(0, 4)
  return (
    <div className="cc-workspace cs-app">
      <AppSidebar current="home" recent={page.items} />
      <main className="cs-main">
        <div className="hp-view">
          {/* 一句话开始（V6-062）与封面缩影（V6-063）在后续提交里补上 */}
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
                <button
                  className="btn"
                  type="button"
                  onClick={() => setTick((value) => value + 1)}
                >
                  重试
                </button>
              </div>
            ) : loading ? (
              <div className="hp-state" role="status">
                正在加载项目…
              </div>
            ) : continuing.length ? (
              <div className="hp-grid">
                {continuing.map((project) => {
                  const state = projectState(project)
                  return (
                    <Link
                      key={project.id}
                      className="hp-card"
                      to={canvasPath(project)}
                    >
                      <span className="hp-cover">
                        {state && (
                          <span className={`hp-badge ${state}`}>
                            {state === 'wait' ? '等你确认' : '运行中'}
                          </span>
                        )}
                      </span>
                      <strong>{project.name}</strong>
                    </Link>
                  )
                })}
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
