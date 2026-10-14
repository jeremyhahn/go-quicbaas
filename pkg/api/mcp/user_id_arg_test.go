//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestGetUserID_AcceptsTheFullUint64Range(t *testing.T) {
	for _, want := range []uint64{1, 1 << 53, (1 << 53) + 1, math.MaxUint64} {
		got, err := getUserID(map[string]interface{}{"user_id": strconv.FormatUint(want, 10)})
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestGetUserID_RefusesANonID(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		value   string
		wantErr error
	}{
		{name: "missing", args: map[string]interface{}{}, value: "", wantErr: strconv.ErrSyntax},
		{name: "non-numeric", args: map[string]interface{}{"user_id": "u1"}, value: "u1", wantErr: strconv.ErrSyntax},
		{name: "overflowing", args: map[string]interface{}{"user_id": "18446744073709551616"},
			value: "18446744073709551616", wantErr: strconv.ErrRange},
		{name: "zero", args: map[string]interface{}{"user_id": "0"}, value: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getUserID(tt.args)
			assert.Zero(t, got)

			var idErr *UserIDArgError
			require.ErrorAs(t, err, &idErr)
			assert.Equal(t, tt.value, idErr.Value)
			assert.Equal(t, qberrors.ErrInvalidInput, idErr.ErrorCode())
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, errors.Unwrap(err))
			}
		})
	}
}

// TestWebAuthnTools_RefuseANonNumericUserIDBeforeTheService proves a tool
// rejects a malformed id at the edge and never reaches the service.
func TestWebAuthnTools_RefuseANonNumericUserIDBeforeTheService(t *testing.T) {
	stub := &stubWebAuthnCredentials{}
	reg := newWebAuthnTestRegistry(stub)

	_, err := reg.executeIAMWebAuthnCredentialsList(context.Background(),
		map[string]interface{}{"tenant_id": "t1", "user_id": "18446744073709551616"})

	var idErr *UserIDArgError
	require.ErrorAs(t, err, &idErr)
	assert.Empty(t, stub.gotTenant, "the service must not be called")
}
