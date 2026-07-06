package keyring

import (
	"errors"
	"testing"
)

func TestFake_SetGetDelete(t *testing.T) {
	f := NewFake()
	if _, err := f.Get("svc", "user1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty fake: want ErrNotFound, got %v", err)
	}
	if err := f.Set("svc", "user1", "secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := f.Get("svc", "user1")
	if err != nil || got != "secret" {
		t.Errorf("Get after Set: got %q err=%v, want secret", got, err)
	}
	// Overwrite.
	if err := f.Set("svc", "user1", "new"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got, _ = f.Get("svc", "user1")
	if got != "new" {
		t.Errorf("overwrite: got %q, want new", got)
	}
	// Service/user namespacing is honored.
	if err := f.Set("svc", "user2", "other"); err != nil {
		t.Fatalf("Set user2: %v", err)
	}
	got, _ = f.Get("svc", "user1")
	if got != "new" {
		t.Errorf("user1 clobbered by user2 write: %q", got)
	}
	// Delete is idempotent.
	if err := f.Delete("svc", "user1"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if err := f.Delete("svc", "user1"); err != nil {
		t.Errorf("idempotent Delete: %v", err)
	}
	if _, err := f.Get("svc", "user1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: want ErrNotFound, got nil")
	}
}

func TestUnavailableFake_AlwaysUnavailable(t *testing.T) {
	u := UnavailableFake{}
	if _, err := u.Get("s", "u"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Get: want ErrUnavailable, got %v", err)
	}
	if err := u.Set("s", "u", "x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Set: want ErrUnavailable, got %v", err)
	}
	if err := u.Delete("s", "u"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Delete: want ErrUnavailable, got %v", err)
	}
}

// Default returns a non-nil Store in every build (the OS impl). Whether that
// store is reachable is probed separately via Available(); this just guards the
// constructor contract used by NewApp.
func TestDefault_NonNil(t *testing.T) {
	if Default() == nil {
		t.Fatal("Default() must return a non-nil Store")
	}
}
