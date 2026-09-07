import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useLegacyReadOnly } from './useLegacyReadOnly'
export function LegacyWriteSurface({ children }: { children: ReactNode }) {
 const { id = '' } = useParams(); const state = useLegacyReadOnly()
 if (state === 'write') return children
 return <main className="content"><h1>旧策划记录</h1><p role="status">{state === 'loading' ? '正在读取账号状态…' : state === 'error' ? '暂时无法确认编辑权限，请刷新后重试。' : '旧记录已只读，素材与执行历史仍保留。'}</p><Link to={`/shoot-plans/${id}`}>查看旧策划记录</Link> · <Link to="/creative-workspaces">前往创意空间</Link></main>
}
