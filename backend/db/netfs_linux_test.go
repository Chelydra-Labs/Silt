//go:build linux

package db

import "testing"

// The #1020 split, pinned at the magic-set seam (a real FUSE mount cannot be
// reproduced on CI): the verified-write detector recognizes FUSE; the
// WAL-gate set consumed by db.IsNetworkFS — and through it the vault mover's
// destination check — must NOT, or vaults that open fine today would be
// retroactively refused.
func TestLinuxWriteNetworkFSMagic_ExtendedWithFUSE(t *testing.T) {
	cases := []struct {
		name      string
		magic     int64
		wantBase  bool // in linuxNetworkFSMagic (WAL gate + vault mover)
		wantWrite bool // in the extended verified-write set
	}{
		{"NFS_SUPER_MAGIC", 0x6969, true, true},
		{"CIFS_MAGIC_NUMBER (SMB/Samba)", 0xFF534D42, true, true},
		{"SMB2_MAGIC_NUMBER", 0xFE534D42, true, true},
		{"SMB_SUPER_MAGIC (legacy)", 0x517B, true, true},
		{"NCP_SUPER_MAGIC (NetWare)", 0x564C, true, true},
		{"FUSE_SUPER_MAGIC (sshfs, rclone)", fuseSuperMagic, false, true},
		{"EXT4_SUPER_MAGIC (local)", 0xEF53, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := linuxNetworkFSMagic[tc.magic]; got != tc.wantBase {
				t.Errorf("WAL-gate set contains 0x%x = %v, want %v (the WAL gate and vault mover stay conservative)", tc.magic, got, tc.wantBase)
			}
			if got := isWriteNetworkFSMagic(tc.magic); got != tc.wantWrite {
				t.Errorf("verified-write set contains 0x%x = %v, want %v", tc.magic, got, tc.wantWrite)
			}
		})
	}
}

// Both detectors stay silent on a local filesystem: the extension must not
// change behavior for the overwhelmingly common case.
func TestLinuxNetworkFSDetectors_LocalDirPasses(t *testing.T) {
	local := t.TempDir()
	if err := IsNetworkFS(local); err != nil {
		t.Errorf("IsNetworkFS(local) = %v, want nil", err)
	}
	if err := IsNetworkFSForWrite(local); err != nil {
		t.Errorf("IsNetworkFSForWrite(local) = %v, want nil", err)
	}
}
