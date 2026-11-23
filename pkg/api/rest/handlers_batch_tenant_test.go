//go:build rest

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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestHandleBatch_NoTenantIsForbidden(t *testing.T) {
	// The service returns BuildKey's tenant refusal unchanged.
	_, tenantErr := tenantctx.FromContext(context.Background(), "keyspace.BuildKey")
	require.Error(t, tenantErr)
	invalidErr := &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "BatchPut",
		Err:  &errors.MessageError{Message: "validate entity type __reserved"},
	}

	tests := []struct {
		name   string
		op     string
		svcErr error
		want   int
	}{
		{name: "BatchPut no tenant", op: "put", svcErr: tenantErr, want: http.StatusForbidden},
		{name: "BatchDelete no tenant", op: "delete", svcErr: tenantErr, want: http.StatusForbidden},
		{name: "BatchPut invalid input", op: "put", svcErr: invalidErr, want: http.StatusBadRequest},
		{name: "BatchDelete invalid input", op: "delete", svcErr: invalidErr, want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mock, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()

			var handler http.HandlerFunc
			var body string
			switch tt.op {
			case "put":
				mock.BatchPutFunc = func(context.Context, *service.BatchPutRequest) (*service.BatchPutResponse, error) {
					return nil, tt.svcErr
				}
				handler = server.HandleBatchPut
				body = `{"operations":[{"entity_type":"user","id":1,"data":{"name":"a"}}]}`
			case "delete":
				mock.BatchDeleteFunc = func(context.Context, *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
					return nil, tt.svcErr
				}
				handler = server.HandleBatchDelete
				body = `{"operations":[{"entity_type":"user","id":1}]}`
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/"+tt.op, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler(rec, req)

			require.Equal(t, tt.want, rec.Code, "body: %s", rec.Body.String())
		})
	}
}
