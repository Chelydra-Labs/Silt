# Build Directory

Houses platform-specific build assets (icons, manifests, installer
definitions) consumed by the Wails v3 toolchain.

The build flow is driven by **`Taskfile.yml`** (root) and the per-platform
`Taskfile.yml` files under `build/<os>/`. Build configuration (app identity,
version, signing) lives in **`build/config.yml`**. Run `wails3 build` /
`wails3 dev` via the task targets rather than invoking the legacy `wails`
command directly.
