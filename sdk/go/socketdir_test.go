package quicbaas

import (
	"os"
	"testing"
)

// shortSocketDir returns a fresh directory directly under /tmp for a test
// that binds a Unix socket in it. t.TempDir() lives under TMPDIR, which on
// the CI runner (/home/runner/_work/_temp/<TestName><digits>/) is long
// enough to push a socket path past Linux's 107-byte sun_path limit, and
// bind then fails with "invalid argument". The directory is removed when
// the test ends.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "qbs")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
