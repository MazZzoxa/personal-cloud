//go:build linux

package systemdisk

import (
	"path/filepath"
	"syscall"
)

// Space returns total capacity and bytes available to the current user.
func Space(path string) (uint64, uint64, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return 0, 0, err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(absolute, &stat); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(stat.Bsize)
	return uint64(stat.Blocks) * blockSize, uint64(stat.Bavail) * blockSize, nil
}
