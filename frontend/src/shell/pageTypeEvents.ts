// Window-event contract between the /type slash command (dispatched from the
// editor's slash menu) and the shell's page-type controller wiring in App.
// Extracted as a shared symbol so renaming the literal on one side can't
// silently break the bridge — both ends import this constant.

export const ASSIGN_PAGE_TYPE_EVENT = 'silt:assign-page-type'
