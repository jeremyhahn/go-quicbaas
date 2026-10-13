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

package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

func TestReadTimeoutDuration(t *testing.T) {
	got, err := readTimeoutDuration(0)
	require.NoError(t, err)
	assert.Equal(t, defaultReadTimeout, got, "an unset timeout uses the default")

	got, err = readTimeoutDuration(250)
	require.NoError(t, err)
	assert.Equal(t, 250*time.Millisecond, got)

	maxMs := uint64(math.MaxInt64 / int64(time.Millisecond))
	got, err = readTimeoutDuration(maxMs)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(maxMs)*time.Millisecond, got)
}

func TestReadTimeoutDuration_TooLarge(t *testing.T) {
	maxMs := uint64(math.MaxInt64 / int64(time.Millisecond))
	for _, timeoutMs := range []uint64{maxMs + 1, math.MaxInt64 + 1, math.MaxUint64} {
		_, err := readTimeoutDuration(timeoutMs)
		var overflow *conv.ConversionOverflowError
		require.ErrorAs(t, err, &overflow, "timeout %d", timeoutMs)
	}
}

func TestKVService_Get_TimeoutTooLarge(t *testing.T) {
	svc := createTestService(t, withQRDBClientProvider(newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), &GetRequest{
		EntityType: "user",
		ID:         1,
		TimeoutMs:  math.MaxUint64,
	})
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	var overflow *conv.ConversionOverflowError
	assert.ErrorAs(t, err, &overflow)
}

func TestKVService_List_PageSizeBeyondInt(t *testing.T) {
	scanData := map[string][]byte{
		"users:1": []byte(`{"name":"alice"}`),
		"users:2": []byte(`{"name":"bob"}`),
		"users:3": []byte(`{"name":"charlie"}`),
	}
	svc := createTestService(t, withQRDBClientProvider(newSvcBoostMockQRDBProvider(scanData, nil)))
	defer svc.Close()

	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), &ListRequest{
		EntityType: "users",
		PageSize:   math.MaxUint64,
	}, stream)
	require.NoError(t, err)

	var responses []*ListResponse
	for resp := range stream {
		responses = append(responses, resp)
	}
	require.Len(t, responses, 1, "a page size beyond int returns everything in one page")
	assert.Len(t, responses[0].Entities, 3)
	assert.Empty(t, responses[0].NextPageToken)
}

func TestBackupService_ListBackups_LimitBeyondInt(t *testing.T) {
	var gotLimit int
	mockBackup := &mockBackupManager{
		listBackupsFn: func(ctx context.Context, limit int) (interface{}, error) {
			gotLimit = limit
			return &backup.BackupList{
				Backups:    []*backup.Backup{{ID: "backup-1", Size: -1, Timestamp: time.Now()}},
				TotalCount: -1,
			}, nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.ListBackups(tenantctx.WithSystem(context.Background()), &ListBackupsRequest{Limit: math.MaxUint64})
	require.NoError(t, err)
	assert.Equal(t, math.MaxInt, gotLimit, "a limit beyond int is clamped, not wrapped negative")
	require.Len(t, resp.Backups, 1)
	assert.Zero(t, resp.Backups[0].Size, "a negative size is reported as zero")
	assert.Zero(t, resp.TotalCount, "a negative count is reported as zero")
}

func TestTimeSeriesEntityID(t *testing.T) {
	tests := []struct {
		name   string
		entity map[string]interface{}
		want   uint64
	}{
		{name: "id field", entity: map[string]interface{}{"id": float64(42)}, want: 42},
		{name: "entity_id field", entity: map[string]interface{}{"entity_id": float64(7)}, want: 7},
		{name: "non-numeric id falls back to entity_id", entity: map[string]interface{}{"id": "x", "entity_id": float64(9)}, want: 9},
		{name: "largest exact float", entity: map[string]interface{}{"id": float64(1 << 53)}, want: 1 << 53},
		{name: "absent", entity: map[string]interface{}{}, want: 0},
		{name: "negative", entity: map[string]interface{}{"id": float64(-1)}, want: 0},
		{name: "fractional", entity: map[string]interface{}{"id": 1.5}, want: 0},
		{name: "beyond uint64", entity: map[string]interface{}{"id": 1e20}, want: 0},
		{name: "not a number", entity: map[string]interface{}{"id": math.NaN()}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, timeSeriesEntityID(tt.entity))
		})
	}
}

func TestTimeSeriesQuery_LimitBatching(t *testing.T) {
	tests := []struct {
		name       string
		points     int
		limit      uint64
		wantPages  []int
		wantTokens []string
	}{
		{name: "limit inside first batch", points: 150, limit: 30, wantPages: []int{30}, wantTokens: []string{""}},
		{name: "limit spans batches", points: 250, limit: 150, wantPages: []int{100, 50}, wantTokens: []string{"100", ""}},
		{name: "limit beyond int", points: 120, limit: math.MaxUint64, wantPages: []int{100, 20}, wantTokens: []string{"100", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entries [][]byte
			for i := 1; i <= tt.points; i++ {
				j, err := json.Marshal(map[string]interface{}{"timestamp": float64(i), "value": float64(i)})
				require.NoError(t, err)
				entries = append(entries, j)
			}
			svc := &quicBaaS{logger: createTestLogger(t), qrdbClient: newSvcBoostMockQRDBProvider(sliceToCannedScanResult(entries), nil)}
			tsSvc := &timeSeriesService{svc: svc}

			stream := make(chan *TimeSeriesQueryResponse, 10)
			err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), &TimeSeriesQueryRequest{
				EntityType: "metrics",
				StartTime:  1,
				EndTime:    int64(tt.points),
				Limit:      tt.limit,
			}, stream)
			require.NoError(t, err)

			var pages []int
			var tokens []string
			for resp := range stream {
				pages = append(pages, len(resp.DataPoints))
				tokens = append(tokens, resp.NextPageToken)
				assert.Equal(t, uint64(tt.points), resp.TotalCount)
			}
			assert.Equal(t, tt.wantPages, pages)
			assert.Equal(t, tt.wantTokens, tokens)
		})
	}
}
