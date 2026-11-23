//go:build linux

package orchestration

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// lockVolumeState opens path and takes an exclusive, non-blocking flock on
// it. flock locks belong to the open file description, so a second open of
// the same file conflicts even inside one process. A lock another description
// holds is reported as ErrVolumeStateLocked. Closing the file drops the lock.
func lockVolumeState(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		_ = f.Close()
		return nil, ErrVolumeStateLocked
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	return f, nil
}
