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

package quic

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// tenantDisplayNameServer answers tenant get/update the way the REST server
// does: an update's display_name is stored in settings.display_name and a
// read returns the tenant with its settings.
type tenantDisplayNameServer struct {
	t           *testing.T
	mu          sync.Mutex
	displayName string
	updates     []map[string]interface{}
}

func (s *tenantDisplayNameServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/tenants/acme" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method != http.MethodGet {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Errorf("decode body: %v", err)
		}
		s.updates = append(s.updates, body)
		if dn, ok := body["display_name"].(string); ok {
			s.displayName = dn
		}
	}
	w.Header().Set("Content-Type", "application/json")
	reply := map[string]interface{}{
		"id":       "acme",
		"name":     "Acme",
		"settings": map[string]interface{}{"display_name": s.displayName, "max_users": 42},
	}
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		s.t.Errorf("encode reply: %v", err)
	}
}

func TestTenantDisplayNameRoundTripOverHTTP3(t *testing.T) {
	srv := &tenantDisplayNameServer{t: t}
	addr, shutdown := startTestHTTP3Server(t, srv)
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	client := iam.NewTenantClient(tr)
	_, err = client.Update(context.Background(), "acme", &iam.UpdateTenantRequest{DisplayName: "Acme Corp"})
	require.NoError(t, err)

	srv.mu.Lock()
	require.Len(t, srv.updates, 1)
	body := srv.updates[0]
	srv.mu.Unlock()
	assert.Equal(t, "Acme Corp", body["display_name"])
	assert.NotContains(t, body, "name", "display_name must not be sent as the tenant name")

	got, err := client.Get(context.Background(), "acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", got.DisplayName)
	assert.Equal(t, "Acme", got.Name)
}

func TestTenantDisplayNameOverHTTP3_UnknownTenantFails(t *testing.T) {
	srv := &tenantDisplayNameServer{t: t}
	addr, shutdown := startTestHTTP3Server(t, srv)
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = iam.NewTenantClient(tr).Update(context.Background(), "ghost", &iam.UpdateTenantRequest{DisplayName: "Ghost"})
	require.Error(t, err)
}
