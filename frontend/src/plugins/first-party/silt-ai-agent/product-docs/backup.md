---
id: backup
title: Backup and migrate your vault
---

## Plain-folder backup

Your vault is plain text on disk. Copying the vault folder is always a valid
backup.

## Portable `.silt-vault` archive

**Settings → General → workspace ⋮ menu → Export vault…** creates a single
checksummed `.silt-vault` file. **Import vault…** restores into an empty local
folder.

### What is included

- Notebooks, sections, pages (notes and tasks)
- `.system/` (config, themes, templates, plugins)

### What is excluded

- The search index (rebuilt automatically after import)
- Linked external notebooks (re-link them on the other machine)

## Move vs Copy vs Export

| Action | Use when |
| --- | --- |
| **Move vault…** | Relocate the active vault on this machine |
| **Copy vault…** | Duplicate vault as another workspace here |
| **Export vault…** | Portable backup or move to another machine |
| **Import vault…** | Restore from a `.silt-vault` archive |

## Page history (not a backup)

Turn on **Capture page history** in Settings → Editor. Open **Page history**
from the editor chrome to preview or Compare a version, then Restore a
**live** page. After a delete, **Browse deleted pages** finds leftover
snapshots; Restore recreates the page, or Restore as… if that name is taken.

The in-app agent can list and preview versions and restore a live page
(`restore_page_version` always asks for Confirm). Recreating a deleted page
from leftovers is UI-only.
