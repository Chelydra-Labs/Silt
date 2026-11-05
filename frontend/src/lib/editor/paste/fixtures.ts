// Captured-shape Word/Outlook clipboard fixtures.
//
// These are hand-trimmed copies of real desktop paste payloads: namespaces,
// `mso-*` declarations, conditional comments, `<o:p>` markers, layout tables
// and pseudo-list paragraphs are all present because that is what the browser
// hands the editor. Tests must not fetch anything; keeping the markup inline
// keeps the fixtures stable and reviewable.

const WORD_NS =
  'xmlns:o="urn:schemas-microsoft-com:office:office" ' +
  'xmlns:w="urn:schemas-microsoft-com:office:word" ' +
  'xmlns:m="http://schemas.microsoft.com/office/2004/12/omml"'

// (a) Heading + bold/italic paragraph + two-item bullet list. Exercises
// leaf-tag pastes (h2 -> headerBlock), inline marks, and Word wrappers.
export const OUTLOOK_BODY = `<html ${WORD_NS}>
<head><meta name=Generator content="Microsoft Word 15"><style>p.MsoNormal{margin:0}</style></head>
<body lang=EN-US style='word-wrap:break-word'>
<div class=WordSection1>
<h2 style='mso-style-link:"Heading 2 Char";background-color:#ffffff'>Q3 Planning<o:p></o:p></h2>
<p class=MsoNormal style='background-color:#ffffff'><b><span style='font-size:12.0pt;color:#1F3864'>Bold launch</span></b><span style='mso-spacerun:yes'>&nbsp;</span><i style='mso-bidi-font-style:normal'>and italic</i><span style='color:windowtext'> body text</span><o:p></o:p></p>
<!--[if !supportLists]--><ul type=disc>
<li class=MsoNormal style='mso-list:l0 level1 lfo1'><span style='font-family:Symbol'><span style='mso-list:Ignore'>·<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;</span></span></span>Ship the beta<o:p></o:p></li>
<li class=MsoNormal style='mso-list:l0 level1 lfo1'><span style='font-family:Symbol'><span style='mso-list:Ignore'>·<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;</span></span></span>Write the docs<o:p></o:p></li>
</ul><!--[endif]-->
<p class=MsoNormal><o:p>&nbsp;</o:p></p>
</div>
</body>
</html>`

// (b) Ordered list with a nested unordered list in the first item. Exercises
// the flattener's depth math and `<ol start>` ordinal.
export const NESTED_ORDERED_LIST = `<div class=WordSection1>
<ol start=3>
<li class=MsoNormal>Install the app</li>
<li class=MsoNormal>Configure it<ul type=disc><li class=MsoNormal>Windows</li><li class=MsoNormal>Linux</li></ul></li>
<li class=MsoNormal>Restart</li>
</ol>
</div>`

// (c) Outlook wraps the whole body in a single-cell layout table. This must
// be unwrapped BEFORE the Table extension sees it, or the page becomes a
// one-cell table block.
export const LAYOUT_TABLE_BODY = `<table class=MsoTableGrid border=0 cellspacing=0 cellpadding=0 role=presentation style='background:#ffffff'>
<tr><td width=600 valign=top style='background:#eeeeee;padding:0in 5.4pt 0in 5.4pt'>
<h1 style='mso-style-link:"Heading 1 Char"'>Wrapped title</h1>
<p class=MsoNormal>Body living inside a layout cell.</p>
<table border=0><tr><td><p class=MsoNormal>Nested layout row</p></td></tr></table>
</td></tr>
</table>`

// (d) Word "lists" that are really styled paragraphs (`mso-list:lN levelM`),
// with the bullet/number in an `mso-list:Ignore` marker span. No <ul>/<ol>.
export const MSO_PSEUDO_LIST = `<div class=WordSection1>
<p class=MsoListParagraphCxSpFirst style='margin-left:.5in;text-indent:-.25in;mso-list:l1 level1 lfo2'><span style='font-family:Symbol'><span style='mso-list:Ignore'>·<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;</span></span></span>Alpha</p>
<p class=MsoListParagraphCxSpMiddle style='margin-left:1in;text-indent:-.25in;mso-list:l1 level2 lfo2'><span style='font-family:"Courier New"'><span style='mso-list:Ignore'>o<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;</span></span></span>Beta</p>
<p class=MsoListParagraphCxSpLast style='margin-left:.5in;text-indent:-.25in;mso-list:l1 level1 lfo2'><span style='font-family:Symbol'><span style='mso-list:Ignore'>·<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp;</span></span></span>Gamma</p>
<p class=MsoListParagraphCxSpFirst style='margin-left:.5in;text-indent:-.25in;mso-list:l2 level1 lfo3'><span style='mso-list:Ignore'>1.</span>First numbered</p>
<p class=MsoListParagraphCxSpLast style='margin-left:.5in;text-indent:-.25in;mso-list:l2 level1 lfo3'><span style='mso-list:Ignore'>2.</span>Second numbered</p>
</div>`

// (e) Emphasis expressed only through CSS (no <b>/<i>/<u>). TipTap's
// Bold/Italic/Strike/Underline style parse rules must pick these up.
export const CSS_ONLY_EMPHASIS = `<p class=MsoNormal><span style='font-weight:700'>Bold by CSS</span> <span style='font-style:italic'>Italic by CSS</span> <span style='text-decoration:underline'>Underlined by CSS</span> <span style='font-weight:700;font-style:italic;text-decoration:underline'>All three</span></p>`

// (f) Blockquote, including a nested quote. Silt has no blockquote node, so
// this becomes `data-quote` prefixes on the descendant notes.
export const BLOCKQUOTE_BODY = `<div class=WordSection1>
<blockquote style='border-left:2.0pt solid #CCCCCC;margin-left:0in;padding-left:1.0pt'>
<p class=MsoQuote>Quoted first</p>
<p class=MsoQuote>Quoted second <b>with bold</b></p>
<blockquote style='border-left:2.0pt solid #999999'><p class=MsoQuote>Nested quote</p></blockquote>
</blockquote>
</div>`

// (g) Code block with the language in the nested <code> class, plus the
// trailing newline a "copy line" leaves behind.
export const CODE_BLOCK = `<div class=WordSection1>
<pre class=language-ts><code class=language-ts>const answer = 42
</code></pre>
</div>`

// (h) Shaded Word wrapper/table (VA4 / #1034): block-level backgrounds must
// be dropped, inline run backgrounds must survive as highlight marks.
export const SHADED_CONTAINER = `<div class=WordSection1 style='background-color:#f2f2f2;color:#222222'>
<p class=MsoNormal style='background-color:#ffffff'>Plain paragraph</p>
<p class=MsoNormal><span style='background-color:#fff2cc'>Highlighted run</span> and <span style='color:#c00000'>red run</span></p>
<table class=MsoTableGrid border=1 cellspacing=0 cellpadding=0 style='background:#ffffff'>
<tr><td style='background-color:#eeeeee'>Shaded cell</td><td style='background-color:#ffffff'><span style='background-color:#d9ead3'>Shaded span</span></td></tr>
</table>
</div>`

// A genuine table must survive normalization; colspan expands, rowspan drops.
export const GENUINE_TABLE = `<table border=1>
<tr><th>Name</th><th colspan=2>Values</th></tr>
<tr><td rowspan=2>Row</td><td>1</td><td><span style='background-color:#fff2cc'>2</span></td></tr>
<tr><td>3</td><td>4</td></tr>
</table>`

// Internal Silt clipboard payload: ProseMirror marks the wrapper with
// data-pm-slice, and the normalizer must leave it byte-for-byte untouched.
export const SILT_INTERNAL_CLIPBOARD = `<div data-pm-slice="0 0 []"><table class=MsoTableGrid border=0><tr><td><p class=MsoNormal>Internal table must stay a table</p></td></tr></table></div>`
