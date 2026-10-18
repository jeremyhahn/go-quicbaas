//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateTenant_RefusesUnstoredFieldsAsBadRequest proves an update that
// sets description, metadata or status answers 400 naming the field and
// stores nothing, not even the display_name beside it (#1518).
func TestUpdateTenant_RefusesUnstoredFieldsAsBadRequest(t *testing.T) {
	cases := map[string]string{
		"description": `{"display_name":"New","description":"d"}`,
		"metadata":    `{"display_name":"New","metadata":{"k":"v"}}`,
		"status":      `{"display_name":"New","status":"suspended"}`,
	}
	for field, body := range cases {
		t.Run(field, func(t *testing.T) {
			handler, tenants := newRealTenantManagerHandler(t)
			ctx := context.Background()
			w := putTenant(handler, "acme", `{"display_name":"Old"}`)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			w = putTenant(handler, "acme", body)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), field)
			stored, err := tenants.GetTenant(ctx, "acme")
			require.NoError(t, err)
			assert.Equal(t, "Old", stored.Settings.DisplayName)
		})
	}
}

// TestUpdateTenant_EmptyMetadataAndNullStatusAreNotRefused proves only a set
// field is refused: an empty metadata object and a null status are absent.
func TestUpdateTenant_EmptyMetadataAndNullStatusAreNotRefused(t *testing.T) {
	handler, tenants := newRealTenantManagerHandler(t)

	w := putTenant(handler, "acme", `{"display_name":"Acme","metadata":{},"status":null}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err := tenants.GetTenant(context.Background(), "acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme", stored.Settings.DisplayName)
}
