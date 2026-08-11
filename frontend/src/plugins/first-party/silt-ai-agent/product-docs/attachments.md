---
id: attachments
title: Attach files and images
---

## Attach a file to a note

1. Put the caret in the page editor.  
2. Type **`/`** and choose **Attach File** (“Pick a file and embed it in the
   note”).  
3. Select a file in the system picker.

Images show inline in the note. Other files appear as attachment cards; click
to open with the OS default app.

## Where files live

Silt **copies** the file into that notebook’s **`attachments/`** folder (not
move, not a bare external link). Name collisions get a counter suffix
(`report-1.pdf`). The `attachments/` directory is hidden from the normal
sidebar tree and is not treated as a notes section.

## Limits

- Max size about **100 MB** per file.  
- Executables and many script types are blocked for safety.  
- Prefer **Attach File** so the note gets a proper embed; dropping files only
  into the folder without the command may not insert a reference in the page.

## Markdown shape

- Images: standard `![alt](attachments/…)`  
- Other files: a Silt embed marker that round-trips through the parser  

Attachments under a task can move with the task when you reorder blocks.
