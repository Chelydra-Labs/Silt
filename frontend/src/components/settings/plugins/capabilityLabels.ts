// Shared capability display labels + qualifier formatting for the Plugins
// settings surface. Consumed by both the install-preview capability list
// (PluginInstallFlow) and the per-card grant/revoke list (CapabilityGrantList),
// so neither component depends on the other for this lookup.
//
// Capability ids are the manifest's `capabilities` map keys; the qualifier is
// the value (`true` = granted at any scope, or a string scope like "notebook").
export const capabilityLabels: Record<string, string> = {
  'read-files': 'Read notebook files',
  'write-files': 'Write notebook files',
  network: 'Network access',
  'os-open': 'Open files / URLs',
  'os-clipboard': 'Clipboard',
  'os-notify': 'Notifications',
  'ui-surface': 'Render UI surfaces',
  'editor-schema': 'Extend the editor',
  'content-mutate': 'Create and modify content',
  ai: 'AI completions'
}

export function qualifierLabel(q: true | string): string {
  if (q === true || q === 'granted' || q === '') return ''
  return ` (${q})`
}
