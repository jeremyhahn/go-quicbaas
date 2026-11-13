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

//go:build linux

package fleet

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func mustLockWorkDir(t *testing.T, dir string) *WorkDirLock {
	t.Helper()
	lock, err := LockWorkDir(dir)
	if err != nil {
		t.Fatalf("LockWorkDir(%s): %v", dir, err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Errorf("Release: %v", err)
		}
	})
	return lock
}

// flock locks belong to an open file description, so a second open of the
// same lock file in this one process conflicts exactly as a second process
// would. This is the property the whole lock rests on: prove it, do not
// assume it.
func TestLockWorkDir_SecondAcquireIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustLockWorkDir(t, dir)

	second, err := LockWorkDir(dir)
	if err == nil {
		if releaseErr := second.Release(); releaseErr != nil {
			t.Errorf("Release: %v", releaseErr)
		}
		t.Fatal("second LockWorkDir on a held work dir succeeded; want refusal")
	}
	if !errors.Is(err, ErrWorkDirLocked) {
		t.Fatalf("err = %v; want errors.Is ErrWorkDirLocked", err)
	}
	var locked *WorkDirLockedError
	if !errors.As(err, &locked) {
		t.Fatalf("err = %T; want *WorkDirLockedError", err)
	}
	if locked.Dir != dir {
		t.Errorf("Dir = %q; want %q", locked.Dir, dir)
	}
	if locked.PID != os.Getpid() {
		t.Errorf("PID = %d; want the holder's pid %d", locked.PID, os.Getpid())
	}
	if !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Errorf("error %q must name the work dir and the holder's pid", err)
	}
}

func TestLockWorkDir_ReleaseThenAcquireSucceeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := LockWorkDir(dir)
	if err != nil {
		t.Fatalf("first LockWorkDir: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	mustLockWorkDir(t, dir)
}

// A process forked while the lock is held shares its open file description
// until it execs. Release must drop the flock itself, not rely on Close, or
// that child keeps the work dir locked after Release returns.
func TestLockWorkDir_ReleaseFreesTheDirWhileAForkedCopyIsOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := LockWorkDir(dir)
	if err != nil {
		t.Fatalf("first LockWorkDir: %v", err)
	}
	childFD, err := unix.Dup(int(first.file.Fd()))
	if err != nil {
		t.Fatalf("dup lock fd: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Close(childFD); err != nil {
			t.Errorf("close dup fd: %v", err)
		}
	})
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	mustLockWorkDir(t, dir)
}

func TestLockWorkDir_ReleaseReportsAFailedUnlockAndClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock, err := LockWorkDir(dir)
	if err != nil {
		t.Fatalf("LockWorkDir: %v", err)
	}
	if err := lock.file.Close(); err != nil {
		t.Fatalf("close lock file underneath: %v", err)
	}
	err = lock.Release()
	var opErr *OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("Release error = %v, want *OpError", err)
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("Release error = %v, want it to wrap os.ErrClosed", err)
	}
	if err := lock.Release(); err != nil {
		t.Errorf("second Release = %v, want nil", err)
	}
}

func TestLockWorkDir_ReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	lock, err := LockWorkDir(t.TempDir())
	if err != nil {
		t.Fatalf("LockWorkDir: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
	var nilLock *WorkDirLock
	if err := nilLock.Release(); err != nil {
		t.Fatalf("Release on a nil lock: %v", err)
	}
}

func TestLockWorkDir_TwoWorkDirsBothLock(t *testing.T) {
	t.Parallel()
	a := mustLockWorkDir(t, t.TempDir())
	b := mustLockWorkDir(t, t.TempDir())
	if a.Dir() == b.Dir() {
		t.Fatalf("both locks report work dir %q", a.Dir())
	}
}

func TestLockWorkDir_RecordsHolderPID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustLockWorkDir(t, dir)
	data, err := os.ReadFile(filepath.Join(dir, WorkDirLockFileName))
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != strconv.Itoa(os.Getpid()) {
		t.Fatalf("lock file holds %q; want pid %d", got, os.Getpid())
	}
}

func TestLockWorkDir_MissingWorkDirIsRefusedAndNotCreated(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "absent")
	_, err := LockWorkDir(dir)
	var opErr *OpError
	if !errors.As(err, &opErr) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v; want *OpError wrapping fs.ErrNotExist", err)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("LockWorkDir created the missing work dir (stat err %v)", statErr)
	}
}

func TestLockWorkDir_UnwritableWorkDirIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := filepath.Join(dir, WorkDirLockFileName)
	// Root ignores mode bits, so a 0500 work dir does not refuse it; the
	// opener fails the way the kernel does for a caller without write access.
	denied := func(name string, flag int, _ os.FileMode) (*os.File, error) {
		if name != want || flag&os.O_CREATE == 0 {
			t.Errorf("open(%q, %#x); want %q with O_CREATE", name, flag, want)
		}
		return nil, &fs.PathError{Op: "open", Path: name, Err: syscall.EACCES}
	}
	_, err := lockWorkDir(dir, denied)
	var opErr *OpError
	if !errors.As(err, &opErr) || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v; want *OpError wrapping fs.ErrPermission", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("err = %v; want it to name the work dir %s", err, dir)
	}
}

func TestLockWorkDir_FileIsNotAWorkDir(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LockWorkDir(path); !errors.Is(err, ErrWorkDirNotDirectory) {
		t.Fatalf("err = %v; want ErrWorkDirNotDirectory", err)
	}
}

func TestLockWorkDir_EmptyWorkDirIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := LockWorkDir("  "); !errors.Is(err, ErrConfigFieldRequired) {
		t.Fatalf("err = %v; want ErrConfigFieldRequired", err)
	}
}

func TestAcquireWorkDirLock_CreatesTheWorkDirThenLocks(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "fresh")
	lock, err := AcquireWorkDirLock(dir, &testLogger{t: t})
	if err != nil {
		t.Fatalf("AcquireWorkDirLock: %v", err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Errorf("Release: %v", err)
		}
	})
	if _, err := LockWorkDir(dir); !errors.Is(err, ErrWorkDirLocked) {
		t.Fatalf("err = %v; want the work dir held after AcquireWorkDirLock", err)
	}
}

func TestAcquireWorkDirLock_HeldWorkDirIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustLockWorkDir(t, dir)
	if _, err := AcquireWorkDirLock(dir, &testLogger{t: t}); !errors.Is(err, ErrWorkDirLocked) {
		t.Fatalf("err = %v; want ErrWorkDirLocked", err)
	}
}
