//go:build mcp

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

package server

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// The MCP half of the transport-classification contract.
//
// It is a separate, TAGGED file because mcpapi.ToolRegistry exists only under
// the `mcp` build tag: an untagged file naming it fails to COMPILE in every tag
// set that omits mcp, including the one CI's vet pass uses. The
// transport-independent assertions stay untagged in
// qrdb_user_servicers_transport_test.go, so the contract that matters to gRPC
// is not gated behind a tag gRPC has nothing to do with.

// TestAuthFactorMCPTool_ReservedMetadataIsClassifiedAsInvalidInput crosses the
// MCP transport for real.
//
// It drives the qrdb_user_authfactor_add TOOL — argument parsing, dispatch and
// all — against the same proxy lifecycle_mcp.go installs, rather than calling
// the proxy directly. That is the gap the finding named: a transport-contract
// bug cannot be caught by a test that never crosses the transport.
func TestAuthFactorMCPTool_ReservedMetadataIsClassifiedAsInvalidInput(t *testing.T) {
	registry := newAuthFactorToolRegistry(t, newRecordingProxy(&recordingAuthFactorServicer{}))

	_, err := registry.ExecuteTool(context.Background(), "qrdb_user_authfactor_add", map[string]interface{}{
		"user_id":        float64(1),
		"type":           string(qrdbsdk.AuthFactorFIDO2),
		"credential_b64": base64.StdEncoding.EncodeToString([]byte("credential")),
		"metadata_json":  `{"` + iam.AuthFactorMetadataAttestLevel + `":"hardware"}`,
	})
	if err == nil {
		t.Fatal("the MCP tool accepted reserved attestation metadata")
	}
	if !errors.Is(err, qrdbsdk.ErrInvalidFactor) {
		t.Errorf("the MCP tool failure is not classified as invalid input: %v", err)
	}
	var metadataErr *iam.AuthFactorAttestationMetadataError
	if !errors.As(err, &metadataErr) {
		t.Fatalf("the MCP tool lost the specific reason: %T: %v", err, err)
	}
}

// TestAuthFactorMCPTool_AcceptsAFactorWithoutReservedMetadata is the control.
// The classification change must not turn a legitimate enrollment into a
// refusal, and the tool must still succeed through the same path.
func TestAuthFactorMCPTool_AcceptsAFactorWithoutReservedMetadata(t *testing.T) {
	rec := &recordingAuthFactorServicer{}
	registry := newAuthFactorToolRegistry(t, newRecordingProxy(rec))

	_, err := registry.ExecuteTool(context.Background(), "qrdb_user_authfactor_add", map[string]interface{}{
		"user_id":        float64(1),
		"type":           string(qrdbsdk.AuthFactorFIDO2),
		"credential_b64": base64.StdEncoding.EncodeToString([]byte("credential")),
		"metadata_json":  `{"binding_source":"operator"}`,
	})
	if err != nil {
		t.Fatalf("a factor carrying no reserved metadata was refused: %v", err)
	}
	if len(rec.added) != 1 {
		t.Fatalf("the factor never reached the servicer: %d adds", len(rec.added))
	}
	added := rec.added[0]
	// The stamp still applies through the transport: the record says the
	// enrollment proved nothing, and the caller's own metadata survives.
	// Nothing is stamped on the way through: the node service writes
	// attest_level=none itself (go-qrdb v0.16.2-alpha), and a proxy that wrote
	// it would be refused as a caller.
	if got, present := added.Metadata[iam.AuthFactorMetadataAttestLevel]; present {
		t.Errorf("the proxy stamped %s = %q; the node service owns that key", iam.AuthFactorMetadataAttestLevel, got)
	}
	if got := added.Metadata["binding_source"]; got != "operator" {
		t.Errorf("caller metadata was lost: binding_source = %q", got)
	}
}

// newAuthFactorToolRegistry builds the MCP tool registry with the auth-factor
// proxy installed exactly as lifecycle_mcp.go installs it.
func newAuthFactorToolRegistry(t *testing.T, proxy qrdbsdk.UserAuthFactorServicer) *mcpapi.ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	registry := mcpapi.NewToolRegistry(nil, log)
	registry.SetUserAuthFactorService(proxy)
	return registry
}
