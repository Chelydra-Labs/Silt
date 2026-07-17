package themes

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RelativeLuminance returns the WCAG 2.x relative luminance of a color
// string (0 = black, 1 = white), or ok=false if the value is not a
// recognized color form. It accepts the same color grammar the theme
// validator permits at every token slot: #hex (#rgb / #rrggbb /
// #rrggbbaa), rgb(), and rgba(). The alpha channel of #rrggbbaa is
// dropped because luminance is defined for opaque colors (it composites
// against an unknown backdrop in WCAG's model, so callers should pass
// the opaque token value).
//
// This is the perceptual-math backing for the WCAG contrast assertions
// in contrast_test.go and the authoring guidance in docs/THEMING.md.
// It does no I/O and is safe to call from any goroutine.
func RelativeLuminance(s string) (lum float64, ok bool) {
	r, g, b, ok := parseColorAny(s)
	if !ok {
		return 0, false
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b), true
}

// ContrastRatio returns the WCAG 2.x contrast ratio between two color
// strings (range 1:1 to 21:1), or ok=false if either value is not a
// recognized color form. The ratio is symmetric and independent of
// argument order: (L_lighter + 0.05) / (L_darker + 0.05). Pure white on
// pure black is exactly 21:1.
func ContrastRatio(a, b string) (ratio float64, ok bool) {
	la, oka := RelativeLuminance(a)
	lb, okb := RelativeLuminance(b)
	if !oka || !okb {
		return 0, false
	}
	lighter, darker := la, lb
	if lb > la {
		lighter, darker = lb, la
	}
	return (lighter + 0.05) / (darker + 0.05), true
}

// resolveAccentOn returns the on-accent label ink for a triple: the authored
// On when set, otherwise a black/white pick derived from Start so solid
// accent buttons stay readable without a v1 migration. surfaceBG is the mode's
// app surface (or other CTA backdrop) used when Start is translucent so contrast
// is measured against the painted fill, not the uncomposited RGB channels.
func resolveAccentOn(t AccentTriple, surfaceBG string) string {
	if on := strings.TrimSpace(t.On); on != "" {
		return on
	}
	return DeriveInkOnAccent(effectiveAccentFill(t.Start, surfaceBG))
}

// effectiveAccentFill returns an opaque color representing how start paints
// over surfaceBG. Opaque starts pass through; translucent starts are
// source-over composited so DeriveInkOnAccent matches the rendered CTA fill.
// When Start is translucent and surfaceBG is unparseable, return start so
// DeriveInkOnAccent decides from the authored color (not a dark-biased black
// composite). Authors should set On for solid CTAs when possible.
func effectiveAccentFill(start, surfaceBG string) string {
	r, g, b, a, ok := parseColorRGBA(start)
	if !ok {
		return start
	}
	if a >= 0.999 {
		return start
	}
	sr, sg, sb, sok := parseColorAny(surfaceBG)
	if !sok {
		return start
	}
	// source-over: out = src*α + dst*(1-α)
	or := uint8(float64(r)*a + float64(sr)*(1-a) + 0.5)
	og := uint8(float64(g)*a + float64(sg)*(1-a) + 0.5)
	ob := uint8(float64(b)*a + float64(sb)*(1-a) + 0.5)
	return fmt.Sprintf("#%02x%02x%02x", or, og, ob)
}

// DeriveInkOnAccent picks near-black, pure black, or white label ink for a
// solid fill of start so WCAG AA (4.5:1) is met when possible. Medium accents
// (e.g. cyber_forest light teal #0d9488) need dark ink; pure black is used
// when near-black falls short of 4.5:1 (e.g. indigo #6366f1).
//
// Callers that may receive translucent Start should pass effectiveAccentFill
// first (see resolveAccentOn); ContrastRatio deliberately drops alpha.
func DeriveInkOnAccent(start string) string {
	const (
		nearBlack = "#0a0a0a"
		pureBlack = "#000000"
		white     = "#ffffff"
	)
	type cand struct {
		ink   string
		ratio float64
		ok    bool
	}
	cands := []cand{
		{nearBlack, 0, false},
		{pureBlack, 0, false},
		{white, 0, false},
	}
	for i := range cands {
		cands[i].ratio, cands[i].ok = ContrastRatio(cands[i].ink, start)
	}
	// Prefer any candidate that meets AA text contrast.
	var bestPass *cand
	for i := range cands {
		c := &cands[i]
		if !c.ok || c.ratio < 4.5 {
			continue
		}
		if bestPass == nil || c.ratio > bestPass.ratio {
			bestPass = c
		}
	}
	if bestPass != nil {
		// Prefer near-black over pure black when both pass (softer on bright fills).
		if bestPass.ink == pureBlack {
			for i := range cands {
				if cands[i].ink == nearBlack && cands[i].ok && cands[i].ratio >= 4.5 {
					return nearBlack
				}
			}
		}
		return bestPass.ink
	}
	// No AA candidate: pick the highest ratio (still better than a fixed default).
	best := cands[0]
	for _, c := range cands[1:] {
		if c.ok && (!best.ok || c.ratio > best.ratio) {
			best = c
		}
	}
	if best.ok {
		return best.ink
	}
	return white
}

// linear converts an 8-bit sRGB channel to its linearized value using
// the WCAG 2.x transfer function. The legacy 0.03928 threshold is used
// (the value the WCAG 2.0/2.1 normative text specifies).
func linear(c uint8) float64 {
	cs := float64(c) / 255.0
	if cs <= 0.03928 {
		return cs / 12.92
	}
	return math.Pow((cs+0.055)/1.055, 2.4)
}

// parseColorAny accepts #hex (#rgb / #rrggbb / #rrggbbaa), rgb() / rgba()
// functional notation, and oklch() (resolved to sRGB via the Oklab inverse in
// derivation.go), returning 8-bit sRGB channels. It mirrors isValidColor's
// accepted grammar (validate.go) so any color that passes theme validation can
// be measured for contrast. Non-matching inputs — including NaN/Inf
// components, which strconv.ParseFloat accepts with a nil error — return
// ok=false.
//
// Alpha is dropped (luminance is defined over opaque colors). See parseColorRGBA
// when compositing translucent fills. Pinned by TestContrastRatio_AcceptedColorForms.
func parseColorAny(s string) (r, g, b uint8, ok bool) {
	r, g, b, _, ok = parseColorRGBA(s)
	return r, g, b, ok
}

// parseColorRGBA is parseColorAny plus the alpha channel (1.0 when omitted).
// Used by effectiveAccentFill to composite translucent accent starts.
func parseColorRGBA(s string) (r, g, b uint8, a float64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, 0, 0, false
	}
	if s[0] == '#' {
		r, g, b, a, ok = hexToRGBA(s)
		return r, g, b, a, ok
	}
	// OKLCH resolves through the Oklab inverse to sRGB so contrast math works
	// on OKLCH-authored tokens without a separate code path.
	if strings.HasPrefix(s, "oklch(") && strings.HasSuffix(s, ")") {
		lch, ok := parseOKLCH(s)
		if !ok {
			return 0, 0, 0, 0, false
		}
		r, g, b := oklchToSRGB(lch.L, lch.C, lch.H)
		a := 1.0
		if lch.hasAlpha {
			a = lch.alpha
		}
		return r, g, b, a, true
	}
	inner, wantParts := "", 0
	switch {
	case strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")"):
		inner, wantParts = s[len("rgba("):len(s)-1], 4
	case strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")"):
		inner, wantParts = s[len("rgb("):len(s)-1], 3
	default:
		return 0, 0, 0, 0, false
	}
	parts := strings.Split(inner, ",")
	if len(parts) != wantParts {
		return 0, 0, 0, 0, false
	}
	ch := [3]uint8{}
	for i := 0; i < 3; i++ {
		p := strings.TrimSpace(parts[i])
		percent := strings.HasSuffix(p, "%")
		num := p
		if percent {
			num = p[:len(p)-1]
		}
		v, err := strconv.ParseFloat(num, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, 0, 0, 0, false
		}
		if percent {
			if v < 0 || v > 100 {
				return 0, 0, 0, 0, false
			}
			ch[i] = uint8(v/100*255 + 0.5)
		} else {
			if v < 0 || v > 255 {
				return 0, 0, 0, 0, false
			}
			ch[i] = uint8(v + 0.5)
		}
	}
	a = 1.0
	if wantParts == 4 {
		alphaStr := strings.TrimSpace(parts[3])
		// CSS Color 4 percent alpha (e.g. 50%).
		if strings.HasSuffix(alphaStr, "%") {
			num := alphaStr[:len(alphaStr)-1]
			v, err := strconv.ParseFloat(num, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
				return 0, 0, 0, 0, false
			}
			a = v / 100
		} else {
			alpha, err := strconv.ParseFloat(alphaStr, 64)
			if err != nil || math.IsNaN(alpha) || math.IsInf(alpha, 0) || alpha < 0 || alpha > 1 {
				return 0, 0, 0, 0, false
			}
			a = alpha
		}
	}
	return ch[0], ch[1], ch[2], a, true
}

// hexToRGBA parses #rgb / #rrggbb / #rrggbbaa, returning alpha 1.0 when omitted.
func hexToRGBA(s string) (r, g, b uint8, a float64, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '#' {
		return 0, 0, 0, 0, false
	}
	hex := s[1:]
	var full string
	a = 1.0
	switch len(hex) {
	case 3:
		full = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	case 6:
		full = hex
	case 8:
		full = hex[0:6]
		ai, okA := parseHexByte(hex[6:8])
		if !okA {
			return 0, 0, 0, 0, false
		}
		a = float64(ai) / 255.0
	default:
		return 0, 0, 0, 0, false
	}
	ri, ok1 := parseHexByte(full[0:2])
	gi, ok2 := parseHexByte(full[2:4])
	bi, ok3 := parseHexByte(full[4:6])
	if !ok1 || !ok2 || !ok3 {
		return 0, 0, 0, 0, false
	}
	return ri, gi, bi, a, true
}
