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
	"encoding/json"
	"reflect"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// TestCeremonyInitialize_SendsOfficersAndSANsAsJSONStrings pins the wire
// shape of qrdb_ceremony_initialize: go-qrdb's native tool declares officers
// and sans as strings holding JSON, and drops an array value. The server here
// is a fake in-process MCP server that records the arguments it receives, not
// the native handler.
func TestCeremonyInitialize_SendsOfficersAndSANsAsJSONStrings(t *testing.T) {
	var got map[string]interface{}
	tr, _, cleanup := newMultiToolMCPServer(t, []string{"qrdb_ceremony_initialize"}, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		got = args
		return map[string]interface{}{"state": "READY"}
	})
	defer cleanup()

	if _, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{
		Threshold: 2,
		Hostname:  "host",
		SANs:      []string{"a", "b"},
		Officers: []qrdbtransport.CeremonyOfficerRequest{
			{Username: "alice", CSRPEM: "CSR"},
		},
	}); err != nil {
		t.Fatalf("CeremonyInitialize: %v", err)
	}

	officersRaw, ok := got["officers"].(string)
	if !ok {
		t.Fatalf("officers arrived as %T, want a JSON-encoded string", got["officers"])
	}
	var officers []map[string]string
	if err := json.Unmarshal([]byte(officersRaw), &officers); err != nil {
		t.Fatalf("officers %q is not JSON: %v", officersRaw, err)
	}
	wantOfficers := []map[string]string{{"username": "alice", "csr_pem": "CSR"}}
	if !reflect.DeepEqual(officers, wantOfficers) {
		t.Fatalf("officers = %v, want %v", officers, wantOfficers)
	}

	sansRaw, ok := got["sans"].(string)
	if !ok {
		t.Fatalf("sans arrived as %T, want a JSON-encoded string", got["sans"])
	}
	var sans []string
	if err := json.Unmarshal([]byte(sansRaw), &sans); err != nil {
		t.Fatalf("sans %q is not JSON: %v", sansRaw, err)
	}
	if !reflect.DeepEqual(sans, []string{"a", "b"}) {
		t.Fatalf("sans = %v, want [a b]", sans)
	}
}
