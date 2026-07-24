import { defineConfig, globalIgnores } from 'eslint/config'
import js from '@eslint/js'
import globals from 'globals'
import ts from 'typescript-eslint'
import svelte from 'eslint-plugin-svelte'
import svelteConfig from './svelte.config.js'

// Fast non-type-aware config for local `npm run lint` (<1 min target).
// CI and pre-merge use `npm run lint:typed` (eslint.config.js + projectService).
export default defineConfig(
  globalIgnores([
    'bindings/**',
    'dist/**',
    'coverage/**',
    'node_modules/**',
    'wailsjs/**'
  ]),

  js.configs.recommended,
  ...ts.configs.recommended,
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
