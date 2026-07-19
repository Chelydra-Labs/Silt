<script lang="ts">
  import { OpenDevTools } from '../../../bindings/silt/app.js'
  import { pushNotification } from '../../notifications/store.svelte'

  async function openDevTools(): Promise<void> {
    try {
      await OpenDevTools()
    } catch (e) {
      console.error('OpenDevTools failed:', e)
      pushNotification({
        kind: 'error',
        message: 'Could not open DevTools. Is Dev Mode enabled?'
      })
    }
  }
</script>

<div class="p-6 max-w-4xl mx-auto w-full space-y-6">
  <div
    class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
  >
    <h4
      class="font-label-sm-bold text-text-primary uppercase tracking-wider text-type-2xs"
    >
      Developer Tools
    </h4>

    <div class="space-y-3">
      <p class="text-text-muted text-type-sm font-body-md leading-relaxed">
        Dev Mode is enabled — the Dev tab appears in the settings sidebar.
        Disable it on the <strong>About</strong> page.
      </p>

      <div
        class="bg-surface-panel/30 border border-surface-panel-border rounded-lg p-4 space-y-3"
      >
        <p class="text-text-primary text-type-md font-body-md font-semibold">
          Chromium DevTools
        </p>
        <p class="text-text-muted text-type-xs font-body-md leading-relaxed">
          Inspect the DOM, view console errors, and debug rendering issues.
        </p>
        <ul
          class="text-text-muted text-type-xs font-body-md leading-relaxed list-disc pl-5 space-y-1"
        >
          <li>
            Press <kbd
              class="inline-block px-1.5 py-0.5 rounded bg-surface-panel border border-surface-panel-border text-text-primary text-type-2xs font-mono"
              >Ctrl+Shift+F12</kbd
            > (View → Open Developer Tools)
          </li>
          <li>
            Right-click the editor, sidebar, tabs, empty content, or Tasks
            manage menu and choose <strong>Inspect</strong>
          </li>
        </ul>
        <button
          type="button"
          class="px-3 py-1.5 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm font-body-md hover:bg-surface-panel/80"
          onclick={openDevTools}
        >
          Open DevTools
        </button>
        <p class="text-text-muted text-type-2xs font-body-md leading-relaxed">
          View → Open Developer Tools tracks Dev Mode live (enabled when on,
          disabled when off; <code>SILT_DEBUG=1</code> keeps it enabled). If Dev
          Mode was off when the app launched, restart after enabling it so the
          webview is created with DevTools support — runtime Inspect and the
          shortcut still need a window created with DevTools enabled (or
          <code>SILT_DEBUG=1</code>). Production builds may need a
          DevTools-enabled build for the inspector to appear.
        </p>
      </div>

      <div
        class="bg-surface-panel/30 border border-surface-panel-border rounded-lg p-4 space-y-3"
      >
        <p class="text-text-primary text-type-md font-body-md font-semibold">
          State Debug Badge
        </p>
        <p class="text-text-muted text-type-xs font-body-md leading-relaxed">
          A red badge appears at the bottom of the content area showing the
          current navigation state (<code>view</code>, <code>nb</code>,
          <code>pg</code>,
          <code>tab</code>, <code>dt</code>, <code>nr</code>) to help diagnose
          blank-page issues.
        </p>
      </div>
    </div>
  </div>
</div>
