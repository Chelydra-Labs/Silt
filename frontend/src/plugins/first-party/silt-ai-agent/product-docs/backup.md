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
