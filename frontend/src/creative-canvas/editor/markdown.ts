// The subset of Markdown the assistant writes: paragraphs, headings, lists,
// fenced code, bold and inline code. It parses to a tree, never to HTML, and
// it is re-run on every streamed chunk: a marker that has not closed yet stays
// plain text until it does.

export type Inline = string | { bold: Inline[] } | { code: string }

export type MarkdownNode =
  | { type: 'paragraph'; content: Inline[] }
  | { type: 'heading'; level: number; content: Inline[] }
  | { type: 'list'; ordered: boolean; start: number; items: Inline[][] }
  | { type: 'code'; text: string }

export function parseInline(source: string): Inline[] {
  const out: Inline[] = []
  let plain = ''
  const flush = () => {
    if (plain) out.push(plain)
    plain = ''
  }
  let i = 0
  while (i < source.length) {
    if (source[i] === '`') {
      const end = source.indexOf('`', i + 1)
      if (end > i + 1) {
        flush()
        out.push({ code: source.slice(i + 1, end) })
        i = end + 1
        continue
      }
    }
    if (source.startsWith('**', i)) {
      const end = source.indexOf('**', i + 2)
      if (end > i + 2) {
        flush()
        out.push({ bold: parseInline(source.slice(i + 2, end)) })
        i = end + 2
        continue
      }
    }
    plain += source[i++]
  }
  flush()
  return out
}

export function parseMarkdown(source: string): MarkdownNode[] {
  const nodes: MarkdownNode[] = []
  let paragraph: string[] = []
  const flush = () => {
    if (paragraph.length)
      nodes.push({ type: 'paragraph', content: parseInline(paragraph.join('\n')) })
    paragraph = []
  }
  const lines = source.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (line.trimStart().startsWith('```')) {
      flush()
      const code: string[] = []
      // An unclosed fence is still code: the rest of the text is inside it.
      for (i++; i < lines.length && !lines[i].trimStart().startsWith('```'); i++)
        code.push(lines[i])
      nodes.push({ type: 'code', text: code.join('\n') })
      continue
    }
    const heading = /^(#{1,4})\s+(.+)$/.exec(line)
    if (heading) {
      flush()
      nodes.push({
        type: 'heading',
        level: heading[1].length,
        content: parseInline(heading[2]),
      })
      continue
    }
    const item = /^\s*(?:(\d+)\.|[-*])\s+(.*)$/.exec(line)
    if (item) {
      flush()
      const ordered = item[1] !== undefined
      const last = nodes.at(-1)
      if (last?.type === 'list' && last.ordered === ordered)
        last.items.push(parseInline(item[2]))
      else
        nodes.push({
          type: 'list',
          ordered,
          start: ordered ? Number(item[1]) : 1,
          items: [parseInline(item[2])],
        })
      continue
    }
    if (!line.trim()) {
      flush()
      continue
    }
    paragraph.push(line)
  }
  flush()
  return nodes
}
