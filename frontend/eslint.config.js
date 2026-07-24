import { defineConfig, globalIgnores } from 'eslint/config'
import js from '@eslint/js'
import globals from 'globals'
import ts from 'typescript-eslint'
import svelte from 'eslint-plugin-svelte'
import svelteConfig from './svelte.config.js'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const tsconfigRootDir = path.dirname(fileURLToPath(import.meta.url))

// Shared across every projectService block. Mismatched extraFileExtensions
// between .ts and .svelte files forces a full TypeScript project reload per
// file (typescript-eslint typed-linting performance docs) and multiplies
// wall-clock cost.
const extraFileExtensions = ['.svelte']

// Type-aware config (CI / `npm run lint:typed`). Authored frontend source only.
export default defineConfig(
  globalIgnores([
    'bindings/**',
    'dist/**',
    'coverage/**',
    'node_modules/**',
    'wailsjs/**'
  ]),

  js.configs.recommended,
  ...ts.configs.recommendedTypeChecked,
  svelte.configs.recommended,

  {
    languageOptions: {
      globals: {
        ...globals.browser
      },
      parserOptions: {
        projectService: true,
        tsconfigRootDir,
        extraFileExtensions
      }
    }
  },

  {
    files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir,
        extraFileExtensions,
        parser: ts.parser,
        // svelteConfig is non-serializable; typed lint cache may be weaker
        // than the fast config. Prefer lint:typed in CI, lint (fast) locally.
        svelteConfig
      }
    }
  },

  {
    rules: {
      // Underscore-prefixed names are intentional discards.
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
          destructuredArrayIgnorePattern: '^_'
        }
      ],
      // Staged only: Wails-generated bindings and many IPC payloads are typed
      // as `any`/`unknown` today. Enabling no-unsafe-* floods the gate with
      // binding-boundary noise rather than authored-logic bugs. Re-enable when
      // binding d.ts / plugin IPC types are tightened (follow-up to #723).
      '@typescript-eslint/no-unsafe-assignment': 'off',
      '@typescript-eslint/no-unsafe-member-access': 'off',
      '@typescript-eslint/no-unsafe-call': 'off',
      '@typescript-eslint/no-unsafe-return': 'off',
      '@typescript-eslint/no-unsafe-argument': 'off'
    }
  },

  // Vitest: async mock factories often return resolved values without await;
  // expect(fn) passes unbound methods by design.
  {
    files: [
      '**/*.{test,spec}.{ts,js}',
      '**/test-helpers.ts',
      '**/*.stub.svelte',
      '**/__test_helpers__/**'
    ],
    rules: {
      '@typescript-eslint/require-await': 'off',
      '@typescript-eslint/unbound-method': 'off'
    }
  }
)
