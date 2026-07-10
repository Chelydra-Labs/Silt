import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
// Vitest config lives in vitest.config.ts (which takes precedence over a
// test key here). See vitest.config.ts for the test environment, setup
// files, and the svelteTesting plugin.
export default defineConfig({
  plugins: [
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
    preserveSymlinks: true
  }
})
