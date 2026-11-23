//go:build !linux

package orchestration

import "os"

// lockVolumeState opens path without a lock. The fleet volume node is
// Linux-only, and the per-work-dir staging dir still keeps daemons apart.
func lockVolumeState(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
}
