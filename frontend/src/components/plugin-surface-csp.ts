// CSP directives for the plugin surface iframe (#149).
// Shared between PluginSurfaceFrame.svelte (injection) and the test (assertion)
// so the test catches drift instead of asserting on a duplicated string.

export const SURFACE_CSP =
  "default-src 'none'; " +
  "script-src 'unsafe-inline'; " +
  "style-src 'unsafe-inline'; " +
  "connect-src 'none'"

export const SURFACE_CSP_META = `<meta http-equiv="Content-Security-Policy" content="${SURFACE_CSP}">`
