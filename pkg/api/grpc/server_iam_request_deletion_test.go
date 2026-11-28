package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// gracePeriodRecordingIAMService records the grace period RequestDeletion
// hands the service. Every other method is the nil embedded interface and
// panics if reached.
type gracePeriodRecordingIAMService struct {
	IAMService
	called          bool
	gracePeriodDays int
}

func (s *gracePeriodRecordingIAMService) RequestDeletion(_ context.Context, tenantID string, userID, requestedBy uint64, _ string, _, _ []string, gracePeriodDays int) (*iam.DataDeletionRequest, error) {
	s.called = true
	s.gracePeriodDays = gracePeriodDays
	return &iam.DataDeletionRequest{ID: "del-1", TenantID: tenantID, UserID: userID, RequestedBy: requestedBy}, nil
}

// TestGRPC_RequestDeletionNeverSelectsImmediateErasure pins that the gRPC
// deletion request always keeps a grace period. A grace period of zero makes
// the GDPR manager erase the user's data at once, which is ProcessDeletion's
// work, and proto3 sends zero whenever the caller omits the field.
func TestGRPC_RequestDeletionNeverSelectsImmediateErasure(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		grace int32
		want  int
	}{
		{name: "omitted", grace: 0, want: iam.DefaultGracePeriodDays},
		{name: "explicit", grace: 7, want: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &gracePeriodRecordingIAMService{}
			srv := NewIAMServer(svc, log)
			_, err := srv.RequestDeletion(callerContext("acme"), &iampb.RequestDeletionRequest{
				TenantId:        "acme",
				UserId:          "42",
				RequestedBy:     "42",
				GracePeriodDays: tc.grace,
			})
			require.NoError(t, err)
			require.True(t, svc.called)
			assert.Equal(t, tc.want, svc.gracePeriodDays)
		})
	}

	t.Run("negative", func(t *testing.T) {
		svc := &gracePeriodRecordingIAMService{}
		srv := NewIAMServer(svc, log)
		_, err := srv.RequestDeletion(callerContext("acme"), &iampb.RequestDeletionRequest{
			TenantId:        "acme",
			UserId:          "42",
			RequestedBy:     "42",
			GracePeriodDays: -1,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.False(t, svc.called, "a refused request reached the GDPR manager")
	})
}
