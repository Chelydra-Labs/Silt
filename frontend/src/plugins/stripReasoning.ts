// Strip model reasoning/thinking content from LLM output (#483).
//
// WHY. Reasoning models (DeepSeek-R1, Qwen3 thinking, QwQ, Gemma-4-thinking,
// …) wrap internal scratchpad in <thought>/<think>/<thinking>/<reasoning> tags.
// Native providers (Google, Anthropic) separate reasoning into structured
// fields, so it never reaches `content`. But the OpenAI-compatible endpoints
// Silt supports (Ollama without `--reasoning-parser`, LM Studio, llama.cpp,
// generic servers) leave the tags inline in `message.content`. Without
// stripping they leak into rendered replies and persisted summaries.
//
// HOW. A depth-counting single pass is used instead of a regex replace: it
// correctly handles nesting (the outer block and everything inside it is
// dropped), unclosed blocks at end-of-string (truncated generation — the rest is
// reasoning), stray closing tags with no opener (Qwen3-2507+ emits a bare
// </think>), and mismatched open/close variants — all without backreference
// bookkeeping. Text is kept only while the nesting depth is 0.
//
// This is applied at the `ctx.ai.complete` SDK boundary (context.ts) so every
// plugin consumer — and the Sprint 22 streaming path when it lands — receives
// reasoning-free content. Reasoning has no value in rendered or persisted
// output; surfacing it later would be a separate channel feature, never leaked
// tags in `content`.

/** Reasoning-tag name variants emitted in the wild, with optional namespace
 *  prefixes used when models are bridged through OpenAI-compat shims. The set
 *  is intentionally the common XML-like forms; special-token delimiters (GPT-OSS
 *  `<|channel|>`, Kimi `◁think▷`) are not emitted by Silt's supported providers
 *  and are left out — extend this alternation if one is ever observed. */
const TAG_NAMES =
  'antthinking|reasoning|thinking|thought|think|antml:think|antml:thinking|mm:think'

/** Matches an open or close reasoning tag. `</` (char code 47) distinguishes a
 *  close. Trailing whitespace before `>` is tolerated (some servers pretty-print
 *  tags). `<thinker>` etc. are NOT matched: `\s*>` anchors the tag name. */
const TAG_RE = new RegExp(`</?(?:${TAG_NAMES})\\s*>`, 'gi')

/** Remove reasoning/thinking blocks from model output. Pure and idempotent:
 *  tag-free input is returned (trimmed) unchanged. */
export function stripReasoningContent(text: string): string {
  if (!text) return text

  let out = ''
  let last = 0
  let depth = 0
  let m: RegExpExecArray | null
  TAG_RE.lastIndex = 0
  while ((m = TAG_RE.exec(text)) !== null) {
    // Keep the text preceding this tag only when outside every reasoning block.
    if (depth === 0) out += text.slice(last, m.index)
    last = m.index + m[0].length
    if (m[0].charCodeAt(1) === 47) {
      // `</…>`: closes the most recent open. A stray closer at depth 0 (no open)
      // is dropped entirely rather than emitted.
      if (depth > 0) depth--
    } else {
      depth++
    }
  }
  // Trailing text survives only if no block is still open. An unclosed open tag
  // (generation truncated mid-thought) leaves depth > 0, so its tail is dropped.
  if (depth === 0) out += text.slice(last)

  // Collapse the blank-line seams a removed block can leave behind, then trim.
  return out.replace(/\n{3,}/g, '\n\n').trim()
}
