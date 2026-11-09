// silt-meeting plugin entry (#1105). Headless: its entire surface is the
// /meeting slash command, which prepends a collapsible "Meeting information"
// details section to the top of the current note, pre-seeded with labeled
// empty field lines (Subject, When, Location, Organizer, Attendees, Join
// link, Notes).
import type { SiltPlugin } from '../../sdk'

export const manifest = {
  id: 'silt-meeting',
  name: 'Meeting Information',
  version: '1.0.0',
  author: 'Silt',
  description:
    'Prepend a collapsible Meeting information section with labeled fields via /meeting.',
  icon: 'event',
  // Documentation parity: first-party plugins are implicitly granted every
  // capability (seedFirstPartyGrants), so this declaration surfaces the
  // editor-schema need in the Plugins UI without gating anything.
  capabilities: { 'editor-schema': true }
}

export default {
  manifest
} satisfies SiltPlugin
