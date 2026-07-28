import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { svelteTesting } from '@testing-library/svelte/vite'

const root = path.dirname(fileURLToPath(import.meta.url))

// Vitest config for the theme engine coverage (#74) and the picker
// component tests (#50). The Svelte plugin compiles .svelte components
// (theme store + AppearanceTab). jsdom provides a DOM. The svelteTesting
// plugin sets the browser export condition so Svelte resolves to its
// client build (the default Node/server build throws
// "mount(...) is not available on the server" under vitest).
export default defineConfig({
  plugins: [svelte(), svelteTesting()],
  resolve: {
    preserveSymlinks: true,
    alias: {
      // Stable path for Wails app bindings so tests mock once regardless of depth (#766).
      '$silt-app': path.resolve(root, 'bindings/silt/app.js')
    }
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    setupFiles: ['./vitest.setup.ts'],
    globals: false
  }
})
