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

//go:build integration

package rest

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

func TestREST_ShardHeaderPresent(t *testing.T) {
	base := os.Getenv("QUICBAAS_NODE1_URL")
	if base == "" {
		base = "https://test-node1:8080"
	}
	req, err := http.NewRequest(http.MethodPut, base+"/api/v1/entities/foo/123", strings.NewReader(`{"data":"{}"}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewDataPlaneHTTPClient(t, 30*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if h := resp.Header.Get("X-QuicBaaS-Shard"); h == "" {
		t.Fatalf("expected X-QuicBaaS-Shard header present")
	}
}
