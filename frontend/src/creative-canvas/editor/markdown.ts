import { marked } from 'marked'

// Inspect syntax for optional downloads; Marked never emits HTML here.
export function markdownFeatures(source: string) {
  let code = false
  let hasCodeBlock = false
  let math = false
  marked.walkTokens(marked.lexer(source), (token) => {
    if (token.type === 'code') {
      hasCodeBlock = true
      code ||= !!token.lang && token.lang.split(/\s/)[0] !== 'mermaid'
    } else if (token.type === 'text') {
      math ||= token.raw.includes('$$')
    }
  })
  return { code, math, hasCodeBlock }
}

// Marked removes list/blockquote containers from code tokens. Count code nodes
// before the HAST offset to distinguish identical diagrams in one parent block.
function codeBlocks(source: string): string[] {
  const blocks: string[] = []
  marked.walkTokens(marked.lexer(source), (token) => {
    if (token.type === 'code') blocks.push(token.raw)
  })
  return blocks
}

export function closedCodeFence(source: string, offset = 0): boolean {
  const index = offset ? codeBlocks(source.slice(0, offset)).length : 0
  const raw = codeBlocks(source)[index]
  if (!raw) return false
  const lines = raw.replaceAll('\r\n', '\n').trimEnd().split('\n')
  if (lines.length < 2) return false
  const fence = /^ {0,3}(`{3,}|~{3,})/.exec(lines[0])?.[1]
  if (!fence) return false
  return new RegExp(`^ {0,3}${fence[0]}{${fence.length},}[ \t]*$`).test(
    lines.at(-1) ?? '',
  )
}
