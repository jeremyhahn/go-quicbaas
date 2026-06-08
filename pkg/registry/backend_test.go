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

package registry

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// fakeTransport is the minimal qrdb transport the clientBackend exercises.
// The qrdb *Client type-asserts its transport to the typed sub-clients
// (LeaseClient, MVCCClient, KVClient, WatchClient), so the fake embeds
// those interfaces to satisfy the assertions and overrides only the six
// methods backend.go calls. Embedding leaves the unused methods nil, but
// they are never invoked by the clientBackend, so this stays a faithful
// adapter test rather than a partial reimplementation.
type fakeTransport struct {
	qrdbtransport.LeaseClient
	qrdbtransport.MVCCClient
	qrdbtransport.KVClient
	qrdbtransport.WatchClient

	grantedTTL    int64
	keepAliveID   int64
	revokedID     int64
	putKey        string
	putValue      []byte
	putLease      int64
	scannedPrefix string
	watchPrefix   string
	watchOpts     qrdbtransport.WatchOptions

	grantErr     error
	keepAliveErr error
	revokeErr    error
	putErr       error
	scanErr      error
	watchErr     error
}

// Transport interface methods (unused by the backend but required to
// construct a real qrdbsdk.Client via NewClient).
func (f *fakeTransport) Connect(_ context.Context) error { return nil }
func (f *fakeTransport) Close() error                    { return nil }
func (f *fakeTransport) Healthy(_ context.Context) bool  { return true }
func (f *fakeTransport) Request(_ context.Context, _ string, _, _ any) error {
	return nil
}

// LeaseClient overrides.
func (f *fakeTransport) LeaseGrant(_ context.Context, ttl int64) (*qrdbtransport.LeaseInfo, error) {
	if f.grantErr != nil {
		return nil, f.grantErr
	}
	f.grantedTTL = ttl
	return &qrdbtransport.LeaseInfo{ID: 4242, TTL: ttl}, nil
}

func (f *fakeTransport) LeaseKeepAlive(_ context.Context, leaseID int64) (*qrdbtransport.LeaseInfo, error) {
	if f.keepAliveErr != nil {
		return nil, f.keepAliveErr
	}
	f.keepAliveID = leaseID
	return &qrdbtransport.LeaseInfo{ID: leaseID}, nil
}

func (f *fakeTransport) LeaseRevoke(_ context.Context, leaseID int64) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revokedID = leaseID
	return nil
}

// MVCCClient override.
func (f *fakeTransport) PutWithLease(_ context.Context, key string, value []byte, leaseID int64, _ ...qrdbtransport.WriteOption) (*qrdbtransport.MVCCKeyValue, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	f.putKey = key
	f.putValue = value
	f.putLease = leaseID
	return &qrdbtransport.MVCCKeyValue{Key: []byte(key), Value: value}, nil
}

// KVClient override.
func (f *fakeTransport) Scan(_ context.Context, prefix string, _ ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	f.scannedPrefix = prefix
	return map[string][]byte{prefix + "a": []byte("v")}, nil
}

// WatchClient override.
func (f *fakeTransport) WatchPrefix(_ context.Context, prefix string, opts qrdbtransport.WatchOptions) (<-chan qrdbtransport.WatchResponse, error) {
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	f.watchPrefix = prefix
	f.watchOpts = opts
	ch := make(chan qrdbtransport.WatchResponse)
	close(ch)
	return ch, nil
}

func newClientWith(t *testing.T, ft *fakeTransport) *qrdbsdk.Client {
	t.Helper()
	c, err := qrdbsdk.NewClient(ft)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestNewFromClient_WiresBackend(t *testing.T) {
	r := NewFromClient(newClientWith(t, &fakeTransport{}))
	if r == nil {
		t.Fatal("NewFromClient returned nil Registry")
	}
}

func TestClientBackend_Grant(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))

	id, err := b.Grant(context.Background(), 30)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if id != 4242 {
		t.Fatalf("Grant id = %d, want 4242 (LeaseInfo.ID propagated)", id)
	}
	if ft.grantedTTL != 30 {
		t.Fatalf("forwarded TTL = %d, want 30", ft.grantedTTL)
	}
}

func TestClientBackend_GrantError(t *testing.T) {
	boom := errors.New("grant boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{grantErr: boom}))
	id, err := b.Grant(context.Background(), 30)
	if !errors.Is(err, boom) {
		t.Fatalf("Grant err = %v, want %v", err, boom)
	}
	if id != 0 {
		t.Fatalf("Grant id on error = %d, want 0", id)
	}
}

func TestClientBackend_KeepAlive(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	if err := b.KeepAlive(context.Background(), 7); err != nil {
		t.Fatalf("KeepAlive: %v", err)
	}
	if ft.keepAliveID != 7 {
		t.Fatalf("forwarded leaseID = %d, want 7", ft.keepAliveID)
	}
}

func TestClientBackend_KeepAliveError(t *testing.T) {
	boom := errors.New("keepalive boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{keepAliveErr: boom}))
	if err := b.KeepAlive(context.Background(), 7); !errors.Is(err, boom) {
		t.Fatalf("KeepAlive err = %v, want %v", err, boom)
	}
}

func TestClientBackend_Revoke(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	if err := b.Revoke(context.Background(), 9); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if ft.revokedID != 9 {
		t.Fatalf("forwarded leaseID = %d, want 9", ft.revokedID)
	}
}

func TestClientBackend_RevokeError(t *testing.T) {
	boom := errors.New("revoke boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{revokeErr: boom}))
	if err := b.Revoke(context.Background(), 9); !errors.Is(err, boom) {
		t.Fatalf("Revoke err = %v, want %v", err, boom)
	}
}

func TestClientBackend_PutWithLease(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	if err := b.PutWithLease(context.Background(), "svc/a", []byte("data"), 11); err != nil {
		t.Fatalf("PutWithLease: %v", err)
	}
	if ft.putKey != "svc/a" || string(ft.putValue) != "data" || ft.putLease != 11 {
		t.Fatalf("forwarded put = (%q,%q,%d), want (svc/a,data,11)", ft.putKey, ft.putValue, ft.putLease)
	}
}

func TestClientBackend_PutWithLeaseError(t *testing.T) {
	boom := errors.New("put boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{putErr: boom}))
	if err := b.PutWithLease(context.Background(), "k", nil, 1); !errors.Is(err, boom) {
		t.Fatalf("PutWithLease err = %v, want %v", err, boom)
	}
}

func TestClientBackend_Scan(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	out, err := b.Scan(context.Background(), "svc/")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if ft.scannedPrefix != "svc/" {
		t.Fatalf("forwarded prefix = %q, want svc/", ft.scannedPrefix)
	}
	if string(out["svc/a"]) != "v" {
		t.Fatalf("Scan result = %v, want svc/a=v", out)
	}
}

func TestClientBackend_ScanError(t *testing.T) {
	boom := errors.New("scan boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{scanErr: boom}))
	if _, err := b.Scan(context.Background(), "svc/"); !errors.Is(err, boom) {
		t.Fatalf("Scan err = %v, want %v", err, boom)
	}
}

// TestClientBackend_WatchPrefix proves the backend requests PrevKV so the
// registry can recover the instance payload on lease-expiry deletes.
func TestClientBackend_WatchPrefix(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	ch, err := b.WatchPrefix(context.Background(), "svc/")
	if err != nil {
		t.Fatalf("WatchPrefix: %v", err)
	}
	if ch == nil {
		t.Fatal("WatchPrefix returned nil channel")
	}
	if ft.watchPrefix != "svc/" {
		t.Fatalf("forwarded prefix = %q, want svc/", ft.watchPrefix)
	}
	if !ft.watchOpts.PrevKV {
		t.Fatal("WatchPrefix must request PrevKV=true for lease-expiry recovery")
	}
}

func TestClientBackend_WatchPrefixError(t *testing.T) {
	boom := errors.New("watch boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{watchErr: boom}))
	if _, err := b.WatchPrefix(context.Background(), "svc/"); !errors.Is(err, boom) {
		t.Fatalf("WatchPrefix err = %v, want %v", err, boom)
	}
}
