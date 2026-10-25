package server

import (
	"os"
	"testing"

	testqrdbnode "github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// TestMain closes the package's shared clustered qrdb node (and removes its
// data directory) once every test has run.
func TestMain(m *testing.M) {
	code := m.Run()
	testqrdbnode.CloseShared()
	os.Exit(code)
}
