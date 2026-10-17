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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// appWire mirrors the envelope iam.AppClient sends for Register and Update.
type appWire struct {
	TenantID string      `json:"tenant_id"`
	ClientID string      `json:"client_id,omitempty"`
	App      interface{} `json:"app"`
}

// TestIAMHandler_TransformRequest_AppAuthMethod proves the App Update and
// Register tool arguments carry client_secret_basic, the enum's zero value,
// when the caller names it, and an update that omits the method sends no key
// (#1473).
func TestIAMHandler_TransformRequest_AppAuthMethod(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:8888"})
	require.NoError(t, err)
	handler := newIAMHandler(tr)

	basic := iam.TokenEndpointAuthClientSecretBasic
	tests := []struct {
		name    string
		method  string
		app     interface{}
		want    interface{}
		present bool
	}{
		{"update names basic", "Update", &iam.UpdateAppRequest{TokenEndpointAuthMethod: &basic}, "client_secret_basic", true},
		{"update omits method", "Update", &iam.UpdateAppRequest{Name: "renamed"}, nil, false},
		{"register names basic", "Register", &iam.RegisterAppRequest{Name: "app"}, "client_secret_basic", true},
		{"register names post", "Register", &iam.RegisterAppRequest{Name: "app", TokenEndpointAuthMethod: iam.TokenEndpointAuthClientSecretPost}, "client_secret_post", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := &appWire{TenantID: "t1", ClientID: "c1", App: tt.app}
			args, err := handler.transformRequest("quicbaas.IAM.App", tt.method, wire)
			require.NoError(t, err)
			got, ok := args["token_endpoint_auth_method"]
			assert.Equal(t, tt.present, ok, "token_endpoint_auth_method present in %v", args)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIAMHandler_TransformRequest_AppUnencodable proves an App Update the
// transport cannot encode is refused instead of reaching the tool empty.
func TestIAMHandler_TransformRequest_AppUnencodable(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:8888"})
	require.NoError(t, err)

	_, err = newIAMHandler(tr).transformRequest("quicbaas.IAM.App", "Update", func() {})
	require.Error(t, err)
}
