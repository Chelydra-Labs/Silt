// Frontend mirror of parser.ParsedBlock (backend/parser/models.go).
// This is the JSON shape that crosses the Wails IPC boundary in
// SaveFileBlocks / FetchPageBlocks. The editor never touches raw markdown —
// it deals only in this structured representation. Go's RenderFileContent
// remains the single on-disk serializer (#40 contract preserved).

export type BlockType = 'TASK' | 'NOTE' | 'HEADER' | 'CODE'

// Must match parser.DefaultTaskPriority for task nodes created in the editor.
export { DEFAULT_TASK_PRIORITY } from '../taskPriority'

export interface ParsedBlock {
  id: string
  parent_id: string
  type: string
  depth: number
  raw_text: string
  clean_text: string
  status?: string
  owner?: string
  start_date?: string
  due_date?: string
  recurrence?: string
  priority?: number
  line_number: number
  file_date?: string
  /** Fenced-code-block info string (the ```{lang} prefix). CODE blocks only. */
  language?: string
}

// ProseMirror / TipTap node JSON shape (the subset we produce/consume).
export interface NodeJSON {
  type: string
  attrs?: Record<string, unknown>
  content?: NodeJSON[]
  text?: string
  marks?: Array<{ type: string; attrs?: Record<string, unknown> }>
}

export interface DocJSON {
  type: 'doc'
  content: NodeJSON[]
}
