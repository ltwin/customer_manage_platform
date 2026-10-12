import { Component, lazy, memo, Suspense, type ReactNode } from 'react'
import './agentMarkdown.css'

const Renderer = lazy(() => import('./AgentMarkdownRenderer.tsx'))
export interface AgentMarkdownProps {
  source: string
  streaming?: boolean
}

class MarkdownBoundary extends Component<
  { source: string; children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    return this.state.failed ? (
      <div className="cc-md-fallback">
        <small>格式暂时无法显示，以下是原文</small>
        <pre>{this.props.source}</pre>
      </div>
    ) : (
      this.props.children
    )
  }
}

export const AgentMarkdown = memo(function AgentMarkdown({
  source,
  streaming = false,
}: AgentMarkdownProps) {
  return (
    <div className="cc-agent-markdown">
      <MarkdownBoundary source={source}>
        <Suspense fallback={<pre className="cc-md-loading">{source}</pre>}>
          <Renderer source={source} streaming={streaming} />
        </Suspense>
      </MarkdownBoundary>
      {streaming && <span className="cc-agent-caret" aria-hidden />}
    </div>
  )
})
