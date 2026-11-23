package backend_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/backend"
)

// TestPIVImportReexport_TypesAreIdentical proves the request, response and
// importer re-exports are aliases of the go-xkms SDK types, not copies.
func TestPIVImportReexport_TypesAreIdentical(t *testing.T) {
	upstreamReq := &xkmssdk.ImportPIVKeyRequest{Backend: "software", Slot: "9a"}
	var req *backend.ImportPIVKeyRequest = upstreamReq
	if req.Slot != "9a" || req.Backend != "software" {
		t.Fatalf("aliased request lost fields: %+v", req)
	}

	upstreamResp := &xkmssdk.ImportPIVKeyResponse{Slot: "9a", Algorithm: "ecdsap256"}
	var resp *backend.ImportPIVKeyResponse = upstreamResp
	if resp.Algorithm != "ecdsap256" {
		t.Fatalf("aliased response lost fields: %+v", resp)
	}

	var importer backend.PIVKeyImporter
	var upstreamImporter xkmssdk.PIVKeyImporter = importer
	if upstreamImporter != nil {
		t.Fatal("zero PIVKeyImporter should be nil")
	}
}

// TestPIVImportReexport_SentinelIsUpstream proves ErrPIVImportNotSupported
// is the go-xkms sentinel itself, so errors.Is matches across the seam.
func TestPIVImportReexport_SentinelIsUpstream(t *testing.T) {
	if backend.ErrPIVImportNotSupported == nil {
		t.Fatal("ErrPIVImportNotSupported is nil")
	}
	if !errors.Is(backend.ErrPIVImportNotSupported, xkmssdk.ErrPIVImportNotSupported) {
		t.Fatal("ErrPIVImportNotSupported is not the go-xkms sentinel")
	}
	if errors.Is(backend.ErrPIVImportNotSupported, backend.ErrPIVNotInitialized) {
		t.Fatal("ErrPIVImportNotSupported must stay distinct from ErrPIVNotInitialized")
	}
}

// TestPIVImportReexport_UninitializedRefuses proves ImportPIVKey before the
// PIV manager is initialized returns ErrPIVNotInitialized and does not panic.
func TestPIVImportReexport_UninitializedRefuses(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal test key: %v", err)
	}
	resp, err := backend.ImportPIVKey(context.Background(), &backend.ImportPIVKeyRequest{
		Backend:    "software",
		Slot:       "9a",
		PrivateKey: der,
	})
	if err == nil {
		t.Fatalf("ImportPIVKey without PIV init succeeded: %+v", resp)
	}
	if !errors.Is(err, backend.ErrPIVNotInitialized) {
		t.Fatalf("ImportPIVKey without PIV init: got %v, want ErrPIVNotInitialized", err)
	}
	if resp != nil {
		t.Fatalf("ImportPIVKey returned a response with its error: %+v", resp)
	}
}
