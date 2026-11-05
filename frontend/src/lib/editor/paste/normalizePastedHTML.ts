// Paste HTML normalizer — a pure `(html) => html` pass that turns Word,
// Outlook and generic browser clipboard HTML into the containers Silt's
// schema can express.
//
// This module owns CONTAINER-level work only (lists, blockquotes, layout
// tables, wrapper divs, Word artefacts). Mark/leaf-tag fidelity belongs to
// the schema's parse rules; keeping the two separate means the schema stays
// context-free and this pass is testable as a pure function.
//
// Pipeline order matters and is load-bearing:
//   1. passthrough guards (internal clipboard + size cap)
//   2. drop unsafe/inert elements and comments
//   3. unwrap layout tables before the Table extension can claim them
//   4. flatten real + Word pseudo-lists
//   5. turn blockquotes into note-level quote attrs
//   6. code-block language + spacer div cleanup
//   7. inline-only divs -> paragraphs
//   8. collapse whitespace spacer blocks
//   9. filter attributes/styles last (mso-list must survive until step 4)

import { colorFromStyle } from './styleValues'

// Chromium/WebKit cap clipboard payloads around this size anyway; the guard
// exists so a pathological payload falls back to the text pipeline instead of
// blocking the editor while a multi-megabyte DOM is rebuilt.
export const PASTE_HTML_SIZE_CAP = 2 * 1024 * 1024

const PM_SLICE_ATTR_RE = /\bdata-pm-slice\s*=/

/**
 * How the paste pipeline should treat a clipboard HTML string.
 *
 * - `passthrough`: internal Silt payload; hands off unchanged so block copies
 *   keep their serialized shape.
 * - `text`: payload is too large to parse safely on the main thread; callers
 *   must use the plain-text clipboard payload instead.
 * - `normalize`: external HTML that the normalizer can process.
 */
export type PasteHTMLDecision = 'normalize' | 'passthrough' | 'text'

export function classifyPastedHTML(
  html: string | null | undefined
): PasteHTMLDecision {
  // `data-pm-slice` is checked before the size cap: an internal copy must never
  // be downgraded to plain text, whatever its length.
  if (html && PM_SLICE_ATTR_RE.test(html)) return 'passthrough'
  if (html && html.length > PASTE_HTML_SIZE_CAP) return 'text'
  return 'normalize'
}

const UNSAFE_TAGS = new Set([
  'script',
  'style',
  'xml',
  'meta',
  'link',
  'title',
  'o:p'
])

const BLOCK_TAGS = new Set([
  'address',
  'article',
  'aside',
  'blockquote',
  'center',
  'details',
  'div',
  'dl',
  'fieldset',
  'figcaption',
  'figure',
  'footer',
  'form',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'header',
  'hr',
  'li',
  'main',
  'nav',
  'ol',
  'p',
  'pre',
  'section',
  'summary',
  'table',
  'tbody',
  'td',
  'tfoot',
  'th',
  'thead',
  'tr',
  'ul'
])

// Containers whose color is page chrome, not text formatting. Keeping their
// background/foreground would turn every Word section/table wrapper into a
// per-run highlight mark (white boxes on dark themes, `<span style=…>` noise
// in stored markdown). Inline containers keep their color because that is real
// character formatting (#1034).
const ALLOWED_STYLE_PROPS = new Set([
  'color',
  'background-color',
  'font-weight',
  'font-style',
  'text-decoration'
])

const KEPT_ATTRS = new Set(['href', 'src', 'alt', 'title'])

function isBlockTag(tagName: string): boolean {
  return BLOCK_TAGS.has(tagName.toLowerCase())
}

function isElement(node: Node | null): node is Element {
  return !!node && node.nodeType === Node.ELEMENT_NODE
}

// Raw style attribute parsing: CSSOM drops unknown `mso-*` properties in some
// engines (jsdom does), but Word's list/emphasis metadata lives there. Parse
// the attribute string ourselves so behaviour is identical everywhere.
function parseStyleDeclarations(raw: string | null): [string, string][] {
  if (!raw) return []
  const out: [string, string][] = []
  for (const part of raw.split(';')) {
    const colon = part.indexOf(':')
    if (colon < 0) continue
    const name = part.slice(0, colon).trim().toLowerCase()
    const value = part
      .slice(colon + 1)
      .replace(/!important\s*$/i, '')
      .trim()
    if (name && value) out.push([name, value])
  }
  return out
}

function buildStyleMap(
  raw: string | null,
  isBlockContainer: boolean
): Map<string, string> {
  const map = new Map<string, string>()
  for (const [name, value] of parseStyleDeclarations(raw)) {
    if (name === 'mso-highlight') {
      const color = colorFromStyle(value)
      if (color && !map.has('background-color')) {
        map.set('background-color', color)
      }
      continue
    }
    if (name.startsWith('mso-')) continue
    if (!ALLOWED_STYLE_PROPS.has(name)) continue
    if (isBlockContainer && (name === 'color' || name === 'background-color')) {
      continue
    }
    if (name === 'color' || name === 'background-color') {
      const color = colorFromStyle(value)
      if (color) map.set(name, color)
      continue
    }
    // `font-weight`, `font-style` and `text-decoration` are NOT inert: TipTap's
    // Bold/Italic/Underline rules read them, including the "normal" values that
    // clear an inherited mark (Google Docs wraps its payload in
    // `<b style="font-weight:normal">`). Keep them verbatim.
    map.set(name, value)
  }
  return map
}

function serializeStyleMap(map: Map<string, string>): string {
  return [...map].map(([name, value]) => `${name}: ${value}`).join('; ')
}

function filterStyle(raw: string | null, isBlockContainer: boolean): string {
  return serializeStyleMap(buildStyleMap(raw, isBlockContainer))
}

function unwrapElement(el: Element): void {
  const parent = el.parentNode
  if (!parent) return
  while (el.firstChild) parent.insertBefore(el.firstChild, el)
  parent.removeChild(el)
}

function removeUnsafeElements(root: HTMLElement): void {
  for (const el of Array.from(root.getElementsByTagName('*'))) {
    if (UNSAFE_TAGS.has(el.tagName.toLowerCase())) el.remove()
  }
}

function removeComments(root: HTMLElement): void {
  const walker = root.ownerDocument!.createTreeWalker(
    root,
    NodeFilter.SHOW_COMMENT
  )
  const comments: Comment[] = []
  while (walker.nextNode()) comments.push(walker.currentNode as Comment)
  for (const comment of comments) comment.remove()
}

function tableCells(table: HTMLTableElement): HTMLTableCellElement[] {
  const rows = Array.from(table.rows ?? [])
  if (rows.length > 0) {
    return rows.flatMap((row) => Array.from(row.cells ?? []))
  }
  return Array.from(table.querySelectorAll('td, th')) as HTMLTableCellElement[]
}

function isLayoutTable(table: HTMLTableElement): boolean {
  const role = (table.getAttribute('role') || '').toLowerCase()
  if (role === 'presentation' || role === 'none') return true
  const rows = Array.from(table.rows ?? [])
  if (rows.length !== 1) return false
  return Array.from(rows[0].cells ?? []).length === 1
}

function unwrapLayoutTables(root: HTMLElement): void {
  for (const table of Array.from(root.querySelectorAll('table'))) {
    if (!table.isConnected) continue
    if (!isLayoutTable(table as HTMLTableElement)) continue
    const fragment = table.ownerDocument.createDocumentFragment()
    for (const cell of tableCells(table as HTMLTableElement)) {
      // Each cell's inline runs become at least one block, or a multi-cell
      // presentation table would glue neighbours together ("ab") once the
      // cell boundary disappears.
      wrapInlineRuns(cell)
      while (cell.firstChild) fragment.appendChild(cell.firstChild)
    }
    if (!fragment.firstChild) {
      while (table.firstChild) fragment.appendChild(table.firstChild)
    }
    table.replaceWith(fragment)
  }
}

function spanCount(raw: string | null): number {
  const value = Number.parseInt(raw || '1', 10)
  return Number.isFinite(value) && value > 1 ? value : 1
}

// Spread colspan into empty sibling cells and drop rowspan (the GFM/table model
// is a strict grid with no spanning). Dropping a rowspan would shift every
// later cell left, so track the occupied columns and pad each following row at
// the spanned column instead — `serializeTableToGFM` only pads at the end, and
// end-padding would put "3"/"4" under the wrong headers.
function expandTableCells(root: HTMLElement): void {
  for (const table of Array.from(root.querySelectorAll('table'))) {
    const rows = Array.from((table as HTMLTableElement).rows ?? [])
    // column -> number of rows this span still covers, current row included.
    const spans = new Map<number, number>()
    for (const row of rows) {
      let column = 0
      for (const cell of Array.from(row.cells ?? [])) {
        while ((spans.get(column) ?? 0) > 0) {
          const filler = cell.ownerDocument.createElement('td')
          row.insertBefore(filler, cell)
          column++
        }
        const colspan = spanCount(cell.getAttribute('colspan'))
        const rowspan = spanCount(cell.getAttribute('rowspan'))
        cell.removeAttribute('colspan')
        cell.removeAttribute('rowspan')
        for (let i = 1; i < colspan; i++) {
          cell.after(
            cell.ownerDocument.createElement(cell.tagName.toLowerCase())
          )
        }
        if (rowspan > 1) {
          for (let i = 0; i < colspan; i++) {
            spans.set(column + i, Math.max(spans.get(column + i) ?? 0, rowspan))
          }
        }
        column += colspan
      }
      // A span can extend past this row's last cell; pad the tail so the next
      // row starts at the same column.
      while (spans.size > 0 && column <= Math.max(...spans.keys())) {
        row.appendChild(row.ownerDocument.createElement('td'))
        column++
      }
      for (const [spanColumn, remaining] of spans) {
        if (remaining <= 1) spans.delete(spanColumn)
        else spans.set(spanColumn, remaining - 1)
      }
    }
  }
}

function hasBlockChild(el: Element): boolean {
  return Array.from(el.children).some((child) => isBlockTag(child.tagName))
}

function hasMeaningfulContent(nodes: Node[]): boolean {
  return nodes.some(
    (node) =>
      isElement(node) ||
      (node.nodeType === Node.TEXT_NODE && !!node.textContent?.trim())
  )
}

function makeNote(
  doc: Document,
  depth: number,
  bullet: string,
  content: Node[]
): HTMLDivElement {
  const div = doc.createElement('div')
  div.setAttribute('data-type', 'note')
  div.setAttribute('data-depth', String(depth))
  div.setAttribute('data-bullet', bullet)
  for (const node of content) div.appendChild(node)
  return div
}

function isNoteAtDepth(node: Node, depth: number): node is HTMLDivElement {
  return (
    isElement(node) &&
    node.tagName === 'DIV' &&
    node.getAttribute('data-type') === 'note' &&
    node.getAttribute('data-depth') === String(depth)
  )
}

function collectListItemNodes(li: Element, depth: number): Node[] {
  const doc = li.ownerDocument
  const result: Node[] = []
  let inlineBuffer: Node[] = []

  const flush = () => {
    if (inlineBuffer.length && hasMeaningfulContent(inlineBuffer)) {
      result.push(makeNote(doc, depth, '', inlineBuffer))
    }
    inlineBuffer = []
  }

  for (const child of Array.from(li.childNodes)) {
    if (child.nodeType === Node.TEXT_NODE) {
      inlineBuffer.push(child)
      continue
    }
    if (!isElement(child)) continue
    const tag = child.tagName
    if (tag === 'UL' || tag === 'OL') {
      flush()
      flattenListInto(
        child as HTMLUListElement | HTMLOListElement,
        depth + 1,
        result
      )
      continue
    }
    if (tag === 'P') {
      flush()
      result.push(makeNote(doc, depth, '', Array.from(child.childNodes)))
      continue
    }
    if (tag === 'DIV' && !hasBlockChild(child)) {
      flush()
      result.push(makeNote(doc, depth, '', Array.from(child.childNodes)))
      continue
    }
    if (isBlockTag(tag)) {
      flush()
      // Headings/code/tables/quotes stay as sibling flow content; the later
      // stages (quote marking, div->p) handle them in place.
      result.push(child)
      continue
    }
    inlineBuffer.push(child)
  }
  flush()
  return result
}

function flattenListInto(
  list: HTMLUListElement | HTMLOListElement,
  depth: number,
  out: Node[]
): void {
  const doc = list.ownerDocument
  const ordered = list.tagName === 'OL'
  const start = ordered
    ? Number.parseInt(list.getAttribute('start') || '1', 10)
    : 1
  let ordinal = Number.isFinite(start) ? start : 1

  for (const child of Array.from(list.children)) {
    if (child.tagName !== 'LI') continue
    // `<li value="N">` restarts numbering mid-list; browsers emit it when a
    // slice of a numbered list is copied. Unrepresentable values (< 1) keep
    // the running ordinal.
    const explicit = Number.parseInt(child.getAttribute('value') ?? '', 10)
    if (ordered && Number.isFinite(explicit) && explicit >= 1) {
      ordinal = explicit
    }
    // Only non-negative ordinals are representable in the block model; a
    // negative `<ol start>` marker would be dropped by both bullet detectors
    // on save, so fall back to the unordered marker.
    const marker = ordered && ordinal >= 1 ? `${ordinal}. ` : '- '
    if (ordered) ordinal++

    const nodes = collectListItemNodes(child, depth)
    let marked = false
    for (const node of nodes) {
      if (
        !marked &&
        isNoteAtDepth(node, depth) &&
        node.getAttribute('data-bullet') === ''
      ) {
        node.setAttribute('data-bullet', marker)
        marked = true
      }
      out.push(node)
    }
    // An <li> that contains only a nested list still owns an (empty) item at
    // this depth; keep the marker so the outline shape survives.
    if (!marked) {
      out.splice(out.length - nodes.length, 0, makeNote(doc, depth, marker, []))
    }
  }
}

function isMsoListIgnore(el: Element): boolean {
  return (
    el.tagName === 'SPAN' &&
    /mso-list\s*:\s*ignore/i.test(el.getAttribute('style') || '')
  )
}

function isMsoMarkerWrapper(el: Element): boolean {
  if (el.tagName !== 'SPAN') return false
  if (isMsoListIgnore(el)) return true
  return Array.from(el.querySelectorAll('span')).some(isMsoListIgnore)
}

function findMsoMarker(el: Element): HTMLElement | null {
  return (
    (Array.from(el.querySelectorAll('span')).find(isMsoListIgnore) as
      HTMLElement | undefined) ?? null
  )
}

function pseudoBullet(marker: HTMLElement | null): string {
  const text = (marker?.textContent || '').replace(/\u00a0/g, ' ').trim()
  // The block model persists only ordered (non-negative arabic) and unordered
  // markers. Lettered markers and Word's symbol fonts are unordered lists;
  // emitting `a. ` would not survive a save/reload, so map them to `- `.
  if (/^\d+[.)]?$/.test(text)) return `${Number.parseInt(text, 10)}. `
  return '- '
}

function flattenPseudoLists(root: HTMLElement): void {
  for (const el of Array.from(root.querySelectorAll<HTMLElement>('[style]'))) {
    if (!el.isConnected) continue
    if (el.closest('li')) continue
    // A pseudo paragraph already folded into a note must not become a note
    // inside a note (noteBlock content is inline-only).
    if (el.closest('[data-type="note"]')) continue
    if (!isBlockTag(el.tagName)) continue
    const rawStyle = el.getAttribute('style') || ''
    const listStyle = /mso-list\s*:\s*([^;]*)/i.exec(rawStyle)
    if (!listStyle) continue

    const level = /level\s*(\d+)/i.exec(listStyle[1])
    const depth = level ? Math.max(0, Number.parseInt(level[1], 10) - 1) : 0
    const marker = findMsoMarker(el)
    const bullet = pseudoBullet(marker)

    // Drop the marker's wrapper span (usually a symbol-font span around the
    // ignore span), not just the innermost node, so no stray bullet text is
    // left behind.
    if (marker) {
      let top: Element = marker
      while (top.parentElement && top.parentElement !== el) {
        top = top.parentElement
      }
      top.remove()
    }

    const note = makeNote(el.ownerDocument, depth, bullet, [])
    while (el.firstChild) note.appendChild(el.firstChild)
    const first = note.firstChild
    if (first?.nodeType === Node.TEXT_NODE) {
      first.textContent = (first.textContent || '').replace(/^[\s\u00a0]+/, '')
    }
    el.replaceWith(note)
  }
}

function flattenLists(root: HTMLElement): void {
  const lists = Array.from(root.querySelectorAll('ul, ol'))
  for (const list of lists) {
    if (!list.isConnected) continue
    const nodes: Node[] = []
    flattenListInto(list as HTMLUListElement | HTMLOListElement, 0, nodes)
    const fragment = root.ownerDocument.createDocumentFragment()
    for (const node of nodes) fragment.appendChild(node)
    list.replaceWith(fragment)
  }
  flattenPseudoLists(root)
}

function wrapInlineRuns(container: Element): void {
  const doc = container.ownerDocument
  let node = container.firstChild
  while (node) {
    if (isElement(node) && isBlockTag(node.tagName)) {
      node = node.nextSibling
      continue
    }
    const paragraph = doc.createElement('p')
    container.insertBefore(paragraph, node)
    while (node && !(isElement(node) && isBlockTag(node.tagName))) {
      const next: ChildNode | null = node.nextSibling
      paragraph.appendChild(node)
      node = next
    }
    if (!hasMeaningfulContent(Array.from(paragraph.childNodes))) {
      paragraph.remove()
    }
  }
}

function applyBlockquotes(root: HTMLElement): void {
  const quotes = Array.from(root.querySelectorAll('blockquote'))
  for (const quote of quotes) {
    if (!quote.isConnected) continue
    wrapInlineRuns(quote)
    let ancestorDepth = 0
    let ancestor = quote.parentElement
    while (ancestor) {
      if (ancestor.tagName === 'BLOCKQUOTE') ancestorDepth++
      ancestor = ancestor.parentElement
    }
    const prefix = `${'>'.repeat(ancestorDepth + 1)} `
    for (const el of Array.from(quote.querySelectorAll('*'))) {
      if (!isBlockTag(el.tagName)) continue
      // Nested quotes get their own (deeper) prefix when their turn comes.
      if (el.closest('blockquote') !== quote) continue
      el.setAttribute('data-quote', prefix)
      // The block model cannot carry bullet + quote; the quote wins.
      if (el.getAttribute('data-type') === 'note') {
        el.setAttribute('data-bullet', '')
      }
    }
  }
  for (const quote of quotes) {
    if (quote.isConnected) unwrapElement(quote)
  }
}

function lastTextNode(root: Element): Text | null {
  const walker = root.ownerDocument!.createTreeWalker(
    root,
    NodeFilter.SHOW_TEXT
  )
  let last: Text | null = null
  while (walker.nextNode()) last = walker.currentNode as Text
  return last
}

function normalizePreBlocks(root: HTMLElement): void {
  for (const pre of Array.from(root.querySelectorAll('pre'))) {
    const source = pre.querySelector('code') ?? pre
    const classes = `${source.getAttribute('class') || ''} ${
      pre.getAttribute('class') || ''
    }`
    const language = /(?:^|\s)(?:language|lang)-([\w+#.-]+)/.exec(classes)
    if (language && !pre.getAttribute('data-language')) {
      pre.setAttribute('data-language', language[1])
    }
    // A copied code block usually ends in one newline that is not part of the
    // code ("copy line" semantics); the fence supplies its own.
    const last = lastTextNode(pre)
    if (last && last.data.endsWith('\n')) {
      last.data = last.data.slice(0, -1)
    }
  }
}

function convertInlineDivs(root: HTMLElement): void {
  for (const div of Array.from(root.querySelectorAll('div'))) {
    if (!div.isConnected) continue
    if (div.hasAttribute('data-type')) continue
    if (hasBlockChild(div)) continue
    const paragraph = div.ownerDocument.createElement('p')
    for (const attr of Array.from(div.attributes)) {
      paragraph.setAttribute(attr.name, attr.value)
    }
    while (div.firstChild) paragraph.appendChild(div.firstChild)
    div.replaceWith(paragraph)
  }
}

function isWhitespaceBlock(el: Element): boolean {
  if (el.tagName !== 'P' && el.tagName !== 'DIV') return false
  if (el.hasAttribute('data-type')) return false
  if (hasBlockChild(el) || el.querySelector('img, table, hr')) return false
  const text = (el.textContent || '').replace(/\u00a0/g, ' ').trim()
  return text === ''
}

function collapseSpacerBlocks(root: HTMLElement): void {
  const parents: Element[] = [
    root,
    ...Array.from(root.querySelectorAll('td, th, div, section, article, li'))
  ]
  for (const parent of parents) {
    let previousWasSpacer = false
    for (const child of Array.from(parent.children)) {
      const spacer = isWhitespaceBlock(child)
      if (spacer && previousWasSpacer) {
        child.remove()
        continue
      }
      previousWasSpacer = spacer
    }
  }
}

function convertFontElement(font: Element): Element {
  const span = font.ownerDocument.createElement('span')
  const style = buildStyleMap(font.getAttribute('style'), false)
  const attrColor = colorFromStyle(font.getAttribute('color'))
  // An explicit style declaration outranks the legacy presentational hint.
  if (attrColor && !style.has('color')) style.set('color', attrColor)
  const css = serializeStyleMap(style)
  if (css) span.setAttribute('style', css)
  while (font.firstChild) span.appendChild(font.firstChild)
  font.replaceWith(span)
  return span
}

function sanitizeElement(el: Element): void {
  const isBlockContainer = isBlockTag(el.tagName)
  for (const attr of Array.from(el.attributes)) {
    const name = attr.name.toLowerCase()
    if (name === 'style' || name.startsWith('data-')) continue
    if (KEPT_ATTRS.has(name)) continue
    el.removeAttribute(attr.name)
  }
  const style = filterStyle(el.getAttribute('style'), isBlockContainer)
  if (style) el.setAttribute('style', style)
  else el.removeAttribute('style')
}

function sanitizeTree(root: HTMLElement): void {
  for (const el of Array.from(root.querySelectorAll('*'))) {
    if (!el.isConnected) continue
    // Marker spans are stripped here as a backstop for Word markup that put
    // them inside a real <li> (where the pseudo-list pass does not look).
    if (isMsoMarkerWrapper(el)) {
      el.remove()
      continue
    }
    if (el.tagName === 'FONT') {
      convertFontElement(el)
      continue
    }
    sanitizeElement(el)
  }
}

/**
 * Normalize externally pasted HTML into Silt-expressible containers.
 *
 * Returns the input verbatim when it is an internal Silt clipboard payload
 * (`data-pm-slice`) or too large to parse safely; callers must use
 * {@link classifyPastedHTML} to send the size case to the plain-text path.
 */
export function normalizePastedHTML(html: string): string {
  if (!html) return html
  if (classifyPastedHTML(html) !== 'normalize') return html

  const doc = new DOMParser().parseFromString(html, 'text/html')
  const body = doc.body

  removeUnsafeElements(body)
  removeComments(body)
  unwrapLayoutTables(body)
  expandTableCells(body)
  flattenLists(body)
  applyBlockquotes(body)
  normalizePreBlocks(body)
  convertInlineDivs(body)
  collapseSpacerBlocks(body)
  sanitizeTree(body)

  return body.innerHTML.trim()
}
