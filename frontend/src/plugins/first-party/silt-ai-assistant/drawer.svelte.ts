// Open/close state for Writing Assistant right drawer.

export const writingAssistantDrawer = $state({
  open: false
})

export function openWritingAssistantDrawer(): void {
  writingAssistantDrawer.open = true
}

export function closeWritingAssistantDrawer(): void {
  writingAssistantDrawer.open = false
}

export function toggleWritingAssistantDrawer(): void {
  writingAssistantDrawer.open = !writingAssistantDrawer.open
}

export function resetWritingAssistantDrawer(): void {
  writingAssistantDrawer.open = false
}
