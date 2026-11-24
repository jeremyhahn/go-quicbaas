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

package ociregistry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
)

// serviceTestRaftNode stands in for the Raft node the builder requires; the
// KMS paths these tests drive never reach it.
type serviceTestRaftNode struct {
	interfaces.RaftNode
}

// faultKMSManager is the test tenant KMS manager with an injectable lookup
// failure and a count of the keys it was asked to create.
type faultKMSManager struct {
	*kmstenant.Manager
	getErr    error
	generated atomic.Int32
}

// GetKey fails with getErr when it is set, and otherwise looks the key up.
func (m *faultKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.Manager.GetKey(ctx, keyID)
}

// GenerateKey counts the call and creates the key.
func (m *faultKMSManager) GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	m.generated.Add(1)
	return m.Manager.GenerateKey(ctx, attrs)
}

// newServiceSDKKMSClient builds the registry's KMS adapter over the real KMS
// service, reached through the SDK's in-process embedded transport. The
// crypto half is the package fake: key lookup and creation never use it.
func newServiceSDKKMSClient(t *testing.T) (*SDKKMSClient, *faultKMSManager) {
	t.Helper()
	mgr := &faultKMSManager{Manager: kmstenant.NewManager()}
	store := qrdbsdk.NewMemoryKVStore()
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&serviceTestRaftNode{}).
		WithKMSManager(mgr).
		WithKVStoreProvider(func(uint64) interface{} { return store }).
		Build()
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	t.Cleanup(func() {
		if err := svc.Close(); err != nil {
			t.Errorf("close service: %v", err)
		}
	})

	tr, err := embedded.New(svc)
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	client, err := NewSDKKMSClient(kms.NewClient(tr), newFakeSDKEncryptionKMS())
	if err != nil {
		t.Fatalf("NewSDKKMSClient: %v", err)
	}
	return client, mgr
}

// serviceTenantContext binds the tenant the in-process caller acts for.
func serviceTenantContext() context.Context {
	return transport.ContextWithTenant(context.Background(), kmstenant.OwnerTenant)
}

func TestSDKKMSClient_RealService_MissingKeyIsNotFound(t *testing.T) {
	client, _ := newServiceSDKKMSClient(t)

	_, err := client.GetKey(serviceTenantContext(), "container-images/absent")
	var notFound KeyNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetKey of a missing key: got %v, want KeyNotFoundError", err)
	}
}

func TestSDKKMSClient_RealService_LookupFailureIsNotNotFound(t *testing.T) {
	tests := []struct {
		name     string
		getErr   error
		wantCode sdkerrors.ErrorCode
	}{
		{name: "untyped backend failure", getErr: errors.New("backend unreachable"), wantCode: sdkerrors.CodeInternal},
		{
			name:     "coded timeout",
			getErr:   qberrors.New(qberrors.ErrTimeout, "kms.GetKey", "backend timed out"),
			wantCode: sdkerrors.CodeTimeout,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, mgr := newServiceSDKKMSClient(t)
			mgr.getErr = tc.getErr
			ctx := serviceTenantContext()

			_, err := client.GetKey(ctx, "container-images/img-key")
			if err == nil {
				t.Fatal("GetKey over a failed lookup returned no error")
			}
			var notFound KeyNotFoundError
			if errors.As(err, &notFound) {
				t.Fatalf("GetKey over a failed lookup: got KeyNotFoundError %v", err)
			}
			if sdkerrors.IsNotFound(err) {
				t.Fatalf("GetKey over a failed lookup: got not-found code %v", err)
			}
			if got := sdkerrors.Code(err); got != tc.wantCode {
				t.Errorf("GetKey over a failed lookup: code %v, want %v (%v)", got, tc.wantCode, err)
			}

			enc, err := NewImageEncryptor(client)
			if err != nil {
				t.Fatalf("NewImageEncryptor: %v", err)
			}
			if _, err := enc.getOrCreateKey(ctx, "img-key"); err == nil {
				t.Fatal("getOrCreateKey over a failed lookup returned no error")
			}
			if got := mgr.generated.Load(); got != 0 {
				t.Errorf("keys created over a failed lookup: got %d, want 0", got)
			}
		})
	}
}

func TestSDKKMSClient_RealService_MissingKeyIsCreatedOnce(t *testing.T) {
	client, mgr := newServiceSDKKMSClient(t)
	ctx := serviceTenantContext()
	enc, err := NewImageEncryptor(client)
	if err != nil {
		t.Fatalf("NewImageEncryptor: %v", err)
	}

	first, err := enc.getOrCreateKey(ctx, "img-key")
	if err != nil {
		t.Fatalf("first getOrCreateKey: %v", err)
	}
	if got := mgr.generated.Load(); got != 1 {
		t.Fatalf("keys created by the first call: got %d, want 1", got)
	}

	second, err := enc.getOrCreateKey(ctx, "img-key")
	if err != nil {
		t.Fatalf("second getOrCreateKey: %v", err)
	}
	if second.KeyID != first.KeyID {
		t.Errorf("second call key ID: got %q, want %q", second.KeyID, first.KeyID)
	}
	if got := mgr.generated.Load(); got != 1 {
		t.Errorf("keys created after the second call: got %d, want 1", got)
	}
}
