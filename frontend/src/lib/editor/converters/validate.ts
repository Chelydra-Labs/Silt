// Validate stage — centralized security sanitization of the Token[] tree.
// This is the ONLY place future mark sanitizers are added (link-scheme
// allowlist, `__flat__` sentinel for dropping enclosing marks).
//
// Runtime dependency direction: tokenize.ts imports from this module
// (validateTokens, flattenFlat, isSafeLinkHref). This module has NO runtime
// dependency on tokenize — the `Token` type import is type-only.

import type { Token } from './tokenize'

// Allowlisted URL schemes for link hrefs (#168, centralized in #198). Mirrors
// TipTap's Link.isAllowedUri default. Non-allowlisted schemes (javascript:,
// data:, vbscript:, etc.) are dropped — the link text survives as literal
// text. This is the ONLY place future mark sanitizers are added.
const SAFE_LINK_SCHEMES = new Set([
  'http',
  'https',
  'ftp',
  'ftps',
  'mailto',
  'tel',
  'callto',
  'sms',
  'cid',
  'xmpp'
])

export function isSafeLinkHref(href: string): boolean {
  if (!href) return false
  if (
    href.startsWith('#') ||
    href.startsWith('/') ||
    href.startsWith('./') ||
    href.startsWith('../')
  )
    return true
  const colonIdx = href.indexOf(':')
  if (colonIdx === -1) return true
  const scheme = href.slice(0, colonIdx).toLowerCase()
  return SAFE_LINK_SCHEMES.has(scheme)
}

// Validate stage: security-critical pass run after tokenize. Walks the
// Token[] tree and rewrites unsafe structures into their safe equivalents:
// - link with disallowed scheme → drop the mark (text survives)
// - color span with extra attrs (e.g. onmouseover) → already stripped at
//   tokenize time via the [^>]* regex absorption; this is the documented
//   last-line-of-defense contract.
//
// Adding a new sanitize rule: emit `{ kind: 'mark', markType: '__flat__',
// children: <inner> }` from validateOne; flattenFlat then strips the wrapper
// and splices the children into the parent stream. Failure to use the
// sentinel for a "drop enclosing mark" intent will leak a non-standard
// markType into the legacy NodeJSON serializer, which silently emits empty
// markup.
export function validateTokens(tokens: Token[]): Token[] {
  return tokens.map((t) => validateOne(t))
}

function validateOne(t: Token): Token {
  if (t.kind === 'mark') {
    if (t.markType === 'link') {
      const href = (t.attrs?.href as string) || ''
      if (!isSafeLinkHref(href)) {
        // Drop the link mark — children survive as plain tokens.
        return { kind: 'mark', markType: '__flat__', children: t.children }
      }
    }
    return { ...t, children: validateTokens(t.children) }
  }
  return t
}

// `__flat__` is the internal sentinel that means "drop the enclosing mark,
// keep the children inline". Flatten it into the parent stream — children
// inherit the parent token's surrounding marks via this splice.
export function flattenFlat(tokens: Token[]): Token[] {
  const out: Token[] = []
  for (const t of tokens) {
    if (t.kind === 'mark' && t.markType === '__flat__') {
      out.push(...flattenFlat(t.children))
    } else if (t.kind === 'mark') {
      out.push({ ...t, children: flattenFlat(t.children) })
    } else {
      out.push(t)
    }
  }
  return out
}
