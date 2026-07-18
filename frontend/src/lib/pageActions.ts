import { ResolvePageLink } from '../../bindings/silt/app.js'
import { pushNotification } from '../notifications/store.svelte'

export interface PageActionRef {
  notebook: string
  section: string
  page: string
}

export function pagePath(ref: PageActionRef): string {
  return [ref.notebook, ref.section, ref.page].filter(Boolean).join('/')
}

export async function shortestPageReference(
  ref: PageActionRef
): Promise<string> {
  const full = pagePath(ref)
  try {
    const resolved = await ResolvePageLink(full)
    return `[[${resolved?.exists && resolved.shortest ? resolved.shortest : full}]]`
  } catch {
    return `[[${full}]]`
  }
}

function reportClipboardFailure(): void {
  pushNotification({
    kind: 'error',
    message: 'Could not copy to the clipboard.'
  })
}

export async function copyPagePath(ref: PageActionRef): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(pagePath(ref))
    return true
  } catch {
    reportClipboardFailure()
    return false
  }
}

export async function copyPageReference(ref: PageActionRef): Promise<boolean> {
  const reference = await shortestPageReference(ref)
  try {
    await navigator.clipboard.writeText(reference)
    return true
  } catch {
    reportClipboardFailure()
    return false
  }
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    reportClipboardFailure()
    return false
  }
}
