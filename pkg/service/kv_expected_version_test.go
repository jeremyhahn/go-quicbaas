package service

import (
	"context"
	stderrors "errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestKVService_ExpectedVersion_RefusesOutOfRange proves an expectedVersion
// above math.MaxInt64, which no stored version can equal, is refused as a
// version conflict and never reaches the store.
func TestKVService_ExpectedVersion_RefusesOutOfRange(t *testing.T) {
	outOfRange := uint64(math.MaxInt64) + 1
	ctx := tenantctx.WithSystem(context.Background())

	t.Run("put", func(t *testing.T) {
		qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
		svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
		defer svc.Close()

		resp, err := svc.Put(ctx, &PutRequest{EntityType: "User", ID: 42, Data: []byte(`{}`), ExpectedVersion: &outOfRange})

		assertVersionConflict(t, err, outOfRange)
		assert.Nil(t, resp)
		assert.Empty(t, qrdbProvider.capturedPutKey(), "an out-of-range expectedVersion must not reach the qrdb write")
	})

	t.Run("delete", func(t *testing.T) {
		qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
		svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
		defer svc.Close()

		resp, err := svc.Delete(ctx, &DeleteRequest{EntityType: "User", ID: 42, ExpectedVersion: &outOfRange})

		assertVersionConflict(t, err, outOfRange)
		assert.Nil(t, resp)
	})

	t.Run("helper", func(t *testing.T) {
		conflict := &KVVersionConflictError{EntityType: "User", ID: 42, Expected: outOfRange}
		_, err := expectedVersionCompare("service.Put", outOfRange, conflict)
		assertVersionConflict(t, err, outOfRange)

		got, err := expectedVersionCompare("service.Put", math.MaxInt64, conflict)
		require.NoError(t, err)
		assert.Equal(t, int64(math.MaxInt64), got)
	})
}

func assertVersionConflict(t *testing.T, err error, expected uint64) {
	t.Helper()
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "want a *errors.QBError, got %v", err)
	assert.Equal(t, errors.ErrConflict, qbErr.Code)
	var conflict *KVVersionConflictError
	require.True(t, stderrors.As(err, &conflict), "want a *KVVersionConflictError cause, got %v", err)
	assert.Equal(t, expected, conflict.Expected)
}
