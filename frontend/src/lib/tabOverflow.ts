export interface HorizontalBounds {
  left: number
  right: number
}

export function hiddenTabIds(
  container: HorizontalBounds,
  tabs: readonly { id: string; left: number; right: number }[]
): string[] {
  return tabs
    .filter(
      (tab) => tab.left < container.left - 1 || tab.right > container.right + 1
    )
    .map((tab) => tab.id)
}
