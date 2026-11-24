package server

import (
	"path/filepath"
	"testing"

	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
)

// resolveXKMSServiceForCABinding initialises the XKMS subsystem the way the
// node does and returns the composed XKMS service. The service's previous CA
// is restored when the test ends.
func resolveXKMSServiceForCABinding(t *testing.T) *xkmslib.XKMSService {
	t.Helper()
	setupOIDCTestXKMS(t)
	x, err := xkmssdk.Get()
	require.NoError(t, err, "the XKMS service must resolve")
	require.NotNil(t, x)
	prev := x.CA()
	t.Cleanup(func() { x.SetCA(prev) })
	return x
}

// newNodeCA builds the node's own certificate authority through initializeCA
// and returns it, with the XKMS service's CA put back to prev so the caller
// observes only its own adoption.
func newNodeCA(t *testing.T, x *xkmslib.XKMSService) xkmssdk.TrustedCA {
	t.Helper()
	prev := x.CA()
	srv := withTempServer(t)
	srv.config.Security.TLSEnabled = true
	srv.config.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	srv.config.Security.TLSCAFile = filepath.Join(t.TempDir(), "ca-chain.crt")
	require.NoError(t, srv.initializeCA(), "initializeCA must succeed")
	require.NotNil(t, srv.xkmsCA, "initializeCA must adopt a CA")
	x.SetCA(prev)
	return srv.xkmsCA
}

// TestXKMSCABinding_AdoptCASetsTheXKMSServiceCA verifies that adopting a CA
// hands it to the composed XKMS service, so the canonical KeystoreService
// serves CA RPCs.
func TestXKMSCABinding_AdoptCASetsTheXKMSServiceCA(t *testing.T) {
	x := resolveXKMSServiceForCABinding(t)
	ca := newNodeCA(t, x)

	srv := withTempServer(t)
	srv.adoptCA(ca)

	require.NotNil(t, x.CA(), "the XKMS service must hold the adopted CA")
	require.Same(t, ca, x.CA(), "the XKMS service must sign with the node's adopted CA")
}

// TestXKMSCABinding_NoCAIsANoOp verifies that with no adopted CA, binding
// leaves the XKMS service's CA untouched.
func TestXKMSCABinding_NoCAIsANoOp(t *testing.T) {
	x := resolveXKMSServiceForCABinding(t)
	before := x.CA()

	srv := withTempServer(t)
	srv.xkmsCA = nil
	require.NotPanics(t, srv.bindCAToXKMSService)

	require.Equal(t, before, x.CA(), "binding without a CA must not change the XKMS service's CA")
}

// TestXKMSCABinding_NonProviderCAWithNilLoggerDoesNotPanic pins that a bare
// Server (no logger, as unit tests build it) adopting a CA that is not an
// XKMS CA provider returns without binding instead of dereferencing the nil
// logger.
func TestXKMSCABinding_NonProviderCAWithNilLoggerDoesNotPanic(t *testing.T) {
	srv := &Server{xkmsCA: newFakeTrustedCA(t)}
	require.NotPanics(t, srv.bindCAToXKMSService)
}
