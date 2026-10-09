//go:build !windows && !linux

package systemdisk

import "fmt"

// Space reports unsupported on platforms without an implementation.
func Space(path string) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("disk capacity is not supported on this platform")
}
