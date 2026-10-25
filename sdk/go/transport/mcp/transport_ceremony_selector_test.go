package mcp

import (
	"context"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestCeremonyInitialize_SendsAdminSelector proves every admin credential
// selector field reaches the qrdb_ceremony_initialize tool arguments
// unchanged.
func TestCeremonyInitialize_SendsAdminSelector(t *testing.T) {
	var got map[string]interface{}
	tr, _ := newMultiToolMCPServerTLS(t, []string{"qrdb_ceremony_initialize"},
		func(_ *testing.T, _ string, args map[string]interface{}) map[string]interface{} {
			got = args
			return map[string]interface{}{"state": "initialized"}
		})

	_, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{
		SOPin:            "so",
		AdminCSRPEM:      "csr",
		AdminCertPEM:     "cert",
		AdminCertPoolPEM: "pool",
		AdminGenerate:    true,
		Secret:           []byte("secret"),
	})
	if err != nil {
		t.Fatalf("CeremonyInitialize: %v", err)
	}

	want := map[string]interface{}{
		"admin_csr_pem":       "csr",
		"admin_cert_pem":      "cert",
		"admin_cert_pool_pem": "pool",
		"admin_generate":      true,
		"secret":              "secret",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("tool argument %s = %v, want %v", key, got[key], value)
		}
	}
}

// TestCeremonyInitialize_NoAdminSelectorIsInvalidInput proves a request
// without an admin credential selector sends no selector argument, and that
// the server's invalid-input refusal comes back as a typed invalid-input
// error.
func TestCeremonyInitialize_NoAdminSelectorIsInvalidInput(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "qrdb_ceremony_initialize",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			for _, key := range []string{"admin_csr_pem", "admin_cert_pem", "admin_cert_pool_pem", "admin_generate", "secret"} {
				if _, present := args[key]; present {
					t.Errorf("tool argument %s sent without a selector: %v", key, args[key])
				}
			}
		},
		toolError:     true,
		toolErrorCode: MCPInvalidInput,
		rawText:       "exactly one admin credential selector is required",
	})
	defer cleanup()

	_, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{Threshold: 1})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected invalid-input error, got %v", err)
	}
}
