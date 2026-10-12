import {
  createContext,
  memo,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ComponentProps,
} from 'react'
import {
  Block,
  defaultComponents,
  Streamdown,
  type BlockProps,
  type Components,
  type PluginConfig,
} from 'streamdown'
import remend from 'remend'
import { closedCodeFence, markdownFeatures } from './markdown.ts'
import type { AgentMarkdownProps } from './AgentMarkdown.tsx'
import { MarkdownImage, MarkdownLink } from './AgentMarkdownMedia.tsx'

import Diagram from './MermaidDiagram.tsx'
const BlockSource = createContext('')
const Streaming = createContext(false)

const SourceBlock = memo(function SourceBlock(props: BlockProps) {
  const hasCode = useMemo(
    () => markdownFeatures(props.content).hasCodeBlock,
    [props.content],
  )
  const streaming = useContext(Streaming)
  return (
    <BlockSource.Provider value={props.content}>
      {/* Never repair an earlier emphasis marker into a code fence's body. */}
      <Block
        {...props}
        content={streaming && !hasCode ? remend(props.content) : props.content}
      />
    </BlockSource.Provider>
  )
})

function MarkdownCode(props: ComponentProps<typeof defaultComponents.code>) {
  const raw = useContext(BlockSource)
  const streaming = useContext(Streaming)
  if (props.className?.split(/\s/).includes('language-mermaid')) {
    const source = String(props.children ?? '').replace(/\n$/, '')
    const start = props.node?.position?.start.offset
    const closed = start !== undefined && closedCodeFence(raw, start)
    const fallback = (
      <div className="cc-md-fallback" data-diagram-pending>
        <small>
          {closed
            ? '图表加载中…'
            : streaming
              ? '图表正在接收…'
              : '图表未完整，保留源码'}
        </small>
        <pre>{source}</pre>
      </div>
    )
    if (!closed) return fallback
    return <Diagram source={source} />
  }
  return <defaultComponents.code {...props} />
}

const components: Components = {
  code: MarkdownCode,
  strong: ({ children }) => <strong>{children}</strong>,
  a: ({ href, children }) => <MarkdownLink href={href}>{children}</MarkdownLink>,
  img: ({ src, alt, title }) => (
    <MarkdownImage
      src={typeof src === 'string' ? src : undefined}
      alt={alt}
      title={title}
    />
  ),
}
const translations = { copyCode: '复制代码', copied: '已复制' }
const controls = { code: { copy: true, download: false }, table: false }
// Explicit loaders retain each dynamic import's CSS preload dependencies.
const loaders = {
  math: () => import('./markdownMath.ts').then((module) => module.default),
  code: () => import('./markdownCode.ts').then((module) => module.default),
}

export default function AgentMarkdownRenderer({
  source,
  streaming = false,
}: AgentMarkdownProps) {
  const needs = useMemo(() => markdownFeatures(source), [source])
  const [plugins, setPlugins] = useState<PluginConfig>({})
  const [failed, setFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let stale = false
    setFailed(false)
    if (needs.math)
      void loaders
        .math()
        .then((math) => {
          if (!stale)
            setPlugins((current) =>
              current.math === math ? current : { ...current, math },
            )
        })
        .catch(() => {
          if (!stale) setFailed(true)
        })
    if (needs.code)
      void loaders
        .code()
        .then((code) => {
          if (!stale)
            setPlugins((current) =>
              current.code === code ? current : { ...current, code },
            )
        })
        .catch(() => {
          if (!stale) setFailed(true)
        })
    return () => {
      stale = true
    }
  }, [needs.math, needs.code, attempt])

  if (failed)
    return (
      <div className="cc-md-fallback">
        <small>增强格式加载失败，保留原文</small>
        <button type="button" onClick={() => setAttempt((value) => value + 1)}>
          重试
        </button>
        <pre>{source}</pre>
      </div>
    )
  return (
    <Streaming.Provider value={streaming}>
      <Streamdown
        className="cc-agent-text"
        plugins={plugins}
        components={components}
        BlockComponent={SourceBlock}
        controls={controls}
        translations={translations}
        lineNumbers={false}
        mode="streaming"
        parseIncompleteMarkdown={false}
        isAnimating={streaming}
        skipHtml
      >
        {source}
      </Streamdown>
    </Streaming.Provider>
  )
}
