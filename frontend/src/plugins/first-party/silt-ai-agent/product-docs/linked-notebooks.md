---
id: linked-notebooks
title: Linked notebooks (external folders)
---

## What a linked notebook is

A **linked notebook** is a folder **outside** your main vault that Silt browses,
searches, and edits **in place**. Files are never copied into the vault on link.
Use this for cloud mounts (OneDrive, Dropbox, SharePoint) or another notes
folder you want beside the vault.

## Link a folder

1. In the sidebar notebook area, choose **Link External Folder…**.  
2. Pick a folder in the native dialog.  
3. The folder appears as a notebook in the tree (display name must be unique).

Nearby actions often include **New Notebook** and **Open Notebook…**.

## Unlink

On a linked notebook’s context menu, choose **Unlink** (not Delete). Confirm
**Unlink Notebook?** Silt stops indexing that root; files on disk are left
alone.

## What is not included

Vault **Move**, **Copy**, and **Export** (`.silt-vault`) **do not** bundle
linked folders. Re-link them on another machine after restore. Search can
include linked content when you enable the **+ Linked** filter in **Search
vault**.

## Tips

- If a linked root is offline or missing, create/edit there may be blocked until
  the folder is available again.  
- Deletes inside a linked notebook remove files in that external folder.  
- Prefer link over import when the folder is already the system of record.
