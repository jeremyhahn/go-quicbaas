// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package quicbaas

import (
	"context"
	"testing"

	qrdbdek "github.com/jeremyhahn/go-qrdb/pkg/dek"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// stubDEKServicer is a go-qrdb DEKServicer that records the tenant each call
// names and answers with a fixed keyring.
type stubDEKServicer struct {
	calls   []string
	current uint64
}

func (s *stubDEKServicer) status(tenantID string) *qrdbdek.Status {
	return &qrdbdek.Status{
		TenantID:  "tenant-a",
		Algorithm: qrdbdek.Algorithm,
		Current:   s.current,
		Versions:  []qrdbdek.VersionInfo{{Version: s.current}},
	}
}

func (s *stubDEKServicer) Status(_ context.Context, tenantID string) (*qrdbdek.Status, error) {
	s.calls = append(s.calls, "status:"+tenantID)
	return s.status(tenantID), nil
}

func (s *stubDEKServicer) Rotate(_ context.Context, tenantID string) (*qrdbdek.Status, error) {
	s.calls = append(s.calls, "rotate:"+tenantID)
	s.current++
	return s.status(tenantID), nil
}

func (s *stubDEKServicer) Retire(_ context.Context, tenantID string, _ uint64) (*qrdbdek.Status, error) {
	s.calls = append(s.calls, "retire:"+tenantID)
	return s.status(tenantID), nil
}

func (s *stubDEKServicer) Encrypt(_ context.Context, _ string, _, _ []byte) (*qrdbdek.Sealed, error) {
	return &qrdbdek.Sealed{}, nil
}

func (s *stubDEKServicer) Decrypt(_ context.Context, _ string, _, _ []byte) (*qrdbdek.Opened, error) {
	return &qrdbdek.Opened{}, nil
}

func (s *stubDEKServicer) Rewrap(_ context.Context, _ string, _, _ []byte) (*qrdbdek.Rewrapped, error) {
	return &qrdbdek.Rewrapped{}, nil
}

var _ QRDBDEKServicer = (*stubDEKServicer)(nil)

// TestSecurityTenantDEK_RoutesThroughEmbeddedTransport proves that a node
// composing the SDK in-process (WithEmbeddedQRDB with a DEK servicer) serves
// Security().TenantDEK() from go-qrdb's own keyring service, and that the
// client it returns is go-qrdb's DEKClient.
func TestSecurityTenantDEK_RoutesThroughEmbeddedTransport(t *testing.T) {
	svc := &stubDEKServicer{current: 1}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
		DEK:   svc,
	})

	keyring, err := c.Security().TenantDEK()
	if err != nil {
		t.Fatalf("Security().TenantDEK(): %v", err)
	}
	var _ qrdbtransport.DEKClient = keyring

	status, err := keyring.TenantDEKStatus(context.Background(), &qrdbtransport.TenantDEKRequest{})
	if err != nil {
		t.Fatalf("TenantDEKStatus: %v", err)
	}
	if status.CurrentVersion != 1 || status.Algorithm != qrdbdek.Algorithm {
		t.Errorf("status = %+v, want current 1, algorithm %s", status, qrdbdek.Algorithm)
	}

	rotated, err := keyring.TenantDEKRotate(context.Background(), &qrdbtransport.TenantDEKRequest{})
	if err != nil {
		t.Fatalf("TenantDEKRotate: %v", err)
	}
	if rotated.CurrentVersion != 2 {
		t.Errorf("rotated current = %d, want 2", rotated.CurrentVersion)
	}
	if len(svc.calls) != 2 {
		t.Errorf("servicer calls = %v, want status then rotate", svc.calls)
	}
}

// TestSecurityTenantDEK_WithoutServicerIsRefused proves that an embedded
// client built without a DEK servicer refuses keyring calls instead of
// answering from some other store.
func TestSecurityTenantDEK_WithoutServicerIsRefused(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
	})

	keyring, err := c.Security().TenantDEK()
	if err != nil {
		t.Fatalf("Security().TenantDEK(): %v", err)
	}
	if _, err := keyring.TenantDEKStatus(context.Background(), &qrdbtransport.TenantDEKRequest{}); err == nil {
		t.Fatal("TenantDEKStatus without a DEK servicer = nil error, want a refusal")
	}
}
