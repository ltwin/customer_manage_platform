import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import * as api from '../editor/api.ts'
import '../editor/workspace.css'
import '../editor/library.css'
import AppSidebar from '../home/AppSidebar.tsx'
import AssetBrowser from './AssetBrowser.tsx'
import AssetInspector from './AssetInspector.tsx'
import LibraryTree from './LibraryTree.tsx'
import {
  enter,
  readPrefs,
  readQuery,
  writePrefs,
  writeQuery,
  type LibraryQuery,
} from './query.ts'
import { useAssetList, useCatalog } from './useLibrary.ts'
import './assets.css'

// 资产库独立页（V6-067）：应用侧栏 + 资料库树 + 浏览区 + 检查器，不加载画布。
export default function AssetsPage() {
  const account = api.currentAccount()
  return account ? <Assets key={account} /> : <main>请先登录</main>
}

function Assets() {
  const [params, setParams] = useSearchParams()
  const query = readQuery(params)
  const [refresh, setRefresh] = useState(0)
  const [prefs, setPrefs] = useState(readPrefs)
  const [selected, setSelected] = useState<string | null>(null)
  const { catalog, error: catalogError } = useCatalog(refresh)
  const list = useAssetList(query, refresh)

  useEffect(() => {
    const previous = document.title
    document.title = '资产库 · 创意空间'
    return () => {
      document.title = previous
    }
  }, [])
  // 画布抽屉和这一页不会同时出现，所以不做推送：切回标签页时重读。
  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === 'visible') setRefresh((n) => n + 1)
    }
    document.addEventListener('visibilitychange', visible)
    return () => document.removeEventListener('visibilitychange', visible)
  }, [])

  // 换视图、进分组记进历史，可以后退；筛选只替换当前记录。
  function go(next: LibraryQuery, push = false) {
    setParams(writeQuery(next), { replace: !push })
  }
  const asset = list.page?.items.find((a) => a.id === selected) ?? null
  return (
    <div className="cc-workspace cs-app">
      <AppSidebar current="library" />
      <main
        className={`cs-main al-page${prefs.inspector ? '' : ' no-inspector'}`}
      >
        <LibraryTree
          query={query}
          catalog={catalog}
          error={catalogError}
          facets={list.facets}
          onView={(view) => go(enter(query, view), true)}
          onGroup={(id) => go(enter(query, 'all', id), true)}
          onTag={(id) =>
            go({
              ...query,
              tags: query.tags.includes(id)
                ? query.tags.filter((t) => t !== id)
                : [...query.tags, id],
            })
          }
        />
        <AssetBrowser
          query={query}
          catalog={catalog}
          list={list}
          prefs={prefs}
          selected={selected}
          onQuery={(q) => go(q)}
          onView={(view) => go(enter(query, view), true)}
          onGroup={(id) => go(enter(query, 'all', id), true)}
          onPrefs={(p) => {
            setPrefs(p)
            writePrefs(p)
          }}
          onSelect={setSelected}
          onRetry={() => setRefresh((n) => n + 1)}
        />
        {prefs.inspector && <AssetInspector asset={asset} catalog={catalog} />}
      </main>
    </div>
  )
}
