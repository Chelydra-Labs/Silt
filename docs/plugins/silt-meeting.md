# Meeting Information (`silt-meeting`)

Prepends a collapsible **Meeting information** section to the top of the
current note via the `/meeting` slash command — pre-seeded with seven labeled
empty field lines so meeting metadata lands in a consistent place and shape.

## What it does

Typing `/meeting` (and selecting the command) inserts a foldable
`<details>` section as the **first block** of the note, above all existing
content, containing:

- **Subject:**
- **When:**
- **Location:**
- **Organizer:**
- **Attendees:**
- **Join link:**
- **Notes:**

The caret lands on the **Subject:** line, so typing or pasting immediately
goes inside the section. In a brand-new note (the only block was the
`/meeting` text itself), the section replaces that emptied block and is the
note's only content. Running `/meeting` again inserts a second section —
there is no de-duplication.

## Expanded / collapsed behavior

The section is inserted **expanded** (its `open` flag is set in the document
model, and the section's view auto-expands), with the caret on the
**Subject:** line. Collapse and expand in-session via the section's toggle —
each toggle flips the in-document `open` flag, so the change follows the
normal edit/save cycle (the saved bytes are identical either way).

Collapse state is **ephemeral**: the on-disk file never records it (no
`<details open>` is ever written; the file is the truth), and a section
re-renders **collapsed** after reopening the note, like every other details
section.

Headless plugin — no view component, no settings page, on by default. The
slash command is its entire surface. First-party plugins are implicitly
granted every capability (`seedFirstPartyGrants`), so the manifest's
`editor-schema` declaration is documentation parity, not a functioning gate;
no `config.yaml` grants entry is needed.

## Pasting into the section

Pasting from **Outlook / Word** (or any rich source) with the caret on a
field line lands the content **inside** the section, normalized by the
existing paste pipeline (layout-table unwrapping, `mso-list` flattening,
mark conversion) — no plugin paste code is involved.

## Persistence

The whole section serializes as a single `DETAILS` block whose text is the
`<details>…</details>` HTML (one line per field). Save + reopen keeps it as
the first block with every field intact.

## Related

- Issue: #1105
- Foldable details sections: #183, #310
- Slash-command plugin SDK: #110, #1035
- Plugin author guide: [PLUGIN_DEVELOPMENT.md](../PLUGIN_DEVELOPMENT.md)
