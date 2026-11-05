//go:build linux

package db

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Linux network-filesystem magic numbers (from linux/magic.h). WAL requires
// shared memory which network mounts do not provide.
var linuxNetworkFSMagic = map[int64]bool{
	0x6969:     true, // NFS_SUPER_MAGIC
	0xFF534D42: true, // CIFS_MAGIC_NUMBER (SMB/Samba)
	0xFE534D42: true, // SMB2_MAGIC_NUMBER
	0x517B:     true, // SMB_SUPER_MAGIC (legacy)
	0x564C:     true, // NCP_SUPER_MAGIC (NetWare)
}

func detectNetworkFilesystem(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return nil // If we can't stat, let sql.Open surface the real error
	}
	if linuxNetworkFSMagic[int64(stat.Type)] {
		return fmt.Errorf("%w: %s is on a network filesystem (type 0x%x). WAL mode requires shared memory, which network mounts do not provide. Move the vault to a local folder",
			ErrNetworkFilesystem, path, stat.Type)
	}
	return nil
}

// fuseSuperMagic is FUSE_SUPER_MAGIC (linux/magic.h): sshfs, rclone, and the
// other user-space mounts. Detected for page writes (#1020) but deliberately
// NOT for the WAL gate — see IsNetworkFSForWrite.
const fuseSuperMagic int64 = 0x65735546

// isWriteNetworkFSMagic is the extended lookup consumed by the verified-write
// detector: the WAL-gate set plus FUSE, composed rather than duplicated so
// the base set has one source of truth (adding a magic to
// linuxNetworkFSMagic widens both consumers; only a deliberate entry here
// widens just the write set).
func isWriteNetworkFSMagic(magic int64) bool {
	return linuxNetworkFSMagic[magic] || magic == fuseSuperMagic
}

func detectNetworkFilesystemForWrite(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		// Same nil-on-unprobeable contract as the WAL detector: the
		// verified-write probe (probeRootNetworkFS) owns fail-safe
		// handling of roots that cannot even be stat'd.
		return nil
	}
	if isWriteNetworkFSMagic(int64(stat.Type)) {
		return fmt.Errorf("%w: %s is on a network filesystem (type 0x%x). Renames may not be atomic there, so page writes use the verified (backup + read-back) mode",
			ErrNetworkFilesystem, path, stat.Type)
	}
	return nil
}
