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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestCeremonyFinishLogin_TenantRefusedIs412 checks that a non-system tenant
// login refused at token issuance answers 412 with the tenant_login_refused
// code and the refusal's own guidance, not the generic login rejection.
// go-quicbaas #1816 removes the refusal.
func TestCeremonyFinishLogin_TenantRefusedIs412(t *testing.T) {
	handler, cleanup := setupCeremonyErrHandler(t, &ceremonyErrLoginClient{
		finishErr: service.RefuseTenantLogin("acme"),
	})
	defer cleanup()

	body, err := json.Marshal(ceremonyFinishLoginRequest{Type: "piv", LookupKey: []byte("k"), Signature: []byte("sig")})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	require.Equal(t, http.StatusPreconditionFailed, w.Code, "body: %s", w.Body.String())
	var apiErr IAMAPIError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apiErr))
	assert.Equal(t, ErrTenantLoginRefused, apiErr.Error)
	assert.Equal(t, (&sdkerrors.TenantLoginRefusedError{}).Error(), apiErr.ErrorDescription)
}
