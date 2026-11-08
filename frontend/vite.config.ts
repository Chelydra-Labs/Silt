import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, type Plugin } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { transformAppBindingsForHarness } from './src/lib/testing/auditHarness'

const root = path.dirname(fileURLToPath(import.meta.url))

function siltAuditHarnessPlugin(): Plugin {
  const harnessModulePath = path
    .resolve(root, 'src/lib/testing/auditHarness.ts')
    .replace(/\\/g, '/')
  return {
    name: 'silt-audit-harness',
    apply: 'serve',
    transform(code, id) {
      const cleanId = id.split('?')[0].replace(/\\/g, '/')
      if (!cleanId.endsWith('/bindings/silt/app.js')) return null
      return {
        code: transformAppBindingsForHarness(code, harnessModulePath),
        map: null
      }
    }
  }
}

// https://vite.dev/config/
// Vitest config lives in vitest.config.ts (which takes precedence over a
// test key here). See vitest.config.ts for the test environment, setup
// files, and the svelteTesting plugin.
export default defineConfig({
  plugins: [
    siltAuditHarnessPlugin(),
    tailwindcss(),
    svelte()
  ],
  server: {
    // Pin the IPv4 loopback. On Windows, Vite's default `localhost` binds
    // IPv6 (::1) while the Wails v3 dev proxy dials 127.0.0.1 (IPv4), and the
    // mismatch makes asset proxying fail with "connectex: ... actively refused".
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true
  },
  resolve: {
    preserveSymlinks: true,
    alias: {
      // Same as vitest.config.ts — tests are the main consumer (#766).
      '$silt-app': path.resolve(root, 'bindings/silt/app.js')
    }
  }
})
