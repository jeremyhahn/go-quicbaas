// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package fleet

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// WorkDirLockFileName is the file inside a work dir that a running runtime
// holds an exclusive lock on, and in which it records its PID.
const WorkDirLockFileName = "runtime.lock"

var (
	// ErrWorkDirLocked indicates another runtime already holds the work dir.
	// The work dir is the owner label SweepOrphanContainers reaps by, so two
	// runtimes sharing one would reap each other's containers.
	ErrWorkDirLocked = errors.New("agent: work dir is locked by another runtime")
	// ErrWorkDirNotDirectory indicates the configured work dir is not a
	// directory.
	ErrWorkDirNotDirectory = errors.New("agent: work dir is not a directory")
	// ErrWorkDirLockUnsupported indicates the platform has no advisory file
	// lock the work-dir lock can be built on.
	ErrWorkDirLockUnsupported = errors.New("agent: work dir lock is not supported on this platform")
)

// WorkDirLockedError reports the work dir another runtime holds, and the PID
// that runtime recorded in the lock file when it could be read.
type WorkDirLockedError struct {
	// Dir is the absolute work dir.
	Dir string
	// PID is the holder's PID, or 0 when the lock file could not be read.
	PID int
}

// Error implements the error interface.
func (e *WorkDirLockedError) Error() string {
	msg := ErrWorkDirLocked.Error() + ": " + e.Dir
	if e.PID > 0 {
		msg += " (held by pid " + strconv.Itoa(e.PID) + ")"
	}
	return msg
}

// Unwrap returns ErrWorkDirLocked for errors.Is support.
func (e *WorkDirLockedError) Unwrap() error {
	return ErrWorkDirLocked
}

// WorkDirLock is an exclusive advisory lock on a work dir. The kernel drops
// it when the holding process exits, so a crashed runtime never leaves a
// stale lock behind. A WorkDirLock is released once, from one goroutine.
type WorkDirLock struct {
	dir  string
	file *os.File
}

// LockWorkDir takes the exclusive lock on dir without blocking. dir must
// already exist and be writable; LockWorkDir never creates it. A dir another
// runtime holds is refused with *WorkDirLockedError.
func LockWorkDir(dir string) (*WorkDirLock, error) {
	return lockWorkDir(dir, os.OpenFile)
}

// openFileFunc opens a file the way os.OpenFile does. lockWorkDir takes one
// so a test can make creating the lock file fail with a permission error
// even when it runs as root, which ignores mode bits.
type openFileFunc func(name string, flag int, perm os.FileMode) (*os.File, error)

// lockWorkDir is LockWorkDir with the lock file opened through open.
func lockWorkDir(dir string, open openFileFunc) (*WorkDirLock, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "work_dir"}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, &OpError{Op: "agent: resolve work dir", Cause: err}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, &OpError{Op: "agent: lock work dir " + abs, Cause: err}
	}
	if !info.IsDir() {
		return nil, &SentinelDetailError{Sentinel: ErrWorkDirNotDirectory, Detail: abs}
	}

	path := filepath.Join(abs, WorkDirLockFileName)
	file, err := open(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, &OpError{Op: "agent: lock work dir " + abs, Cause: err}
	}
	if err := lockFile(file); err != nil {
		if errors.Is(err, ErrWorkDirLocked) {
			err = &WorkDirLockedError{Dir: abs, PID: lockHolderPID(path)}
		} else {
			err = &OpError{Op: "agent: lock work dir " + abs, Cause: err}
		}
		return nil, errors.Join(err, file.Close())
	}
	if err := recordLockHolder(file); err != nil {
		return nil, errors.Join(&OpError{Op: "agent: record work dir lock holder " + abs, Cause: err}, file.Close())
	}
	return &WorkDirLock{dir: abs, file: file}, nil
}

// AcquireWorkDirLock creates dir when it does not exist and takes its lock.
// It is the startup step every runtime that owns a work dir runs before it
// constructs anything that stamps or sweeps by that dir. On a platform
// without the lock it logs a warning and returns a nil lock, whose Release
// is a no-op.
func AcquireWorkDirLock(dir string, log interfaces.Logger) (*WorkDirLock, error) {
	if err := EnsureWorkDir(dir); err != nil {
		return nil, err
	}
	lock, err := LockWorkDir(dir)
	if errors.Is(err, ErrWorkDirLockUnsupported) {
		log.Warnf("agent: work dir %s is not locked: %v", dir, err)
		return nil, nil
	}
	return lock, err
}

// Dir returns the absolute work dir the lock holds.
func (l *WorkDirLock) Dir() string {
	return l.dir
}

// Release drops the lock. Releasing a nil or already released lock is a
// no-op. The lock file stays in place: removing it would let a runtime that
// opened the old file and one that creates a new one both hold "the" lock.
func (l *WorkDirLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	if err := file.Close(); err != nil {
		return &OpError{Op: "agent: release work dir lock " + l.dir, Cause: err}
	}
	return nil
}

// recordLockHolder replaces the lock file's content with this process's PID,
// so the runtime refused the lock can name the one holding it.
func recordLockHolder(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return err
}

// lockHolderPID reads the PID the holder recorded, or 0 when the file cannot
// be read or does not hold one: the PID only enriches the error.
func lockHolderPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
