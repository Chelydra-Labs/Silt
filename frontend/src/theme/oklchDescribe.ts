// Human-readable OKLCH descriptions for aria-valuetext (#528).
// Pattern inspired by React Aria accessible color descriptions:
// "{lightness} {chroma} {hue}" plus precise channel numbers.

import type { Oklch } from './color'

const HUE_SEGMENTS: { max: number; name: string }[] = [
  { max: 32, name: 'red' },
  { max: 71, name: 'orange' },
  { max: 115, name: 'yellow' },
  { max: 155, name: 'green' },
  { max: 220, name: 'cyan' },
  { max: 274, name: 'blue' },
  { max: 302, name: 'purple' },
  { max: 334, name: 'magenta' },
  { max: 360, name: 'pink' }
]

function hueName(H: number): string {
  const h = ((H % 360) + 360) % 360
  if (h < 2 || h >= 334) {
    // pink / red boundary
    if (h >= 334 || h < 2) return h < 2 ? 'red' : 'pink'
  }
  for (const seg of HUE_SEGMENTS) {
    if (h < seg.max) return seg.name
  }
  return 'red'
}

function lightnessWord(L: number): string {
  if (L < 0.15) return 'very dark'
  if (L < 0.35) return 'dark'
  if (L < 0.65) return ''
  if (L < 0.85) return 'light'
  return 'very light'
}

function chromaWord(C: number): string {
  if (C < 0.02) return 'gray'
  if (C < 0.06) return 'grayish'
  if (C < 0.1) return 'pale'
  if (C < 0.2) return ''
  return 'vibrant'
}

/** Short perceptual phrase, e.g. "light pale blue". */
export function describeOklchPhrase(lch: Oklch): string {
  if (lch.C < 0.02) {
    const lw = lightnessWord(lch.L)
    if (lch.L < 0.08) return 'black'
    if (lch.L > 0.95) return 'white'
    return [lw, 'gray'].filter(Boolean).join(' ')
  }
  const parts = [
    lightnessWord(lch.L),
    chromaWord(lch.C),
    hueName(lch.H)
  ].filter(Boolean)
  return parts.join(' ') || hueName(lch.H)
}

/** Full valuetext for sliders / plane: phrase + channel numbers. */
export function describeOklch(lch: Oklch): string {
  const phrase = describeOklchPhrase(lch)
  const Lpct = (lch.L * 100).toFixed(1)
  return `${phrase}, ${Lpct} percent lightness, chroma ${lch.C.toFixed(3)}, hue ${lch.H.toFixed(0)} degrees`
}
