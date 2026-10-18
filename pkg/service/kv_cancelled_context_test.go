package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// deadContexts returns a cancelled context and one past its deadline, each
// paired with the error errors.Is must match (#1583).
func deadContexts(t *testing.T) map[string]struct {
	ctx  context.Context
	want error
} {
	t.Helper()
	cancelled, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	cancel()
	expired, cancelExpired := context.WithDeadline(tenantctx.WithSystem(context.Background()), time.Unix(0, 0))
	t.Cleanup(cancelExpired)
	return map[string]struct {
		ctx  context.Context
		want error
	}{
		"cancelled": {ctx: cancelled, want: context.Canceled},
		"expired":   {ctx: expired, want: context.DeadlineExceeded},
	}
}

func TestKVPut_DeadContext_WritesNothing(t *testing.T) {
	for name, tc := range deadContexts(t) {
		t.Run(name, func(t *testing.T) {
			qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
			svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
			defer svc.Close()

			resp, err := svc.Put(tc.ctx, &PutRequest{EntityType: "User", ID: 42, Data: []byte(`{}`)})

			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, resp)
			assert.Empty(t, qrdbProvider.capturedPutKey(), "a dead context must not reach the qrdb write")
		})
	}
}

func TestKVGet_DeadContext_ReadsNothing(t *testing.T) {
	for name, tc := range deadContexts(t) {
		t.Run(name, func(t *testing.T) {
			qrdbProvider := newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)
			svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
			defer svc.Close()

			resp, err := svc.Get(tc.ctx, &GetRequest{EntityType: "User", ID: 42})

			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, resp)
			assert.Empty(t, qrdbProvider.capturedGetKey(), "a dead context must not reach the qrdb read")
		})
	}
}

func TestKVDelete_DeadContext_DeletesNothing(t *testing.T) {
	for name, tc := range deadContexts(t) {
		t.Run(name, func(t *testing.T) {
			qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
			svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
			defer svc.Close()

			resp, err := svc.Delete(tc.ctx, &DeleteRequest{EntityType: "User", ID: 42})

			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, resp)
			assert.Empty(t, qrdbProvider.capturedDeleteKey(), "a dead context must not reach the qrdb delete")
		})
	}
}

func TestKV_LiveContext_ReachesQRDB(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	qrdbProvider := newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	_, err := svc.Put(ctx, &PutRequest{EntityType: "User", ID: 42, Data: []byte(`{}`)})
	require.NoError(t, err)
	assert.Equal(t, "User:42", qrdbProvider.capturedPutKey())

	got, err := svc.Get(ctx, &GetRequest{EntityType: "User", ID: 42})
	require.NoError(t, err)
	assert.True(t, got.Found)

	_, err = svc.Delete(ctx, &DeleteRequest{EntityType: "User", ID: 42})
	require.NoError(t, err)
	assert.Equal(t, "User:42", qrdbProvider.capturedDeleteKey())
}

func TestContextError(t *testing.T) {
	assert.NoError(t, contextError(context.Background(), "service.Put"))

	for name, tc := range deadContexts(t) {
		t.Run(name, func(t *testing.T) {
			err := contextError(tc.ctx, "service.Put")
			require.ErrorIs(t, err, tc.want)
		})
	}
}
