package themes

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// This file implements OKLCH color support for Theme System v2 (RFC §4).
//
// OKLCH is the perceptually-uniform color space Silt uses for color storage
// and derivation. Hex remains accepted (backward compatible); both formats
// flatten verbatim. Derivation of hover/active/disabled variants is eager and
// deterministic so the CI contrast gate and the editor see the same values.
//
// The Oklab forward/inverse math is ported from Björn Ottosson's reference
// (https://bottosson.github.io/posts/oklab/). sRGB↔linear-sRGB uses the
// standard IEC 61966-2-1 transfer function.

// oklab represents a color in the Oklab perceptual space (L lightness 0–1,
// a/b green-red / blue-yellow opponent axes). Alpha is carried separately so
// the format can round-trip.
type oklab struct {
	L, a, b, alpha float64
}

// oklchText is the CSS oklch() textual form: L (0–1), C (≥0), H (degrees).
type oklchText struct {
	L, C, H, alpha float64
	hasAlpha       bool
}

// ToSRGB resolves any accepted theme color (hex, rgb(), rgba(), oklch()) to
// 8-bit sRGB channels. It is the single color→sRGB path used by the contrast
// math (contrast.go) and the launch BackgroundColour resolver. Alpha is
// dropped: WCAG luminance and the opaque webview background are defined over
// opaque colors. Unrecognized inputs return ok=false.
func ToSRGB(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, 0, false
	}
	if s[0] == '#' {
		return HexToRGB(s)
	}
	if strings.HasPrefix(s, "oklch(") && strings.HasSuffix(s, ")") {
		lch, ok := parseOKLCH(s)
		if !ok {
			return 0, 0, 0, false
		}
		r, g, b := oklchToSRGB(lch.L, lch.C, lch.H)
		return r, g, b, true
	}
	// hex / rgb() / rgba() resolve through the shared functional+hex parser.
	return parseColorAny(s)
}

// parseOKLCH parses "oklch(L C H)" or "oklch(L C H / A)" into components.
// L ∈ [0,1] (percent form L% also accepted), C ≥ 0, H in degrees (wraps).
// NaN/Inf components are rejected (Go's ParseFloat accepts them with nil err).
func parseOKLCH(s string) (oklchText, bool) {
	inner := s[len("oklch(") : len(s)-1]
	var lch oklchText
	// Optional alpha after a slash.
	alphaStr := ""
	if idx := strings.Index(inner, "/"); idx >= 0 {
		alphaStr = strings.TrimSpace(inner[idx+1:])
		inner = strings.TrimSpace(inner[:idx])
		lch.hasAlpha = true
	}
	// Comma-separated form (oklch(L, C, H)) is accepted alongside the
	// space-separated canonical form; strings.Fields would leave trailing
	// commas attached to each component and silently fail.
	var parts []string
	if strings.Contains(inner, ",") {
		parts = strings.Split(inner, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
	} else {
		parts = strings.Fields(inner)
	}
	if len(parts) != 3 {
		return lch, false
	}
	L, okL := parseComponent(parts[0], 1.0, 0, 1) // L is 0–1 (or 0–100%)
	C, okC := parseComponent(parts[1], 1.0, 0, math.Inf(1))
	H, okH := parseAngle(parts[2])
	if !okL || !okC || !okH {
		return lch, false
	}
	lch.L, lch.C, lch.H = L, C, H
	if lch.hasAlpha {
		a, okA := parseComponent(alphaStr, 1.0, 0, 1)
		if !okA {
			return lch, false
		}
		lch.alpha = a
	}
	return lch, true
}

// parseComponent parses a numeric component that may be a bare number or a
// percentage relative to `scale`. NaN/Inf rejected.
func parseComponent(s string, scale, lo, hi float64) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasSuffix(s, "%") {
		num := s[:len(s)-1]
		v, err := strconv.ParseFloat(num, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		v = v / 100.0 * scale
		return clamp(v, lo, hi), true
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return clamp(v, lo, hi), true
}

// parseAngle parses a hue in degrees (unitless or with a `deg` suffix). Hue
// wraps, so any finite value is accepted and normalized to [0,360).
func parseAngle(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "deg") {
		s = s[:len(s)-3]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	v = math.Mod(v, 360)
	if v < 0 {
		v += 360
	}
	return v, true
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// oklchToSRGB converts OKLCH to 8-bit sRGB via OKLab inverse → linear sRGB →
// sRGB gamma. Out-of-gamut values are clamped to [0,1] before quantization.
func oklchToSRGB(L, C, H float64) (uint8, uint8, uint8) {
	rad := H * math.Pi / 180.0
	return oklabToSRGB(oklab{L: L, a: C * math.Cos(rad), b: C * math.Sin(rad)})
}

// oklabToSRGB applies the OKLab→linear-sRGB matrix, then sRGB gamma encoding,
// then quantizes to 8 bits.
func oklabToSRGB(lab oklab) (uint8, uint8, uint8) {
	l_ := lab.L + 0.3963377774*lab.a + 0.2158037573*lab.b
	m_ := lab.L - 0.1055613458*lab.a - 0.0638541728*lab.b
	s_ := lab.L - 0.0894841775*lab.a - 1.2914855480*lab.b
	l := l_ * l_ * l_
	mm := m_ * m_ * m_
	ss := s_ * s_ * s_
	r := 4.0767416621*l - 3.3077115913*mm + 0.2309699292*ss
	g := -1.2684380046*l + 2.6097574011*mm - 0.3413193965*ss
	b := -0.0041960863*l - 0.7034186147*mm + 1.7076147010*ss
	return to8(r), to8(g), to8(b)
}

// sRGBToOklab converts 8-bit sRGB to Oklab via sRGB→linear→OKLab.
func sRGBToOklab(r, g, b uint8) oklab {
	rl := srgbToLinear(r)
	gl := srgbToLinear(g)
	bl := srgbToLinear(b)
	l := 0.4122214708*rl + 0.5363325363*gl + 0.0514459929*bl
	m := 0.2119034982*rl + 0.6806995451*gl + 0.1073969566*bl
	s := 0.0883024619*rl + 0.2817188376*gl + 0.6299787005*bl
	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)
	return oklab{
		L: 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_,
		a: 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_,
		b: 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_,
	}
}

// srgbToLinear is the IEC 61966-2-1 sRGB→linear transfer function.
func srgbToLinear(c uint8) float64 {
	cs := float64(c) / 255.0
	if cs <= 0.04045 {
		return cs / 12.92
	}
	return math.Pow((cs+0.055)/1.055, 2.4)
}

// linearToSRGB is the inverse sRGB gamma encoding.
func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// to8 clamps a linear-sRGB value to [0,1], gamma-encodes, and quantizes.
func to8(c float64) uint8 {
	if c < 0 {
		c = 0
	} else if c > 1 {
		c = 1
	}
	return uint8(linearToSRGB(c)*255 + 0.5)
}

// hexToOklab parses a hex color into Oklab.
func hexToOklab(s string) (oklab, bool) {
	r, g, b, ok := HexToRGB(s)
	if !ok {
		return oklab{}, false
	}
	return sRGBToOklab(r, g, b), true
}

// formatHex renders an Oklab color as #rrggbb.
func formatHex(lab oklab) string {
	r, g, b := oklabToSRGB(lab)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// formatOKLCH renders an Oklab color as oklch(L C H) with L/C to 4 decimals and
// H to 2 decimals (enough precision to round-trip perceptually).
func formatOKLCH(lab oklab) string {
	C := math.Sqrt(lab.a*lab.a + lab.b*lab.b)
	H := math.Atan2(lab.b, lab.a) * 180.0 / math.Pi
	if H < 0 {
		H += 360
	}
	return fmt.Sprintf("oklch(%.4f %.4f %.2f)", lab.L, C, H)
}

// DeriveHover returns a perceptibly-lighter/brighter variant of seed via an
// OKLCH lightness+chroma shift. The output is in the seed's authored format
// (hex stays hex; oklch stays oklch) so Flatten emits what the author wrote.
func DeriveHover(seed string) string {
	return derive(seed, hoverShift)
}

// DeriveActive returns a perceptibly-deeper variant of seed.
func DeriveActive(seed string) string {
	return derive(seed, activeShift)
}

// DeriveDisabled returns a desaturated variant of seed.
func DeriveDisabled(seed string) string {
	return derive(seed, disabledShift)
}

// derive applies a shift function to a seed color, preserving its format.
// Hex seeds → hex output; oklch seeds → oklch output. rgb()/rgba() seeds are
// normalized to hex (they carry no format preference and hex is the canonical
// opaque form). Unparseable seeds round-trip unchanged (fail loud at the call
// site, never silently produce a wrong color).
func derive(seed string, shift func(oklab) oklab) string {
	s := strings.TrimSpace(seed)
	if s == "" {
		return seed
	}
	isOKLCH := strings.HasPrefix(s, "oklch(")
	if isOKLCH {
		lch, ok := parseOKLCH(s)
		if !ok {
			return seed
		}
		rad := lch.H * math.Pi / 180.0
		out := shift(oklab{L: lch.L, a: lch.C * math.Cos(rad), b: lch.C * math.Sin(rad)})
		return formatOKLCH(out)
	}
	lab, ok := hexToOklab(s)
	if !ok {
		// rgb()/rgba(): resolve to sRGB then treat as hex.
		r, g, b, ok := ToSRGB(s)
		if !ok {
			return seed
		}
		lab = sRGBToOklab(r, g, b)
	}
	return formatHex(shift(lab))
}

// hoverShift raises lightness and slightly raises chroma for a perceptibly
// brighter hover state.
func hoverShift(lab oklab) oklab {
	return oklab{L: clamp(lab.L+0.06, 0, 1), a: lab.a * 1.04, b: lab.b * 1.04}
}

// activeShift lowers lightness for a perceptibly deeper pressed state.
func activeShift(lab oklab) oklab {
	return oklab{L: clamp(lab.L-0.04, 0, 1), a: lab.a, b: lab.b}
}

// disabledShift desaturates by dropping chroma (the a/b opponents) toward grey.
func disabledShift(lab oklab) oklab {
	return oklab{L: lab.L, a: lab.a * 0.4, b: lab.b * 0.4}
}
