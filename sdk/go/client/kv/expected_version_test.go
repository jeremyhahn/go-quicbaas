package kv

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestKVClient_RefusesExpectedVersion proves the SDK key-value client
// refuses ExpectedVersion instead of sending it: no SDK transport carries
// the field, so sending it would turn a conditional write into an
// unconditional one.
func TestKVClient_RefusesExpectedVersion(t *testing.T) {
	ctx := context.Background()
	expected := uint64(1)

	newRefusingClient := func(t *testing.T) Client {
		t.Helper()
		return NewClient(&mockTransport{
			requestFn: func(_ context.Context, svc, method string, _ interface{}) (interface{}, error) {
				t.Errorf("request reached the transport: %s.%s", svc, method)
				return nil, nil
			},
		})
	}

	t.Run("put", func(t *testing.T) {
		_, err := newRefusingClient(t).Put(ctx, &service.PutRequest{
			EntityType:      "users",
			ID:              1,
			Data:            []byte("value"),
			ExpectedVersion: &expected,
		})
		if !errors.IsInvalidInput(err) {
			t.Fatalf("Put() error = %v, want an invalid-input error", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		_, err := newRefusingClient(t).Delete(ctx, &service.DeleteRequest{
			EntityType:      "users",
			ID:              1,
			ExpectedVersion: &expected,
		})
		if !errors.IsInvalidInput(err) {
			t.Fatalf("Delete() error = %v, want an invalid-input error", err)
		}
	})
}
