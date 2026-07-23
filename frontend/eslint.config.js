import { defineConfig, globalIgnores } from 'eslint/config'
import js from '@eslint/js'
import globals from 'globals'
import ts from 'typescript-eslint'
import svelte from 'eslint-plugin-svelte'
import svelteConfig from './svelte.config.js'

// Authored frontend source only. Bindings, dist, coverage, and dependencies
// are regenerated or third-party and must not be linted.
export default defineConfig(
  globalIgnores([
    'bindings/**',
    'dist/**',
    'coverage/**',
    'node_modules/**',
    'wailsjs/**'
  ]),

  js.configs.recommended,
  ts.configs.recommended,
  svelte.configs.recommended,

  {
    languageOptions: {
      globals: {
        ...globals.browser
      }
    }
  },

  {
    files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
    languageOptions: {
      parserOptions: {
        parser: ts.parser,
        svelteConfig
      }
    }
  },

  // Underscore-prefixed names are intentional discards (callback params, destructure).
  {
    rules: {
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
          destructuredArrayIgnorePattern: '^_'
        }
      ]
    }
  }
)
