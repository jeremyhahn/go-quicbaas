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

package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// The export generator writes JSON only, so RequestExport refuses any other
// format rather than recording a request it cannot honour (#1724).
func TestGDPRManager_RequestExport_RefusesNonJSONFormats(t *testing.T) {
	for _, format := range []DataExportFormat{DataExportFormatCSV, DataExportFormatXML, "yaml"} {
		t.Run(string(format), func(t *testing.T) {
			manager, _ := createTestGDPRManager(t)

			request, err := manager.RequestExport(context.Background(), "tenant1", 1724, 1724, format, nil, nil)
			require.Error(t, err)
			assert.Nil(t, request)

			var formatErr *UnsupportedDataExportFormatError
			require.True(t, errors.As(err, &formatErr), "want UnsupportedDataExportFormatError, got %T", err)
			assert.Equal(t, format, formatErr.Format)

			var qbErr *dberrors.QBError
			require.True(t, errors.As(err, &qbErr))
			assert.Equal(t, dberrors.ErrValidation, qbErr.Code)

			pending, err := manager.deps.DataExportStorage.ListExportRequests(context.Background(),
				ListExportRequestsOptions{UserID: 1724, TenantID: "tenant1"})
			require.NoError(t, err)
			assert.Empty(t, pending, "a refused request must not be stored")
		})
	}
}

func TestGDPRManager_RequestExport_AcceptsJSONAndDefaultsEmptyToJSON(t *testing.T) {
	for _, format := range []DataExportFormat{DataExportFormatJSON, ""} {
		t.Run("format="+string(format), func(t *testing.T) {
			manager, _ := createTestGDPRManager(t)

			request, err := manager.RequestExport(context.Background(), "tenant1", 1725, 1725, format, nil, nil)
			require.NoError(t, err)
			require.NotNil(t, request)
			assert.Equal(t, DataExportFormatJSON, request.Format)
		})
	}
}

func TestUnsupportedDataExportFormatError_Error(t *testing.T) {
	err := &UnsupportedDataExportFormatError{Format: DataExportFormatCSV}
	assert.Equal(t, `unsupported data export format "csv": only json is supported`, err.Error())
}
