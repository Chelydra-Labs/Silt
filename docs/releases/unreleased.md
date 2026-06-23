# Security

Content and filesystem hardening pass:

- **Attachments now block scriptable and shortcut file types** — `.html`, `.svg`, `.js`, `.lnk`, `.url`, `.desktop`, `.jar`, and similar can no longer be copied into a notebook's attachments. Opening one of these would hand it to the OS default handler at a `file://` URL inside the vault, where it could run script, redirect to a phishing page, or execute a shortcut. Common documents and media (`.pdf`, `.docx`, images, audio, video) attach exactly as before.
- **Vault, plugin, and attachment files are now written with owner-only permissions** (`0o600`, directories `0o700`) so a co-tenant on a shared multi-user host can no longer read your config, plugin data, or attachments. Your note content is unchanged.
- **Malicious or oversized synced files can no longer exhaust memory or hang the indexer.** Every user-supplied config, theme, template, and settings file is now size-capped before it is parsed, and the task-parsing regexes are fixed in the app rather than configurable, closing a denial-of-service vector from a hostile synced vault.

# Notes

- Hardening is Linux-first; on Windows and macOS single-user installs the permission and parsing changes are effectively no-ops but do not regress anything.
