// culori@4 ships without TypeScript declarations; this ambient module
// declares only the tree-shakeable `culori/fn` surface that color.ts uses
// (OKLCH + RGB + WCAG). See https://culorijs.org/api/ for the full runtime
// contract — only the pieces consumed here are typed.

declare module 'culori/fn' {
  export interface RgbColor {
    mode: 'rgb'
    r: number
    g: number
    b: number
    alpha?: number
  }

  // Named `OklchModeColor` to avoid clashing with color.ts's uppercase-L/C/H
  // public `Oklch` shape; culori uses lowercase l/c/h internally.
  export interface OklchModeColor {
    mode: 'oklch'
    l: number
    c: number
    h: number
    alpha?: number
  }

  export type Color = RgbColor | OklchModeColor
  export type ModeDefinition = unknown
  export type Converter = (color: string | Color) => Color | undefined

  export function useMode(definition: ModeDefinition): Converter
  export const modeRgb: ModeDefinition
  export const modeOklch: ModeDefinition
  export const modeLrgb: ModeDefinition

  export function formatHex(color: Color): string
  export function parse(color: string): Color | undefined
  export function wcagContrast(a: string | Color, b: string | Color): number
}
