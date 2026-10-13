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

package grpc

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// requireClientRangeError asserts that err is an invalid-input QBError whose
// cause is a RangeError naming field.
func requireClientRangeError(t *testing.T, err error, field string) {
	t.Helper()
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	var rangeErr *api.RangeError
	require.ErrorAs(t, err, &rangeErr)
	assert.Equal(t, field, rangeErr.Field)
}

// TestClientKMS_RequestOutOfRange proves that a caller value too large for
// its int32 wire field is rejected as invalid input before the client
// touches the connection, instead of being sent as a wrapped integer.
func TestClientKMS_RequestOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	client := &Client{}

	tests := []struct {
		name  string
		field string
		call  func() error
	}{
		{
			name:  "ListKeys limit",
			field: "limit",
			call: func() error {
				_, err := client.KMSListKeys(ctx, &service.ListKeysRequest{Limit: tooBig})
				return err
			},
		},
		{
			name:  "DeleteKey pending window days",
			field: "pending_window_days",
			call: func() error {
				_, err := client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{KeyID: "k1", PendingWindowDays: tooBig})
				return err
			},
		},
		{
			name:  "ListKeyVersions limit",
			field: "limit",
			call: func() error {
				_, err := client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{KeyID: "k1", Limit: tooBig})
				return err
			},
		},
		{
			name:  "SetKeySafety warning percent",
			field: "warning_percent",
			call: func() error {
				_, err := client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{KeyID: "k1", WarningPercent: tooBig})
				return err
			},
		},
		{
			name:  "CreateDEK warning percent",
			field: "warning_percent",
			call: func() error {
				_, err := client.KMSCreateDEK(ctx, &service.CreateDEKRequest{WarningPercent: tooBig})
				return err
			},
		},
		{
			name:  "ListDEKs limit",
			field: "limit",
			call: func() error {
				_, err := client.KMSListDEKs(ctx, &service.ListDEKsRequest{Limit: tooBig})
				return err
			},
		},
		{
			name:  "SetDEKSafety warning percent",
			field: "warning_percent",
			call: func() error {
				_, err := client.KMSSetDEKSafety(ctx, &service.SetDEKSafetyRequest{WarningPercent: tooBig})
				return err
			},
		},
		{
			name:  "StartReencryption batch size",
			field: "batch_size",
			call: func() error {
				_, err := client.KMSStartReencryption(ctx, &service.StartReencryptionRequest{BatchSize: tooBig})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireClientRangeError(t, tt.call(), tt.field)
		})
	}
}

// TestConvertProtoReencryptionProgress_NegativeCount proves that a server
// reporting a negative key count is surfaced as an internal decode error
// instead of a wrapped unsigned count.
func TestConvertProtoReencryptionProgress_NegativeCount(t *testing.T) {
	tests := []struct {
		name     string
		progress *pb.ReencryptionProgress
	}{
		{name: "total keys", progress: &pb.ReencryptionProgress{TotalKeys: -1}},
		{name: "processed keys", progress: &pb.ReencryptionProgress{ProcessedKeys: -1}},
		{name: "failed keys", progress: &pb.ReencryptionProgress{FailedKeys: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertProtoReencryptionProgress(tt.progress)

			assert.Nil(t, result)
			var qbErr *dberrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
			var negative *conv.ConversionNegativeError
			require.ErrorAs(t, err, &negative)
		})
	}
}
