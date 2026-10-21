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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// RequestExport through the real GDPR manager refuses CSV and XML as invalid
// arguments: the generator writes JSON only (#1724).
func TestIAMServer_RequestExport_RefusesNonJSONFormats(t *testing.T) {
	formats := []iampb.DataExportFormat{
		iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV,
		iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML,
	}
	for _, format := range formats {
		t.Run(format.String(), func(t *testing.T) {
			log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
			require.NoError(t, err)
			srv := NewIAMServer(createTestIAMAdapter91(t), log)

			resp, err := srv.RequestExport(callerContext("tenant-1"), &iampb.RequestExportRequest{
				TenantId: "tenant-1", UserId: "10001", RequestedBy: "30021", Format: format,
			})
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, codes.InvalidArgument, status.Code(err), "error: %v", err)
		})
	}
}

func TestIAMServer_RequestExport_AcceptsJSON(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	srv := NewIAMServer(createTestIAMAdapter91(t), log)

	resp, err := srv.RequestExport(callerContext("tenant-1"), &iampb.RequestExportRequest{
		TenantId: "tenant-1", UserId: "10001", RequestedBy: "30021",
		Format: iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Id)
}
