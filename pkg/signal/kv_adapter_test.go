//go:build quicbaas_signal

package signal

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// testQRDBKVClient is an in-memory implementation of the go-qrdb KVClient
// interface, with variadic options that are ignored.
type testQRDBKVClient struct {
	data map[string][]byte
}

func newTestQRDBKVClient() *testQRDBKVClient {
	return &testQRDBKVClient{data: make(map[string][]byte)}
}

func (c *testQRDBKVClient) Put(_ context.Context, key string, value []byte, _ ...qrdbtransport.WriteOption) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	c.data[key] = cp
	return nil
}

func (c *testQRDBKVClient) Get(_ context.Context, key string, _ ...qrdbtransport.ReadOption) ([]byte, error) {
	v, ok := c.data[key]
	if !ok {
		return nil, errors.New("key not found: " + key)
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (c *testQRDBKVClient) Delete(_ context.Context, key string, _ ...qrdbtransport.WriteOption) error {
	if _, ok := c.data[key]; !ok {
		return errors.New("key not found: " + key)
	}
	delete(c.data, key)
	return nil
}

func (c *testQRDBKVClient) List(_ context.Context, prefix string, _ ...qrdbtransport.ReadOption) ([]string, error) {
	var keys []string
	for k := range c.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (c *testQRDBKVClient) Scan(_ context.Context, prefix string, _ ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for k, v := range c.data {
		if strings.HasPrefix(k, prefix) {
			cp := make([]byte, len(v))
			copy(cp, v)
			result[k] = cp
		}
	}
	return result, nil
}

func (c *testQRDBKVClient) Exists(_ context.Context, key string, _ ...qrdbtransport.ReadOption) (bool, error) {
	_, ok := c.data[key]
	return ok, nil
}

// Compile-time check.
var _ qrdbtransport.KVClient = (*testQRDBKVClient)(nil)

func TestKVAdapter_Put(t *testing.T) {
	inner := newTestQRDBKVClient()
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	if err := adapter.Put(ctx, "k1", []byte("v1")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if string(inner.data["k1"]) != "v1" {
		t.Fatalf("expected v1 in inner store, got %s", string(inner.data["k1"]))
	}
}

func TestKVAdapter_Put_Overwrite(t *testing.T) {
	inner := newTestQRDBKVClient()
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	if err := adapter.Put(ctx, "k1", []byte("v1")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if err := adapter.Put(ctx, "k1", []byte("v2")); err != nil {
		t.Fatalf("Put overwrite failed: %v", err)
	}
	if string(inner.data["k1"]) != "v2" {
		t.Fatalf("expected v2 after overwrite, got %s", string(inner.data["k1"]))
	}
}

func TestKVAdapter_Get(t *testing.T) {
	inner := newTestQRDBKVClient()
	inner.data["k1"] = []byte("v1")
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	val, err := adapter.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "v1" {
		t.Fatalf("expected v1, got %s", string(val))
	}
}

func TestKVAdapter_Get_NotFound(t *testing.T) {
	inner := newTestQRDBKVClient()
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	_, err := adapter.Get(ctx, "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestKVAdapter_Delete(t *testing.T) {
	inner := newTestQRDBKVClient()
	inner.data["k1"] = []byte("v1")
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	if err := adapter.Delete(ctx, "k1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, ok := inner.data["k1"]; ok {
		t.Fatal("expected key to be deleted")
	}
}

func TestKVAdapter_Delete_NotFound(t *testing.T) {
	inner := newTestQRDBKVClient()
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	err := adapter.Delete(ctx, "missing")
	if err == nil {
		t.Fatal("expected error for deleting missing key")
	}
}

func TestKVAdapter_List(t *testing.T) {
	inner := newTestQRDBKVClient()
	inner.data["prefix/a"] = []byte("1")
	inner.data["prefix/b"] = []byte("2")
	inner.data["other/c"] = []byte("3")
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	keys, err := adapter.List(ctx, "prefix/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestKVAdapter_List_Empty(t *testing.T) {
	inner := newTestQRDBKVClient()
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	keys, err := adapter.List(ctx, "nonexistent/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

func TestKVAdapter_Scan(t *testing.T) {
	inner := newTestQRDBKVClient()
	inner.data["scan/a"] = []byte("1")
	inner.data["scan/b"] = []byte("2")
	inner.data["other/c"] = []byte("3")
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	var collected []string
	err := adapter.Scan(ctx, "scan/", func(key string, value []byte) error {
		collected = append(collected, key+"="+string(value))
		return nil
	})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(collected))
	}
}

func TestKVAdapter_Scan_CallbackError(t *testing.T) {
	inner := newTestQRDBKVClient()
	inner.data["scan/a"] = []byte("1")
	inner.data["scan/b"] = []byte("2")
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	stopErr := errors.New("stop iteration")
	callCount := 0
	err := adapter.Scan(ctx, "scan/", func(key string, value []byte) error {
		callCount++
		return stopErr
	})
	if !errors.Is(err, stopErr) {
		t.Fatalf("expected stop error, got %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected callback called once before stop, got %d", callCount)
	}
}

func TestKVAdapter_Scan_Empty(t *testing.T) {
	inner := newTestQRDBKVClient()
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	err := adapter.Scan(ctx, "empty/", func(key string, value []byte) error {
		t.Fatal("callback should not be called for empty scan")
		return nil
	})
	if err != nil {
		t.Fatalf("Scan empty failed: %v", err)
	}
}

func TestKVAdapter_Exists_Found(t *testing.T) {
	inner := newTestQRDBKVClient()
	inner.data["k1"] = []byte("v1")
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	exists, err := adapter.Exists(ctx, "k1")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Fatal("expected key to exist")
	}
}

func TestKVAdapter_Exists_NotFound(t *testing.T) {
	inner := newTestQRDBKVClient()
	adapter := NewKVAdapter(inner)
	ctx := context.Background()

	exists, err := adapter.Exists(ctx, "missing")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Fatal("expected key to not exist")
	}
}
