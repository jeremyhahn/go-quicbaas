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
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// recordingKVService is a go-qrdb KV servicer that stores what it is given,
// so a test can see a write made through QRDBData().KV() land in the
// embedded go-qrdb node's servicer.
type recordingKVService struct {
	stubKVService
	mu   sync.Mutex
	data map[string][]byte
}

func (s *recordingKVService) put(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), value...)
	return nil
}

func (s *recordingKVService) get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return nil, stubErr
	}
	return v, nil
}

func (s *recordingKVService) Put(_ context.Context, key string, value []byte) error {
	return s.put(key, value)
}

func (s *recordingKVService) PutWithMode(_ context.Context, key string, value []byte, _ qrdbsdk.WriteMode) error {
	return s.put(key, value)
}

func (s *recordingKVService) Get(_ context.Context, key string) ([]byte, error) {
	return s.get(key)
}

func (s *recordingKVService) GetWithConsistency(_ context.Context, key string, _ qrdbsdk.ReadOptions) ([]byte, error) {
	return s.get(key)
}

// TestQRDBData_ReturnsGoQRDBClients proves the six data services are
// go-qrdb's own clients: each accessor of QRDBData() returns exactly what
// the go-qrdb SDK client this QuicBaaS client carries returns.
func TestQRDBData_ReturnsGoQRDBClients(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    &recordingKVService{},
		Admin: stubAdminService{},
	})

	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}
	qrdb, err := c.qrdbClient()
	if err != nil {
		t.Fatalf("qrdbClient(): %v", err)
	}
	if got, ok := data.(*QRDBClient); !ok || got != qrdb {
		t.Fatalf("QRDBData() = %T, want the go-qrdb SDK client the QuicBaaS client carries", data)
	}

	checks := []struct {
		name string
		got  any
	}{
		{"KV", data.KV()},
		{"Batch", data.Batch()},
		{"Transaction", data.Transaction()},
		{"MVCC", data.MVCC()},
		{"Collections", data.Collections()},
		{"Index", data.Index()},
	}
	for _, ck := range checks {
		if ck.got == nil {
			t.Errorf("QRDBData().%s() is nil", ck.name)
		}
	}
}

// TestQRDBData_KVReachesEmbeddedNode proves a write and a read through
// QRDBData().KV() are served by the embedded go-qrdb node's KV servicer.
func TestQRDBData_KVReachesEmbeddedNode(t *testing.T) {
	kv := &recordingKVService{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    kv,
		Admin: stubAdminService{},
	})

	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}
	ctx := context.Background()
	if err := data.KV().Put(ctx, "row1652/key", []byte("value")); err != nil {
		t.Fatalf("KV().Put: %v", err)
	}
	if got, err := kv.get("row1652/key"); err != nil || string(got) != "value" {
		t.Fatalf("servicer holds %q (err %v), want %q", got, err, "value")
	}
	got, err := data.KV().Get(ctx, "row1652/key")
	if err != nil {
		t.Fatalf("KV().Get: %v", err)
	}
	if string(got) != "value" {
		t.Fatalf("KV().Get = %q, want %q", got, "value")
	}
}

// TestQRDBData_PropagatesQRDBInitError proves QRDBData reports the go-qrdb
// client's construction error instead of returning a nil service.
func TestQRDBData_PropagatesQRDBInitError(t *testing.T) {
	c := &client{opts: defaultOptions(), address: "embedded://local"}
	c.opts.transportType = TransportType(999)
	c.address = ""

	data, err := c.QRDBData()
	if err == nil && data == nil {
		t.Fatal("QRDBData() returned neither a service nor an error")
	}
}
