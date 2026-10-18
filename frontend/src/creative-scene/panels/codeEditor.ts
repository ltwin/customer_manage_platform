import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { javascript } from '@codemirror/lang-javascript'
import { HighlightStyle, bracketMatching, indentOnInput, syntaxHighlighting } from '@codemirror/language'
import { EditorState, StateEffect, StateField } from '@codemirror/state'
import { Decoration, EditorView, drawSelection, highlightActiveLine, keymap, lineNumbers, type DecorationSet } from '@codemirror/view'
import { tags as t } from '@lezer/highlight'

// CodeMirror 6 for the code panel, in its own chunk: the editor loads it only
// when the panel opens. Its own history keeps Cmd+Z inside the text while
// the focus is there (the scene's keymap skips contenteditable targets).

export type CodeEditor = {
  /** Marks a line as the compile error's, or clears the mark. */
  setErrorLine(line: number | null): void
  setDoc(doc: string): void
  focus(): void
  destroy(): void
}

const setError = StateEffect.define<number | null>()
const errorLine = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(marks, tr) {
    let next = tr.docChanged ? Decoration.none : marks.map(tr.changes)
    for (const e of tr.effects) {
      if (!e.is(setError)) continue
      next = Decoration.none
      if (e.value !== null && e.value >= 1 && e.value <= tr.state.doc.lines) {
        const line = tr.state.doc.line(e.value)
        next = Decoration.set([Decoration.line({ class: 'cc-code-error-line' }).range(line.from)])
      }
    }
    return next
  },
  provide: (f) => EditorView.decorations.from(f),
})

// The creative workspace is always dark; the editor takes its surfaces.
const theme = EditorView.theme(
  {
    '&': { height: '100%', fontSize: '12.5px', color: 'var(--fg)', backgroundColor: 'var(--bg)' },
    '.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', lineHeight: '1.55' },
    '.cm-content': { caretColor: 'var(--fg)' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--fg)' },
    '.cm-gutters': { backgroundColor: 'var(--bg)', color: 'var(--fg-3, #6d6d73)', borderRight: '1px solid var(--line, rgba(255,255,255,0.07))' },
    '.cm-activeLine': { backgroundColor: 'rgba(255, 255, 255, 0.04)' },
    '.cm-activeLineGutter': { backgroundColor: 'rgba(255, 255, 255, 0.04)', color: 'var(--fg-2, #a1a1a7)' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'rgba(120, 150, 255, 0.28)' },
    '&.cm-focused .cm-matchingBracket': { backgroundColor: 'rgba(255, 255, 255, 0.12)', outline: 'none' },
    '.cc-code-error-line': { backgroundColor: 'rgba(239, 68, 68, 0.18)' },
  },
  { dark: true },
)

// Muted colours that read on the near-black workspace.
const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.modifier, t.operatorKeyword, t.controlKeyword, t.definitionKeyword, t.moduleKeyword], color: '#c792ea' },
  { tag: [t.string, t.special(t.string)], color: '#c3e88d' },
  { tag: [t.number, t.bool, t.null], color: '#f78c6c' },
  { tag: [t.comment, t.lineComment, t.blockComment], color: '#7d8590', fontStyle: 'italic' },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: '#82aaff' },
  { tag: [t.propertyName], color: '#ffcb6b' },
  { tag: [t.definition(t.variableName)], color: '#ededee' },
  { tag: [t.className, t.typeName], color: '#ffcb6b' },
  { tag: [t.operator, t.punctuation, t.bracket], color: '#a1a1a7' },
])

export function createCodeEditor(parent: HTMLElement, doc: string, onChange: (doc: string) => void): CodeEditor {
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc,
      extensions: [
        lineNumbers(),
        history(),
        drawSelection(),
        highlightActiveLine(),
        indentOnInput(),
        bracketMatching(),
        syntaxHighlighting(highlight),
        javascript(),
        keymap.of([...defaultKeymap, ...historyKeymap]),
        errorLine,
        theme,
        EditorView.lineWrapping,
        EditorView.contentAttributes.of({ 'aria-label': '模型代码' }),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) onChange(u.state.doc.toString())
        }),
      ],
    }),
  })
  return {
    setErrorLine: (line) => view.dispatch({ effects: setError.of(line) }),
    setDoc: (text) => {
      if (text !== view.state.doc.toString()) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } })
    },
    focus: () => view.focus(),
    destroy: () => view.destroy(),
  }
}
