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

package quic

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBackupService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.Backup", true},
		{"Backup", true},
		{"quicbaas.IAM", false},
		{"quicbaas.Cluster", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isBackupService(tt.svc), tt.svc)
	}
}

// TestDecodeBackupResponse_List proves the wire shape produced by the server
// (EncodePayload of the canonical service.ListBackupsResponse) round-trips back
// into the same canonical type — the exact decode the CLI sdkBackupClient.List
// relies on. This is the regression guard for the "unexpected response type"
// bug where the QUIC transport fell through to a raw map[string]interface{}.
func TestDecodeBackupResponse_List(t *testing.T) {
	want := &service.ListBackupsResponse{
		Backups: []*service.BackupMetadata{
			{BackupID: "bk-1", Name: "nightly", Size: 4096, Timestamp: 1700000000, Status: "completed"},
			{BackupID: "bk-2", Name: "weekly", Size: 8192, Timestamp: 1700100000, Status: "completed"},
		},
		TotalCount: 2,
	}

	// Emit exactly what the server handler writes on the wire.
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeBackupResponse("List", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.ListBackupsResponse)
	require.True(t, ok, "expected *service.ListBackupsResponse, got %T", got)
	assert.Equal(t, want.TotalCount, resp.TotalCount)
	require.Len(t, resp.Backups, 2)
	assert.Equal(t, "bk-1", resp.Backups[0].BackupID)
	assert.Equal(t, uint64(4096), resp.Backups[0].Size)
	assert.Equal(t, int64(1700000000), resp.Backups[0].Timestamp)
}

func TestDecodeBackupResponse_AllTypedMethods(t *testing.T) {
	create := &service.CreateBackupResponse{BackupID: "bk-1", Size: 100, Timestamp: 1, Status: "completed"}
	restore := &service.RestoreBackupResponse{JobID: "job-1", Status: "running", Progress: 42.5}
	del := &service.DeleteBackupResponse{Success: true, Message: "deleted"}
	verify := &service.VerifyBackupResponse{Valid: true, ChecksumMatch: true, Issues: []string{}}

	cases := []struct {
		method string
		in     interface{}
		assert func(t *testing.T, got interface{})
	}{
		{"Create", create, func(t *testing.T, got interface{}) {
			r, ok := got.(*service.CreateBackupResponse)
			require.True(t, ok, "got %T", got)
			assert.Equal(t, "bk-1", r.BackupID)
			assert.Equal(t, "completed", r.Status)
		}},
		{"Restore", restore, func(t *testing.T, got interface{}) {
			r, ok := got.(*service.RestoreBackupResponse)
			require.True(t, ok, "got %T", got)
			assert.Equal(t, "job-1", r.JobID)
			assert.Equal(t, 42.5, r.Progress)
		}},
		{"Delete", del, func(t *testing.T, got interface{}) {
			r, ok := got.(*service.DeleteBackupResponse)
			require.True(t, ok, "got %T", got)
			assert.True(t, r.Success)
		}},
		{"Verify", verify, func(t *testing.T, got interface{}) {
			r, ok := got.(*service.VerifyBackupResponse)
			require.True(t, ok, "got %T", got)
			assert.True(t, r.Valid)
			assert.True(t, r.ChecksumMatch)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			payload, err := EncodePayload(tc.in)
			require.NoError(t, err)
			got, err := decodeBackupResponse(tc.method, payload)
			require.NoError(t, err)
			tc.assert(t, got)
		})
	}
}

func TestDecodeBackupResponse_EmptyPayload(t *testing.T) {
	got, err := decodeBackupResponse("List", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeBackupResponse_UnknownMethod(t *testing.T) {
	got, err := decodeBackupResponse("Bogus", []byte(`{"foo":"bar"}`))
	require.NoError(t, err)
	m, ok := got.(map[string]interface{})
	require.True(t, ok, "expected fallback map, got %T", got)
	assert.Equal(t, "bar", m["foo"])
}

func TestDecodeBackupResponse_InvalidJSON(t *testing.T) {
	_, err := decodeBackupResponse("List", []byte(`{not json`))
	require.Error(t, err)
}
