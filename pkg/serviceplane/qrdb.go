package serviceplane

import (
	"context"
	"errors"
	"sort"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// QRDBReader adapts a go-qrdb KV client to the store's Reader. A key the
// client reports as not found reads as absent, so the store answers
// ErrNotFound for it rather than ErrStore.
func QRDBReader(kv transport.KVClient) Reader {
	if kv == nil {
		return nil
	}
	return qrdbReader{kv: kv}
}

type qrdbReader struct {
	kv transport.KVClient
}

func (r qrdbReader) Get(ctx context.Context, key string) ([]byte, error) {
	raw, err := r.kv.Get(ctx, key)
	var missing *transport.KeyNotFoundError
	if errors.As(err, &missing) {
		return nil, nil
	}
	return raw, err
}

// Scan visits the records under prefix in key order: go-qrdb's Scan answers
// a map, and List callers rely on a stable order.
func (r qrdbReader) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	records, err := r.kv.Scan(ctx, prefix)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := fn(k, records[k]); err != nil {
			return err
		}
	}
	return nil
}

func (r qrdbReader) Delete(ctx context.Context, key string) error {
	return r.kv.Delete(ctx, key)
}
