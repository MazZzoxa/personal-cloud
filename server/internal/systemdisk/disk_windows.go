//go:build windows

package systemdisk

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// Space returns total capacity and bytes available to the current user.
func Space(path string) (uint64, uint64, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return 0, 0, err
	}
	name, err := syscall.UTF16PtrFromString(absolute)
	if err != nil {
		return 0, 0, err
	}
	var available, total, totalFree uint64
	result, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return 0, 0, callErr
		}
		return 0, 0, fmt.Errorf("GetDiskFreeSpaceExW failed")
	}
	return total, available, nil
}
