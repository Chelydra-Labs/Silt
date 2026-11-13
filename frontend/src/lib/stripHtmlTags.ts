/**
 * Strip well-formed HTML tags for plain-text display (#1153) — display-layer
 * only; stored NOTE content is never modified. Deliberately not folded into
 * `stripMarkdown` (D7): that helper feeds the Settings release-notes excerpt,
 * whose fenced-code/autolink behavior a tag rule would silently change.
 */
export function stripHtmlTags(input: string): string {
  // The leading [a-zA-Z] is what spares stray `<` (as in `count < limit`) and
  // bare `>` (as in `a > b`); `[^>]*` spans attributes and newlines.
  return input.replace(/<\/?[a-zA-Z][^>]*>/g, '')
}
