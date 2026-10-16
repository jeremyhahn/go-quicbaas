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
// those interfaces to satisfy the assertions and overrides only the seven
// methods backend.go calls. Embedding leaves the unused methods nil, but
// they are never invoked by the clientBackend, so this stays a faithful
// adapter test rather than a partial reimplementation.
type fakeTransport struct {
	qrdbtransport.LeaseClient
	qrdbtransport.MVCCClient
	qrdbtransport.KVClient
	qrdbtransport.WatchClient
	qrdbtransport.AdminClient

	// caps is what Capabilities reports; nil reports a raft node.
	caps    *qrdbtransport.CapabilitiesResponse
	capsErr error

	grantedTTL  int64
	keepAliveID int64
	revokedID   int64
	putKey      string
	putValue    []byte
	putLease    int64
	existsKey   string
	rangeStart  []byte
	rangeEnd    []byte
	watchPrefix string
	watchOpts   qrdbtransport.WatchOptions

	grantErr     error
	keepAliveErr error
	revokeErr    error
	putErr       error
	existsErr    error
	rangeErr     error
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

func (f *fakeTransport) Capabilities(_ context.Context) (*qrdbtransport.CapabilitiesResponse, error) {
	if f.capsErr != nil {
		return nil, f.capsErr
	}
	if f.caps != nil {
		return f.caps, nil
	}
	return &raftCapabilities, nil
}

// raftCapabilities is what a raft qrdb node reports for the planes the
// registry needs.
var raftCapabilities = qrdbtransport.CapabilitiesResponse{
	Mode: "raft", KV: true, MVCC: true, Watch: true, Lease: true,
}

// standaloneCapabilities is what a standalone qrdb node reports: plain KV,
// with the raft-only lease, MVCC and watch planes gated off.
var standaloneCapabilities = qrdbtransport.CapabilitiesResponse{
	Mode: "standalone", KV: true,
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
	return &qrdbtransport.LeaseInfo{ID: leaseID, TTL: 15}, nil
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

// MVCCClient override: the range read Resolve is served from.
func (f *fakeTransport) Range(_ context.Context, start, end []byte, _ qrdbtransport.RangeOptions, _ ...qrdbtransport.ReadOption) (*qrdbtransport.RangeResponse, error) {
	if f.rangeErr != nil {
		return nil, f.rangeErr
	}
	f.rangeStart = start
	f.rangeEnd = end
	return &qrdbtransport.RangeResponse{
		Kvs:      []*qrdbtransport.MVCCKeyValue{{Key: append(append([]byte(nil), start...), 'a'), Value: []byte("v")}, nil},
		Count:    1,
		Revision: 77,
	}, nil
}

// KVClient override: the lease-ownership check.
func (f *fakeTransport) Exists(_ context.Context, key string, _ ...qrdbtransport.ReadOption) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	f.existsKey = key
	return key == "owned", nil
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
	r, err := NewFromClient(context.Background(), newClientWith(t, &fakeTransport{}))
	if err != nil {
		t.Fatalf("NewFromClient on a raft node: %v", err)
	}
	if r == nil {
		t.Fatal("NewFromClient returned nil Registry")
	}
}

func TestNewFromClient_RefusesStandaloneNode(t *testing.T) {
	ft := &fakeTransport{caps: &standaloneCapabilities}
	r, err := NewFromClient(context.Background(), newClientWith(t, ft))
	if r != nil {
		t.Fatal("NewFromClient returned a Registry over a standalone node")
	}
	var raftErr *RegistryRequiresRaftError
	if !errors.As(err, &raftErr) {
		t.Fatalf("err = %v (%T), want *RegistryRequiresRaftError", err, err)
	}
	if raftErr.Mode != "standalone" {
		t.Fatalf("Mode = %q, want %q", raftErr.Mode, "standalone")
	}
	if ft.grantedTTL != 0 {
		t.Fatal("NewFromClient reached the lease plane before refusing")
	}
}

func TestNewFromClient_RefusesNodeMissingAnyPlane(t *testing.T) {
	cases := map[string]qrdbtransport.CapabilitiesResponse{
		"no lease": {Mode: "raft", MVCC: true, Watch: true},
		"no mvcc":  {Mode: "raft", Lease: true, Watch: true},
		"no watch": {Mode: "raft", Lease: true, MVCC: true},
	}
	for name, caps := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewFromClient(context.Background(), newClientWith(t, &fakeTransport{caps: &caps}))
			var raftErr *RegistryRequiresRaftError
			if !errors.As(err, &raftErr) {
				t.Fatalf("err = %v (%T), want *RegistryRequiresRaftError", err, err)
			}
		})
	}
}

func TestNewFromClient_CapabilitiesError(t *testing.T) {
	boom := errors.New("capabilities boom")
	_, err := NewFromClient(context.Background(), newClientWith(t, &fakeTransport{capsErr: boom}))
	var regErr *RegistryError
	if !errors.As(err, &regErr) || regErr.Op != "capabilities" {
		t.Fatalf("err = %v (%T), want *RegistryError{Op: capabilities}", err, err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the capabilities failure", err)
	}
}

func TestRegistryRequiresRaftError_NamesMode(t *testing.T) {
	err := &RegistryRequiresRaftError{Mode: "standalone"}
	want := `registry: service discovery requires a raft qrdb node, but the node runs in "standalone" mode`
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
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
	ttl, err := b.KeepAlive(context.Background(), 7)
	if err != nil {
		t.Fatalf("KeepAlive: %v", err)
	}
	if ft.keepAliveID != 7 {
		t.Fatalf("forwarded leaseID = %d, want 7", ft.keepAliveID)
	}
	if ttl != 15 {
		t.Fatalf("KeepAlive ttl = %d, want the LeaseInfo TTL 15", ttl)
	}
}

func TestClientBackend_KeepAliveError(t *testing.T) {
	boom := errors.New("keepalive boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{keepAliveErr: boom}))
	if _, err := b.KeepAlive(context.Background(), 7); !errors.Is(err, boom) {
		t.Fatalf("KeepAlive err = %v, want %v", err, boom)
	}
}

func TestClientBackend_Exists(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	owned, err := b.Exists(context.Background(), "owned")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !owned || ft.existsKey != "owned" {
		t.Fatalf("Exists = %v (key %q), want true for the forwarded key", owned, ft.existsKey)
	}
	missing, err := b.Exists(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if missing {
		t.Fatal("Exists reported a missing key as present")
	}
}

func TestClientBackend_ExistsError(t *testing.T) {
	boom := errors.New("exists boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{existsErr: boom}))
	if _, err := b.Exists(context.Background(), "k"); !errors.Is(err, boom) {
		t.Fatalf("Exists err = %v, want %v", err, boom)
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

// TestClientBackend_Range proves the prefix is read as the half-open range
// [prefix, prefixEnd) and that the read's revision reaches the registry, which
// is what makes a Resolve-then-Watch handoff gap-free.
func TestClientBackend_Range(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	out, revision, err := b.Range(context.Background(), "svc/")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if string(ft.rangeStart) != "svc/" || string(ft.rangeEnd) != "svc0" {
		t.Fatalf("forwarded range = [%q, %q), want [svc/, svc0)", ft.rangeStart, ft.rangeEnd)
	}
	if string(out["svc/a"]) != "v" || len(out) != 1 {
		t.Fatalf("Range result = %v, want only svc/a=v (nil entries skipped)", out)
	}
	if revision != 77 {
		t.Fatalf("Range revision = %d, want 77", revision)
	}
}

func TestClientBackend_RangeError(t *testing.T) {
	boom := errors.New("range boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{rangeErr: boom}))
	if _, _, err := b.Range(context.Background(), "svc/"); !errors.Is(err, boom) {
		t.Fatalf("Range err = %v, want %v", err, boom)
	}
}

func TestPrefixEnd(t *testing.T) {
	cases := map[string]string{
		"svc/":           "svc0",
		"a":              "b",
		"a\xff":          "b",
		"\xff\xff":       "",
		"__sys:svc-t/x/": "__sys:svc-t/x0",
	}
	for prefix, want := range cases {
		if got := string(prefixEnd(prefix)); got != want {
			t.Errorf("prefixEnd(%q) = %q, want %q", prefix, got, want)
		}
	}
}

// TestClientBackend_WatchPrefix proves the backend requests PrevKV so the
// registry can recover the instance payload on lease-expiry deletes, and
// forwards the start revision a resumed watch asks for.
func TestClientBackend_WatchPrefix(t *testing.T) {
	ft := &fakeTransport{}
	b := NewClientBackend(newClientWith(t, ft))
	ch, err := b.WatchPrefix(context.Background(), "svc/", 41)
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
	if ft.watchOpts.StartRevision != 41 {
		t.Fatalf("forwarded start revision = %d, want 41", ft.watchOpts.StartRevision)
	}
}

func TestClientBackend_WatchPrefixError(t *testing.T) {
	boom := errors.New("watch boom")
	b := NewClientBackend(newClientWith(t, &fakeTransport{watchErr: boom}))
	if _, err := b.WatchPrefix(context.Background(), "svc/", 0); !errors.Is(err, boom) {
		t.Fatalf("WatchPrefix err = %v, want %v", err, boom)
	}
}
