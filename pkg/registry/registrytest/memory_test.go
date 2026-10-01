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

package registrytest

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
)

var _ registry.Backend = (*MemoryBackend)(nil)

func recv(t *testing.T, ch <-chan qrdbtransport.WatchResponse) qrdbtransport.WatchResponse {
	t.Helper()
	select {
	case resp, ok := <-ch:
		if !ok {
			t.Fatal("watch channel closed")
		}
		return resp
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a watch response")
		return qrdbtransport.WatchResponse{}
	}
}

func TestMemoryBackend_LeasesOwnTheirKeys(t *testing.T) {
	m := NewMemoryBackend()
	ctx := context.Background()
	lease, err := m.Grant(ctx, 9)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := m.PutWithLease(ctx, "svc/a", []byte("v"), lease); err != nil {
		t.Fatalf("PutWithLease: %v", err)
	}
	if ttl, err := m.KeepAlive(ctx, lease); err != nil || ttl != 9 {
		t.Fatalf("KeepAlive = %d, %v", ttl, err)
	}
	if ok, err := m.Exists(ctx, "svc/a"); err != nil || !ok {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	pairs, rev, err := m.Range(ctx, "svc/")
	if err != nil || string(pairs["svc/a"]) != "v" || rev != 1 {
		t.Fatalf("Range = %v @%d, %v", pairs, rev, err)
	}
	if err := m.Expire(lease); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if ok, err := m.Exists(ctx, "svc/a"); err != nil || ok {
		t.Fatalf("a revoked lease's key survived: %v, %v", ok, err)
	}
	if m.Revision() != 2 {
		t.Fatalf("revision = %d, want 2 (put, delete)", m.Revision())
	}

	var missing *LeaseNotFoundError
	if _, err := m.KeepAlive(ctx, lease); !errors.As(err, &missing) {
		t.Fatalf("KeepAlive on a revoked lease = %v", err)
	}
	if err := m.Revoke(ctx, lease); !errors.As(err, &missing) {
		t.Fatalf("Revoke twice = %v", err)
	}
	if err := m.PutWithLease(ctx, "svc/b", nil, 99); !errors.As(err, &missing) || missing.Error() == "" {
		t.Fatalf("Put under an unknown lease = %v", err)
	}
}

func TestMemoryBackend_WatchReplaysThenFollows(t *testing.T) {
	m := NewMemoryBackend()
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := m.Grant(ctx, 5)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	for _, key := range []string{"svc/a", "other/x", "svc/b"} {
		if err := m.PutWithLease(ctx, key, []byte(key), lease); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	ch, err := m.WatchPrefix(ctx, "svc/", 2)
	if err != nil {
		t.Fatalf("WatchPrefix: %v", err)
	}
	if replayed := recv(t, ch); replayed.Revision != 3 || string(replayed.Events[0].Kv.Key) != "svc/b" {
		t.Fatalf("replay = %+v, want only svc/b at revision 3", replayed)
	}
	if err := m.Expire(lease); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	for i := 0; i < 2; i++ {
		live := recv(t, ch)
		if live.Events[0].Type != qrdbtransport.EventDelete || live.Events[0].PrevKv == nil {
			t.Fatalf("live = %+v, want a delete carrying the previous value", live)
		}
	}
	m.CancelWatches("evicted")
	if ended := recv(t, ch); !ended.Canceled || ended.CancelReason != "evicted" {
		t.Fatalf("cancel = %+v", ended)
	}
	cancel()
	select {
	case _, open := <-ch:
		for open {
			_, open = <-ch
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch channel not closed with its context")
	}
}

func TestMemoryBackend_CompactionRefusesOldStarts(t *testing.T) {
	m := NewMemoryBackend()
	ctx := context.Background()
	lease, err := m.Grant(ctx, 5)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	for _, key := range []string{"k1", "k2", "k3"} {
		if err := m.PutWithLease(ctx, key, nil, lease); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	m.Compact(2)
	_, err = m.WatchPrefix(ctx, "k", 2)
	var compacted *CompactedError
	if !errors.As(err, &compacted) {
		t.Fatalf("watch from a compacted revision = %v", err)
	}
	if _, ok := qrdbtransport.ClassifyWatchError(err).(*qrdbtransport.WatchCompactedError); !ok {
		t.Fatalf("%v is not recognised by qrdb's watch classifier", err)
	}
	ch, err := m.WatchPrefix(ctx, "k", 3)
	if err != nil {
		t.Fatalf("watch from a retained revision: %v", err)
	}
	if resp := recv(t, ch); resp.Revision != 3 {
		t.Fatalf("replay = %+v, want revision 3", resp)
	}
}
