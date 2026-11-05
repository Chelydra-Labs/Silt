import { describe, expect, it } from 'vitest'
import { normalizePastedHTML, PASTE_HTML_SIZE_CAP } from './normalizePastedHTML'
import {
  BLOCKQUOTE_BODY,
  CODE_BLOCK,
  CSS_ONLY_EMPHASIS,
  GENUINE_TABLE,
  GOOGLE_DOCS_BODY,
  LAYOUT_TABLE_BODY,
  LETTERED_MSO_LIST,
  MSO_PSEUDO_LIST,
  NESTED_ORDERED_LIST,
  OUTLOOK_BODY,
  PRESENTATION_TABLE_2CELL,
  SHADED_CONTAINER,
  SILT_INTERNAL_CLIPBOARD
} from './fixtures'

const ALL_FIXTURES = [
  OUTLOOK_BODY,
  NESTED_ORDERED_LIST,
  LAYOUT_TABLE_BODY,
  MSO_PSEUDO_LIST,
  LETTERED_MSO_LIST,
  CSS_ONLY_EMPHASIS,
  BLOCKQUOTE_BODY,
  CODE_BLOCK,
  SHADED_CONTAINER,
  GENUINE_TABLE,
  GOOGLE_DOCS_BODY,
  PRESENTATION_TABLE_2CELL,
  SILT_INTERNAL_CLIPBOARD
]

function bodyOf(html: string): HTMLElement {
  return new DOMParser().parseFromString(html, 'text/html').body
}

function noteDivs(html: string): HTMLDivElement[] {
  return Array.from(
    bodyOf(html).querySelectorAll<HTMLDivElement>('div[data-type="note"]')
  )
}

describe('normalizePastedHTML guards', () => {
  it('returns internal Silt clipboards untouched (data-pm-slice)', () => {
    expect(normalizePastedHTML(SILT_INTERNAL_CLIPBOARD)).toBe(
      SILT_INTERNAL_CLIPBOARD
    )
    // The untouched payload still contains the wrapper a normalizer pass
    // would otherwise unwrap/strip.
    expect(normalizePastedHTML(SILT_INTERNAL_CLIPBOARD)).toContain(
      'data-pm-slice'
    )
  })

  it('returns oversized payloads untouched so the caller can use text', () => {
    const huge = `<p>${'x'.repeat(PASTE_HTML_SIZE_CAP)}</p>`
    expect(huge.length).toBeGreaterThan(PASTE_HTML_SIZE_CAP)
    expect(normalizePastedHTML(huge)).toBe(huge)
  })
})

describe('normalizePastedHTML Word artefacts', () => {
  it('drops script/style/xml/meta/link, comments and o:p markers', () => {
    const out = normalizePastedHTML(`<div>
      <script>alert(1)</script>
      <style>p{color:red}</style>
      <xml><w:foo/></xml>
      <meta name=Generator content=Word>
      <link rel=stylesheet href=x.css>
      <!--[if !supportLists]--><p class=MsoNormal id=para>Visible<o:p></o:p></p><!--[endif]-->
      <!-- a plain comment -->
    </div>`)
    expect(out).not.toMatch(/script|style|<xml|meta|link/i)
    expect(out).not.toContain('<!--')
    expect(out).not.toContain('o:p')
    expect(out).toContain('Visible')
  })

  it('drops class/id and presentational attributes', () => {
    const out = normalizePastedHTML(
      `<div class=WordSection1 id=body style='margin:0in;background-color:#fff' align=center width=600><p class=MsoNormal style='font-family:Calibri;font-size:11pt'>Text</p></div>`
    )
    expect(out).not.toContain('class=')
    expect(out).not.toContain('id=')
    expect(out).not.toContain('margin:')
    expect(out).not.toContain('font-family')
    expect(out).not.toContain('align=')
    expect(out).not.toContain('width=')
    expect(out).toContain('Text')
  })

  it('keeps the style declarations TipTap mark rules need', () => {
    const out = normalizePastedHTML(
      `<p><span style='color:#c00000;background-color:#fff2cc;font-weight:700;font-style:italic;text-decoration:underline'>runs</span></p>`
    )
    const span = bodyOf(out).querySelector('span')
    expect(span?.getAttribute('style')).toBe(
      'color: #c00000; background-color: #fff2cc; font-weight: 700; font-style: italic; text-decoration: underline'
    )
  })

  it('drops inert colors but keeps weight/style/decoration declarations', () => {
    const out = normalizePastedHTML(
      `<p><span style='color:windowtext;background-color:transparent;font-weight:400;font-style:normal;text-decoration:none'>plain</span></p>`
    )
    // `font-weight`/`font-style`/`text-decoration` are read by TipTap's mark
    // rules (including the clear-mark values), so they must survive even when
    // they look inert. Only colors are value-filtered.
    expect(bodyOf(out).querySelector('span')?.getAttribute('style')).toBe(
      'font-weight: 400; font-style: normal; text-decoration: none'
    )
    expect(out).toContain('plain')
    expect(out).not.toContain('windowtext')
    expect(out).not.toContain('transparent')
  })

  it('keeps the Google Docs not-bold wrapper declarations', () => {
    const out = normalizePastedHTML(GOOGLE_DOCS_BODY)
    const wrapper = bodyOf(out).querySelector('b')
    expect(wrapper?.getAttribute('style')).toBe('font-weight: normal')
    expect(bodyOf(out).querySelector('span')?.getAttribute('style')).toContain(
      'font-weight: 400'
    )
  })

  it('maps mso-highlight to background-color and font color to a span', () => {
    const out = normalizePastedHTML(
      `<p><span style='mso-highlight:yellow'>highlight</span><font color="#ff0000" face="Arial" size="3">red</font></p>`
    )
    const spans = Array.from(bodyOf(out).querySelectorAll('span'))
    expect(spans[0]?.getAttribute('style')).toBe('background-color: yellow')
    expect(spans[1]?.getAttribute('style')).toBe('color: #ff0000')
    expect(out).not.toContain('<font')
  })

  it('drops block-container color/background but keeps inline run colors (#1034)', () => {
    const out =
      normalizePastedHTML(`<div style='background-color:#f2f2f2;color:#222222'>
      <p style='background-color:#ffffff'>Paragraph</p>
      <table style='background:#fff'><tr><td style='background-color:#eeeeee'>Cell</td><td style='background-color:#ffffff'><span style='background-color:#d9ead3;color:#c00000'>Run</span></td></tr></table>
    </div>`)
    const body = bodyOf(out)
    expect(body.querySelector('div')?.hasAttribute('style')).toBe(false)
    expect(body.querySelector('p')?.hasAttribute('style')).toBe(false)
    expect(body.querySelector('td')?.hasAttribute('style')).toBe(false)
    expect(body.querySelector('span')?.getAttribute('style')).toBe(
      'background-color: #d9ead3; color: #c00000'
    )
  })

  it('strips Word artifacts from a captured Outlook body', () => {
    const out = normalizePastedHTML(OUTLOOK_BODY)
    expect(out).not.toContain('mso-')
    expect(out).not.toContain('o:p')
    expect(out).not.toContain('<!--')
    expect(out).not.toContain('class=')
    expect(out).not.toContain('windowtext')
  })
})

describe('normalizePastedHTML lists', () => {
  it('flattens ul items into sibling notes at depth 0', () => {
    const notes = noteDivs(
      normalizePastedHTML('<ul><li>One</li><li>Two</li></ul>')
    )
    expect(
      notes.map((n) => [
        n.getAttribute('data-depth'),
        n.getAttribute('data-bullet'),
        n.textContent
      ])
    ).toEqual([
      ['0', '- ', 'One'],
      ['0', '- ', 'Two']
    ])
  })

  it('honours ol start and nesting depth', () => {
    const notes = noteDivs(
      normalizePastedHTML(
        '<ol start=3><li>Install</li><li>Configure<ul><li>Windows</li><li>Linux</li></ul></li><li>Restart</li></ol>'
      )
    )
    expect(
      notes.map((n) => [
        n.getAttribute('data-depth'),
        n.getAttribute('data-bullet'),
        n.textContent
      ])
    ).toEqual([
      ['0', '3. ', 'Install'],
      ['0', '4. ', 'Configure'],
      ['1', '- ', 'Windows'],
      ['1', '- ', 'Linux'],
      ['0', '5. ', 'Restart']
    ])
  })

  it('turns continuation paragraphs inside an li into same-depth empty-bullet notes', () => {
    const notes = noteDivs(
      normalizePastedHTML(
        '<ul><li><p>First</p><p>Second line</p></li><li>Third</li></ul>'
      )
    )
    expect(
      notes.map((n) => [n.getAttribute('data-bullet'), n.textContent])
    ).toEqual([
      ['- ', 'First'],
      ['', 'Second line'],
      ['- ', 'Third']
    ])
  })

  it('converts mso-list pseudo paragraphs with levels and markers', () => {
    const notes = noteDivs(
      normalizePastedHTML(`<div>
        <p style='mso-list:l1 level1 lfo2'><span style='font-family:Symbol'><span style='mso-list:Ignore'>·</span></span>Alpha</p>
        <p style='mso-list:l1 level2 lfo2'><span style='mso-list:Ignore'>o</span>Beta</p>
        <p style='mso-list:l2 level1 lfo3'><span style='mso-list:Ignore'>1.</span>First numbered</p>
        <p style='mso-list:l2 level1 lfo3'><span style='mso-list:Ignore'>2.</span>Second numbered</p>
      </div>`)
    )
    expect(
      notes.map((n) => [
        n.getAttribute('data-depth'),
        n.getAttribute('data-bullet'),
        n.textContent
      ])
    ).toEqual([
      ['0', '- ', 'Alpha'],
      ['1', '- ', 'Beta'],
      ['0', '1. ', 'First numbered'],
      ['0', '2. ', 'Second numbered']
    ])
  })

  it('maps lettered and negative markers to the persistable unordered bullet', () => {
    const lettered = noteDivs(normalizePastedHTML(LETTERED_MSO_LIST))
    expect(lettered.map((n) => n.getAttribute('data-bullet'))).toEqual([
      '- ',
      '- '
    ])

    const negative = noteDivs(
      normalizePastedHTML('<ol start="-2"><li>Back</li><li>Again</li></ol>')
    )
    expect(negative.map((n) => n.getAttribute('data-bullet'))).toEqual([
      '- ',
      '- '
    ])
  })
})

describe('normalizePastedHTML blockquotes', () => {
  it('moves quotes onto notes and drops the wrapper', () => {
    const out = normalizePastedHTML(BLOCKQUOTE_BODY)
    expect(bodyOf(out).querySelector('blockquote')).toBeNull()
    const quoted = Array.from(bodyOf(out).querySelectorAll('[data-quote]'))
    expect(
      quoted.map((n) => [n.getAttribute('data-quote'), n.textContent])
    ).toEqual([
      ['> ', 'Quoted first'],
      ['> ', 'Quoted second with bold'],
      ['>> ', 'Nested quote']
    ])
  })

  it('lets the quote win over a list bullet', () => {
    const notes = noteDivs(
      normalizePastedHTML(
        '<blockquote><ul><li>Quoted bullet</li></ul></blockquote>'
      )
    )
    expect(notes[0]?.getAttribute('data-quote')).toBe('> ')
    expect(notes[0]?.getAttribute('data-bullet')).toBe('')
  })
})

describe('normalizePastedHTML tables', () => {
  it('unwraps layout tables before the Table extension can claim them', () => {
    const out = normalizePastedHTML(LAYOUT_TABLE_BODY)
    expect(bodyOf(out).querySelector('table')).toBeNull()
    expect(out).toContain('Wrapped title')
    expect(out).toContain('Body living inside a layout cell.')
    expect(out).toContain('Nested layout row')
  })

  it('unwraps a two-cell presentation table into separate blocks', () => {
    const out = normalizePastedHTML(PRESENTATION_TABLE_2CELL)
    expect(bodyOf(out).querySelector('table')).toBeNull()
    const paragraphs = Array.from(bodyOf(out).querySelectorAll('p'))
    expect(paragraphs.map((p) => p.textContent)).toEqual([
      'Left cell',
      'Right cell'
    ])
    // The old unwrap concatenated both cells into one text run.
    expect(out).not.toContain('Left cellRight cell')
  })

  it('keeps genuine tables, expands colspan and drops rowspan', () => {
    const out = normalizePastedHTML(GENUINE_TABLE)
    const body = bodyOf(out)
    expect(body.querySelector('table')).not.toBeNull()
    const firstRow = body.querySelector('tr')
    expect(
      Array.from(firstRow?.children ?? []).map((c) => c.textContent)
    ).toEqual(['Name', 'Values', ''])
    expect(out).not.toContain('rowspan')
    expect(out).not.toContain('colspan')
    const firstBodyCell = body.querySelectorAll('tr')[1]?.children[0]
    expect(firstBodyCell?.textContent).toBe('Row')
  })

  it('keeps the grid aligned after dropping a rowspan', () => {
    const out = normalizePastedHTML(GENUINE_TABLE)
    const rows = Array.from(bodyOf(out).querySelectorAll('tr'))
    // Row 2's "Row" cell spans rows 2-3; row 3 must gain an empty cell in the
    // spanned column so 3/4 stay under Values/'' rather than Name/Values.
    expect(
      Array.from(rows[2]?.children ?? []).map((c) => c.textContent)
    ).toEqual(['', '3', '4'])
    expect(rows.map((row) => row.children.length)).toEqual([3, 3, 3])
  })
})

describe('normalizePastedHTML blocks and code', () => {
  it('lifts pre>code language to data-language and drops one trailing newline', () => {
    const out = normalizePastedHTML(
      '<pre><code class="language-ts">const x = 1\n</code></pre>'
    )
    const pre = bodyOf(out).querySelector('pre')
    expect(pre?.getAttribute('data-language')).toBe('ts')
    expect(pre?.textContent).toBe('const x = 1')
  })

  it('converts inline-only divs to paragraphs but leaves Silt blocks alone', () => {
    const out = normalizePastedHTML(
      '<div>hello <b>world</b></div><div data-type="note" data-depth="0" data-bullet="- ">kept</div>'
    )
    const body = bodyOf(out)
    expect(body.querySelector('p')?.textContent).toBe('hello world')
    expect(body.querySelector('div[data-type="note"]')?.textContent).toBe(
      'kept'
    )
  })

  it('collapses consecutive whitespace-only spacer blocks to one', () => {
    const out = normalizePastedHTML(
      '<p>a</p><p>&nbsp;</p><p> </p><p><span>&nbsp;</span></p><p>b</p>'
    )
    const paragraphs = Array.from(bodyOf(out).querySelectorAll('p'))
    expect(
      paragraphs.map((p) => p.textContent?.replace(/\u00a0/g, ' '))
    ).toEqual(['a', ' ', 'b'])
  })
})

describe('normalizePastedHTML idempotence', () => {
  it('is stable across a second pass for every fixture', () => {
    for (const fixture of ALL_FIXTURES) {
      const once = normalizePastedHTML(fixture)
      expect(normalizePastedHTML(once)).toBe(once)
    }
  })

  it('leaves already-Silt block HTML structurally intact', () => {
    const silt =
      '<div data-type="header" data-depth="2">Title</div><div data-type="note" data-depth="1" data-bullet="- ">Bullet</div><div data-type="note" data-depth="0" data-quote="> ">Quote</div>'
    const out = normalizePastedHTML(silt)
    expect(out).toBe(silt)
  })
})
