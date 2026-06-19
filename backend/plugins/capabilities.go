package plugins

import (
	"fmt"
	"sort"
	"strings"
)

// This file defines the v2 SDK capability & permission model (#113). A
// capability is a privileged operation a plugin may request (file I/O, network,
// OS integration, editor-schema mutation, rendered-UI surfaces). A grant is the
// user's affirmative, per-vault permission for a specific plugin to use one.
//
// Enforcement is server-side: every privileged App binding calls requireGrant
// before doing its work and returns a structured CapabilityDeniedError on
// denial (never a panic). The frontend SDK methods are thin pass-throughs —
// there is exactly one source of truth for grants (config.yaml plugins.grants).
//
// "exec" is intentionally absent from the v2 set. It is deferred until the
// trust/signing model matures (#111), so a manifest declaring "exec" is
// rejected at install time rather than silently ignored.

// Capability is the identifier of a privileged operation class.
type Capability string

const (
	// CapReadFiles — read non-markdown files within a notebook (attachments,
	// assets). Gated by the read-files capability.
	CapReadFiles Capability = "read-files"
	// CapWriteFiles — write/delete files within a notebook + plugin scratch
	// space. Scoped by a qualifier (notebook | vault).
	CapWriteFiles Capability = "write-files"
	// CapNetwork — HTTP fetch through the Go-side proxy. Whole-scope.
	CapNetwork Capability = "network"
	// CapOSOpen — open a file/URL in the OS native handler. Whole-scope
	// (URLs are scheme-allowlisted independently).
	CapOSOpen Capability = "os-open"
	// CapOSClipboard — read/write the system clipboard (text only).
	CapOSClipboard Capability = "os-clipboard"
	// CapOSNotify — show a desktop notification.
	CapOSNotify Capability = "os-notify"
	// CapUISurface — render a third-party UI surface (sidebar panel, modal,
	// status-bar item, settings panel) in the sandboxed iframe renderer.
	CapUISurface Capability = "ui-surface"
	// CapEditorSchema — contribute slash commands, custom embed-block views,
	// and decorations to the TipTap editor.
	CapEditorSchema Capability = "editor-schema"
)

// KnownCapabilities is the set of capabilities recognized by this version of
// Silt. A manifest declaring an unknown capability is rejected at install so a
// plugin cannot silently claim a capability the host does not understand (nor
// enlarge its rights via typos or future names). "exec" is intentionally
// absent — deferred until signing/trust lands (#111).
var KnownCapabilities = map[Capability]bool{
	CapReadFiles:    true,
	CapWriteFiles:   true,
	CapNetwork:      true,
	CapOSOpen:       true,
	CapOSClipboard:  true,
	CapOSNotify:     true,
	CapUISurface:    true,
	CapEditorSchema: true,
}

// Qualifier refines a capability grant's scope. The default/whole-scope
// qualifier is QualGranted. File-write capabilities may be narrowed to the
// active/declared notebook (QualNotebook) or the whole vault (QualVault).
const (
	QualGranted  = "granted"
	QualNotebook = "notebook"
	QualVault    = "vault"
)

// validQualifiers is the set of accepted scope qualifier strings.
var validQualifiers = map[string]bool{
	QualGranted:  true,
	QualNotebook: true,
	QualVault:    true,
}

// qualifierScopeFor reports which qualifiers are meaningful for a capability.
// File-write ops honor notebook/vault narrowing; everything else uses
// QualGranted (a non-default qualifier is accepted but has no extra effect, so
// a manifest can be uniform). Unknown capabilities are rejected upstream.
func qualifierScopeFor(c Capability) string {
	switch c {
	case CapWriteFiles, CapReadFiles:
		return "granted|notebook|vault"
	default:
		return "granted"
	}
}

// NormalizeCapabilities converts a raw manifest capabilities declaration
// (values may be the bool true, a string qualifier, or null/absent) into a
// normalized capability→qualifier map. It rejects unknown capabilities and
// malformed values so an install never silently grants a right the host does
// not understand. An empty/nil input yields an empty (non-nil) map.
func NormalizeCapabilities(raw map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		cap := Capability(k)
		if !KnownCapabilities[cap] {
			return nil, fmt.Errorf("unknown capability %q (recognized: %s)", k, ListCapabilities())
		}
		qual, err := normalizeQualifier(v)
		if err != nil {
			return nil, fmt.Errorf("capability %q: %w", k, err)
		}
		out[k] = qual
	}
	return out, nil
}

// normalizeQualifier interprets a single capability value. `true` (or null)
// means "granted at default scope"; a string must be a recognized qualifier.
func normalizeQualifier(v any) (string, error) {
	switch x := v.(type) {
	case bool:
		if x {
			return QualGranted, nil
		}
		return "", fmt.Errorf("capability value false is not meaningful (omit the capability instead)")
	case nil:
		return QualGranted, nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return QualGranted, nil
		}
		if !validQualifiers[s] {
			return "", fmt.Errorf("invalid scope %q (expected %s)", s, qualifierScopeFor(Capability(s)))
		}
		// The string itself isn't the capability here; we only validate it is a
		// known qualifier. The caller already bound it to a capability key.
		return s, nil
	default:
		return "", fmt.Errorf("capability value must be true or a scope string (got %T)", v)
	}
}

// ListCapabilities returns a sorted, comma-separated list of recognized
// capability ids, for error messages.
func ListCapabilities() string {
	caps := make([]string, 0, len(KnownCapabilities))
	for c := range KnownCapabilities {
		caps = append(caps, string(c))
	}
	sort.Strings(caps)
	return strings.Join(caps, ", ")
}

// CapabilityDeniedError is the structured error returned by requireGrant when a
// plugin attempts a privileged operation it has not been granted. It is
// JSON-serializable so the frontend SDK can surface a specific, actionable
// message (and a re-prompt) rather than a generic string.
type CapabilityDeniedError struct {
	Plugin     string `json:"plugin"`
	Capability string `json:"capability"`
	Requested  string `json:"requested"` // qualifier the plugin asked for ("" if N/A)
	Granted    string `json:"granted"`   // qualifier currently granted ("" if none)
}

func (e *CapabilityDeniedError) Error() string {
	if e.Granted == "" {
		return fmt.Sprintf("plugin %q has not been granted %q", e.Plugin, e.Capability)
	}
	return fmt.Sprintf("plugin %q needs %q scope for %q but is granted %q", e.Plugin, e.Requested, e.Capability, e.Granted)
}
