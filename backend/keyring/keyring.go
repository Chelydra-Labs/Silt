// Package keyring wraps the OS credential store (Windows Credential Manager,
// Linux Secret Service / D-Bus, macOS Keychain) behind a small interface so the
// AI provider bindings (#218) can store API keys off plaintext config.yaml, and
// so tests inject an in-memory fake instead of touching the real OS keyring.
//
// The package depends on github.com/zalando/go-keyring for the real backends.
// go-keyring returns its own ErrNotFound when a secret is absent, and a dbus /
// platform error when the keyring is unavailable (headless Linux, locked GNOME
// session, WSL2 without a keyring agent). Those platform errors are normalized
// to ErrUnavailable here so callers can fall back to config.yaml with a visible
// warning rather than failing the whole AI subsystem.
package keyring

import (
	"errors"

	gokeyring "github.com/zalando/go-keyring"
)

// ErrNotFound means the requested secret is not present in the keyring (a
// normal "no key stored yet" condition, not an error to surface loudly).
var ErrNotFound = errors.New("keyring: secret not found")

// ErrUnavailable means the OS keyring could not be reached (no D-Bus / Secret
// Service on a headless or WSL2 Linux box, locked session, etc.). Callers fall
// back to config.yaml and surface a one-time warning rather than failing.
var ErrUnavailable = errors.New("keyring: no keyring available on this system")

// Store is the credential-store contract. Production uses Default(); tests use
// NewFake().
type Store interface {
	// Available reports whether the backing credential store is reachable right
	// now. Availability can change while the process is running (locked session,
	// dropped D-Bus), so UI state must not infer this from Store being non-nil.
	Available() bool
	// Set stores (or overwrites) a secret for the service+user pair.
	Set(service, user, secret string) error
	// Get retrieves a secret. Returns ErrNotFound when none is stored and
	// ErrUnavailable when the keyring itself cannot be reached.
	Get(service, user string) (string, error)
	// Delete removes a secret. Returns ErrNotFound when none was stored;
	// ErrUnavailable when the keyring cannot be reached. Deleting a missing
	// secret is conventionally idempotent at the call site, so callers usually
	// ignore ErrNotFound.
	Delete(service, user string) error
}

// Default returns the OS-backed store. The store is always non-nil; whether the
// OS keyring is actually reachable is discovered at call time (Get/Set/Delete
// return ErrUnavailable when it is not), because availability can change across
// the process lifetime (a session can lock, a D-Bus can drop).
func Default() Store {
	return osStore{}
}

// Available reports whether the OS keyring is reachable right now. Used by the
// AI Provider settings page to decide whether to offer the "store in OS keyring"
// toggle. The probe is a Get on a throwaway key: a nil or ErrNotFound answer
// proves the keyring answered (so it is available); any other error means it is
// not (no D-Bus / Secret Service, locked session, WSL2 without an agent).
func Available() bool {
	_, err := gokeyring.Get(probeService, probeUser)
	return isPlatformAvailable(err)
}

// probeService/User are a throwaway service+user pair used only to ping the
// keyring. The pair is never Set, so a reachable keyring answers ErrNotFound.
const (
	probeService = "Silt"
	probeUser    = "__silt_keyring_probe__"
)

// osStore is the production Store backed by zalando/go-keyring.
type osStore struct{}

func (osStore) Available() bool { return Available() }

func (osStore) Set(service, user, secret string) error {
	if err := gokeyring.Set(service, user, secret); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (osStore) Get(service, user string) (string, error) {
	s, err := gokeyring.Get(service, user)
	if err == nil {
		return s, nil
	}
	if errors.Is(err, gokeyring.ErrNotFound) {
		return "", ErrNotFound
	}
	// Any other error (dbus, no Secret Service, locked session) means the
	// keyring is not usable right now.
	return "", ErrUnavailable
}

func (osStore) Delete(service, user string) error {
	if err := gokeyring.Delete(service, user); err != nil {
		if errors.Is(err, gokeyring.ErrNotFound) {
			return ErrNotFound
		}
		return ErrUnavailable
	}
	return nil
}

// isPlatformAvailable returns true when err indicates the keyring DID answer
// (i.e. the probe reached a backend). It is the negation of "unavailable": only
// a nil or ErrNotFound from the probe means available. Kept as a named helper so
// the double-negative in Available reads clearly.
func isPlatformAvailable(err error) bool {
	return err == nil || errors.Is(err, gokeyring.ErrNotFound)
}

// Fake is an in-memory Store for tests. It is NOT safe for concurrent use by
// itself; tests that exercise concurrency should external-sync. Guarding with a
// mutex here would mask the race semantics the real OS store has none of.
type Fake struct {
	data map[string]string // key: service+"\x00"+user
}

// NewFake returns an empty in-memory Store for tests.
func NewFake() *Fake {
	return &Fake{data: map[string]string{}}
}

func key(service, user string) string { return service + "\x00" + user }

func (f *Fake) Available() bool { return true }

func (f *Fake) Set(service, user, secret string) error {
	if f.data == nil {
		f.data = map[string]string{}
	}
	f.data[key(service, user)] = secret
	return nil
}

func (f *Fake) Get(service, user string) (string, error) {
	if f.data == nil {
		return "", ErrNotFound
	}
	s, ok := f.data[key(service, user)]
	if !ok {
		return "", ErrNotFound
	}
	return s, nil
}

func (f *Fake) Delete(service, user string) error {
	if f.data != nil {
		delete(f.data, key(service, user))
	}
	return nil
}

// UnavailableFake is a Store whose every op returns ErrUnavailable, for testing
// the headless-Linux / locked-session fallback path.
type UnavailableFake struct{}

func (UnavailableFake) Available() bool                    { return false }
func (UnavailableFake) Set(string, string, string) error   { return ErrUnavailable }
func (UnavailableFake) Get(string, string) (string, error) { return "", ErrUnavailable }
func (UnavailableFake) Delete(string, string) error        { return ErrUnavailable }
