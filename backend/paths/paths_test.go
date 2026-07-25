package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocalDataDir_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SILT_DATA_DIR", tmp)
	got, err := LocalDataDir()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != tmp {
		t.Errorf("LocalDataDir() = %q, want %q", got, tmp)
	}
}

func TestLocalDataDir_DefaultSuffix(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", "")
	got, err := LocalDataDir()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if filepath.Base(got) != "silt" {
		t.Errorf("LocalDataDir() = %q, want base %q", got, "silt")
	}
}

func TestVaultKey(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	ka, kb := VaultKey(a), VaultKey(b)
	if len(ka) != 64 {
		t.Errorf("len(VaultKey) = %d, want 64 hex chars (full sha256)", len(ka))
	}
	if ka == kb {
		t.Errorf("distinct vaults collided on key %q", ka)
	}
	if VaultKey(a) != ka {
		t.Error("VaultKey is not deterministic across calls")
	}
}

func TestVaultKey_CaseInsensitiveOnCIFilesystems(t *testing.T) {
	// Windows folds case (its filesystems are case-insensitive by default) so
	// equivalent casings share a key. macOS is intentionally NOT folded: default
	// macOS is case-insensitive (duplicate-index waste, harmless) but
	// case-sensitive HFS/APFS is supported and folding would corrupt by
	// colliding two distinct physical vaults.
	tmp := t.TempDir()
	if VaultKey(filepath.Join(tmp, "Vault")) != VaultKey(filepath.Join(tmp, "vault")) {
		if runtime.GOOS == "windows" {
			t.Errorf("case variants should share a key on Windows")
		}
	}
}

func TestLocalIndexPath(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	vault := t.TempDir()

	p, err := LocalIndexPath(vault)
	if err != nil {
		t.Fatalf("LocalIndexPath: %v", err)
	}
	if filepath.Base(p) != "index.sqlite" {
		t.Errorf("base = %q, want index.sqlite", filepath.Base(p))
	}
	if filepath.Base(filepath.Dir(p)) != VaultKey(vault) {
		t.Errorf("parent dir %q is not the vault key", filepath.Dir(p))
	}
	// The parent directory is created.
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Errorf("index dir not created: %v", err)
	}
	// Deterministic for the same vault; distinct across vaults.
	if p2, _ := LocalIndexPath(vault); p2 != p {
		t.Error("LocalIndexPath is not deterministic")
	}
	if other, _ := LocalIndexPath(t.TempDir()); other == p {
		t.Error("distinct vaults collided on index path")
	}
}
