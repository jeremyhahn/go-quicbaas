// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Log file names inside a container's bundle directory.
const (
	stdoutLogName = "stdout.log"
	stderrLogName = "stderr.log"
)

// containerLogs holds the files a container's init process writes its
// stdout and stderr to.
type containerLogs struct {
	stdout *os.File
	stderr *os.File
}

// openContainerLogs opens, for appending, the stdout and stderr logs in
// bundleDir.
func openContainerLogs(bundleDir string) (*containerLogs, error) {
	stdout, err := openContainerLog(filepath.Join(bundleDir, stdoutLogName))
	if err != nil {
		return nil, err
	}
	stderr, err := openContainerLog(filepath.Join(bundleDir, stderrLogName))
	if err != nil {
		return nil, errors.Join(err, stdout.Close())
	}
	return &containerLogs{stdout: stdout, stderr: stderr}, nil
}

// openContainerLog opens one log file for appending, creating it if needed.
func openContainerLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) // #nosec G304 -- path is inside the runtime's own bundle directory.
}

// close closes both log files.
func (l *containerLogs) close() error {
	return errors.Join(l.stdout.Close(), l.stderr.Close())
}

// readContainerLog returns the contents of a log file. A log that was never
// written, because the container has not started, is empty.
func readContainerLog(path string) ([]byte, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is inside the runtime's own bundle directory.
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}
