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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestActivateFIDO2_NonSystemTenantRefusedIs412: a non-system tenant's
// passkey invitation activation is refused until go-quicbaas #1816, on both
// verbs, as 412 tenant_login_refused. The ceremony never starts and the
// invite stays usable.
func TestActivateFIDO2_NonSystemTenantRefusedIs412(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()
	token, _, err := f.invites.Create(context.Background(), "acme", activationUserID, time.Hour)
	require.NoError(t, err)
	f.tenant, f.token = "acme", token

	for name, call := range map[string]func(*testing.T) *httptest.ResponseRecorder{
		"begin":  f.begin,
		"finish": f.finish,
	} {
		w := call(t)
		require.Equal(t, http.StatusPreconditionFailed, w.Code, "%s: %s", name, w.Body.String())
		var apiErr IAMAPIError
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apiErr))
		assert.Equal(t, ErrTenantLoginRefused, apiErr.Error, name)
		assert.Equal(t, (&sdkerrors.TenantLoginRefusedError{}).Error(), apiErr.ErrorDescription, name)
	}

	assert.Zero(t, f.webauthn.beginCalls+f.webauthn.finishCalls, "the ceremony ran for a refused tenant")
	_, err = f.invites.Lookup(context.Background(), "acme", token)
	assert.NoError(t, err, "a refused activation consumed the invite")
}
