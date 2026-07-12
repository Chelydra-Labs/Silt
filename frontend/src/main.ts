import './index.css'
import { mount } from 'svelte'
import App from './App.svelte'
import { initTheme } from './theme/store.svelte'

// Start the theme engine as early as possible: it fetches the active theme
// over IPC and injects it onto :root with a same-tick repaint, overriding
// the index.css :root startup fallbacks. Not awaited so the shell renders
// immediately from the fallbacks; the injector repaints the moment IPC
// returns. Dispose on HMR so theme:changed listeners do not stack (#534).
let disposeTheme: (() => void) | undefined
void initTheme().then((unsub) => {
  disposeTheme = unsub
})

if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    disposeTheme?.()
    disposeTheme = undefined
  })
}

const app = mount(App, {
  target: document.getElementById('app')!
})

export default app
