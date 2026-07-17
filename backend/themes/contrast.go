package themes

import (
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
// accent buttons stay readable without a v1 migration.
func resolveAccentOn(t AccentTriple) string {
	if on := strings.TrimSpace(t.On); on != "" {
		return on
	}
	return DeriveInkOnAccent(t.Start)
}

// DeriveInkOnAccent picks near-black, pure black, or white label ink for a
// solid fill of start so WCAG AA (4.5:1) is met when possible. Medium accents
// (e.g. cyber_forest light teal #0d9488) need dark ink; pure black is used
// when near-black falls short of 4.5:1 (e.g. indigo #6366f1).
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
// Asymmetry with isValidColor: only the 3 RGB components are parsed from
// rgb()/rgba() (the loop below reads parts[0..2]); the alpha channel is never
// read, so it is dropped rather than validated. Therefore a malformed alpha
// (e.g. rgba(12,12,14,NaN)) is ACCEPTED here even though isValidColor rejects
// it. This is deliberate — luminance is defined over opaque colors — and is
// pinned by TestContrastRatio_AcceptedColorForms. Do not add an alpha guard
// here without updating that contract.
func parseColorAny(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, 0, false
	}
	if s[0] == '#' {
		r, g, b, ok = HexToRGB(s)
		return r, g, b, ok
	}
	// OKLCH resolves through the Oklab inverse to sRGB so contrast math works
	// on OKLCH-authored tokens without a separate code path.
	if strings.HasPrefix(s, "oklch(") && strings.HasSuffix(s, ")") {
		lch, ok := parseOKLCH(s)
		if !ok {
			return 0, 0, 0, false
		}
		r, g, b := oklchToSRGB(lch.L, lch.C, lch.H)
		return r, g, b, true
	}
	inner, wantParts := "", 0
	switch {
	case strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")"):
		inner, wantParts = s[len("rgba("):len(s)-1], 4
	case strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")"):
		inner, wantParts = s[len("rgb("):len(s)-1], 3
	default:
		return 0, 0, 0, false
	}
	parts := strings.Split(inner, ",")
	if len(parts) != wantParts {
		return 0, 0, 0, false
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
			return 0, 0, 0, false
		}
		if percent {
			if v < 0 || v > 100 {
				return 0, 0, 0, false
			}
			ch[i] = uint8(v/100*255 + 0.5)
		} else {
			if v < 0 || v > 255 {
				return 0, 0, 0, false
			}
			ch[i] = uint8(v + 0.5)
		}
	}
	if wantParts == 4 {
		alphaStr := strings.TrimSpace(parts[3])
		alpha, err := strconv.ParseFloat(alphaStr, 64)
		if err != nil || alpha < 0 || alpha > 1 {
			return 0, 0, 0, false
		}
	}
	return ch[0], ch[1], ch[2], true
}
