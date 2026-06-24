# Silt Security Audit Register

This document is the authoritative register of security findings from the
2026-06-22 audit. Each finding has a severity (P0–P2), a status (Open /
Fixed), and a reference to the PR that closed it. It is updated whenever a
finding is closed or a new one is discovered.

| Finding | Severity | Description | Status | Fixed in |
|---|---|---|---|---|
| F1 | P0 | Enforce session tokens on ALL plugin bindings | Fixed | Sprint 24a (#264) |
| F2 | P0 | Content Security Policy on the main webview | Fixed | Sprint 24a (#264) |
| F3 | P0 | Anchor linked-notebook RootPath against post-link tampering | Fixed | Sprint 24b (#TBD) |
| F4 | P1 | Move plugin grants out of vault-scoped config.yaml | Fixed | Sprint 24b (#TBD) |
| F5 | P1 | Reserve first-party plugin IDs at install time | Fixed | Sprint 24b (#TBD) |
| F6 | P1 | Broaden the attachment extension blocklist | Fixed | Sprint 24c (#TBD) |
| F7 | P1 | Restrictive file permissions (0o600/0o700) on config/vault/archive | Fixed | Prior sprint (note writes) + Sprint 24c (#TBD) |
| F8 | P1 | Add govulncheck / npm audit / gitleaks to CI | Fixed | Sprint 24b (#TBD) |
| F9 | P2 | undici advisories (transitive via Vite) — TLS bypass + cache disclosure | Fixed | Sprint 24d (#244, via #277 Vite 8 upgrade) |
| F10 | P2 | esbuild dev-server CSRF — any website can read Vite dev server responses | Fixed | Sprint 24d (#244, via #277 Vite 8 upgrade) |
| F11 | P2 | Remove the user-editable shorthand_regex (parser DoS vector) | Fixed | Sprint 24c (#TBD) |
| F12 | P2 | Cap JSON/YAML decode sizes for user-supplied files | Fixed | Sprint 24c (#TBD) |
| F13 | P2 | Drop User-Agent on cross-host plugin fetch redirects | Fixed | Sprint 24a (#264) |
| F14 | P2 | Use specific targetOrigin for plugin surface postMessage | Fixed | Sprint 24a (#264) |
| F15 | P2 | osv-scanner stdlib advisories against go 1.25.0 floor | Fixed | Sprint 24d (#249) |
| F16 | P2 | golang.org/x/sys v0.42.0 uncalled advisory (GO-2026-5024) | Fixed | Sprint 24d (#249, bumped to v0.46.0) |
| F17 | P2 | go 1.25.0 directive lags installed toolchain (missing patch fixes) | Fixed | Sprint 24d (#249, bumped to go 1.26.4) |
| F18 | P3 | Linux release artifacts unsigned + no SBOM | Fixed | Sprint 24d (#253) |
| F19 | P2 | Restrictive file permissions (0o600/0o700) on plugin data + attachments | Fixed | Sprint 24c (#TBD) |
| F20 | P2 | Add integrity check for settings.json | Fixed | Sprint 24b (#TBD) |
| F21 | P3 | Plugin network audit log brittle space-delimited format | Fixed | Sprint 24d (#254) |
| F22 | P3 | PluginNotify no length cap on title/body | Fixed | Sprint 24d (#255) |
| F23 | P2 | No durable audit trail for grant/revoke + plugin install/uninstall | Fixed | Sprint 24d (#252) |

**Threat models addressed by the closed findings:**

- **M2 (synced-vault adversary):** an attacker who can edit files in a vault
  that is synced between hosts (OneDrive, Dropbox, Syncthing, etc.). Closed by
  F3 (RootPath fingerprint), F4 (host-scoped grants), F5 (first-party ID
  reservation), F20 (settings.json fingerprint), F6 (attachment blocklist),
  F11 (shorthand_regex removal), F12 (decode size caps). F23 (durable audit
  trail) is the forensic complement: a synced-in grant or plugin install now
  leaves a host-side audit record, so post-incident forensics no longer
  require mtime analysis of config.yaml.
- **M3 (co-tenant / malware with user credentials):** an attacker who can write
  to the user's OS-config dir. Partially closed by F20 (settings.json tripwire
  + 0o600 perms) and F7/F19 (restrictive file/dir permissions across config,
  vault, archive, plugin data, and attachments); the hard boundary is
  filesystem permissions on the home dir.
- **Plugin trust boundary:** F1 (session tokens), F2 (CSP), F13 (UA strip),
  F14 (targetOrigin), F8 (CI SCA gate), F22 (notify length cap).
- **Supply-chain integrity:** F9/F10 (Vite/undici/esbuild upgrade), F15–F17
  (Go toolchain + x/sys), F18 (Linux cosign signing + SBOM), F21 (JSON audit
  log format).
