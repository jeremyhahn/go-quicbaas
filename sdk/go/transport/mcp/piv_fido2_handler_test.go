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

package mcp

import (
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPPIVService_Detection pins the service-name matcher.
func TestMCPPIVService_Detection(t *testing.T) {
	assert.True(t, isMCPPIVService("quicbaas.PIV"))
	assert.True(t, isMCPPIVService("PIV"))
	assert.False(t, isMCPPIVService("quicbaas.KMS"))
	assert.False(t, isMCPPIVService("quicbaas.FIDO2"))
}

// TestMCPFIDO2Service_Detection pins the service-name matcher.
func TestMCPFIDO2Service_Detection(t *testing.T) {
	assert.True(t, isMCPFIDO2Service("quicbaas.FIDO2"))
	assert.True(t, isMCPFIDO2Service("FIDO2"))
	assert.False(t, isMCPFIDO2Service("quicbaas.PIV"))
}

// TestMapToToolName_PIV pins every PIV method onto its xkms.piv.* tool.
func TestMapToToolName_PIV(t *testing.T) {
	tr := &Transport{}
	cases := map[string]string{
		"ListPIVSlots":         "xkms.piv.listSlots",
		"GetPIVCertificate":    "xkms.piv.getCert",
		"StorePIVCertificate":  "xkms.piv.storeCert",
		"DeletePIVCertificate": "xkms.piv.deleteCert",
		"GeneratePIVKey":       "xkms.piv.generateKey",
		"ImportPIVCertificate": "xkms.piv.importCert",
		"ExportPIVCertificate": "xkms.piv.exportCert",
		"GeneratePIVCSR":       "xkms.piv.generateCSR",
	}
	for method, want := range cases {
		if got := tr.mapToToolName("quicbaas.PIV", method); got != want {
			t.Errorf("mapToToolName(PIV, %s) = %q, want %q", method, got, want)
		}
	}
}

// TestMapToToolName_FIDO2 pins every FIDO2 method onto its xkms.fido2.* tool.
func TestMapToToolName_FIDO2(t *testing.T) {
	tr := &Transport{}
	cases := map[string]string{
		"BeginRegistration":    "xkms.fido2.beginRegistration",
		"FinishRegistration":   "xkms.fido2.finishRegistration",
		"BeginAuthentication":  "xkms.fido2.beginAuthentication",
		"FinishAuthentication": "xkms.fido2.finishAuthentication",
	}
	for method, want := range cases {
		if got := tr.mapToToolName("quicbaas.FIDO2", method); got != want {
			t.Errorf("mapToToolName(FIDO2, %s) = %q, want %q", method, got, want)
		}
	}
}

// TestPIVMCPHandler_TransformRequest converts the xkms request structs into
// the tool argument shapes, including base64 []byte encoding.
func TestPIVMCPHandler_TransformRequest(t *testing.T) {
	h := newPIVMCPHandler()

	args, err := h.transformRequest("ListPIVSlots", &xkmstransport.ListPIVSlotsRequest{Backend: "pkcs11"})
	require.NoError(t, err)
	assert.Equal(t, "pkcs11", args["backend"])

	args, err = h.transformRequest("StorePIVCertificate", &xkmstransport.StorePIVCertificateRequest{
		Backend:     "file",
		Slot:        "9a",
		Certificate: []byte("cert"),
		Format:      "pem",
	})
	require.NoError(t, err)
	assert.Equal(t, "9a", args["slot"])
	// []byte marshals to base64, the encoding the tool schema expects.
	assert.Equal(t, "Y2VydA==", args["certificate"])

	_, err = h.transformRequest("ListPIVSlots", nil)
	require.Error(t, err)

	_, err = h.transformRequest("ListPIVSlots", "not a struct")
	require.Error(t, err)
}

// TestPIVMCPHandler_TransformResponse decodes tool results into the xkms
// response types.
func TestPIVMCPHandler_TransformResponse(t *testing.T) {
	h := newPIVMCPHandler()

	slots, err := h.transformResponse("ListPIVSlots", map[string]interface{}{
		"slots": []interface{}{map[string]interface{}{"slot": "9a", "has_cert": true}},
	})
	require.NoError(t, err)
	typed, ok := slots.(*xkmstransport.ListPIVSlotsResponse)
	require.True(t, ok)
	require.Len(t, typed.Slots, 1)
	assert.Equal(t, "9a", typed.Slots[0].Slot)

	cert, err := h.transformResponse("GetPIVCertificate", map[string]interface{}{
		"slot": "9a", "certificate": "Y2VydA==", "format": "pem",
	})
	require.NoError(t, err)
	certTyped, ok := cert.(*xkmstransport.GetPIVCertificateResponse)
	require.True(t, ok)
	assert.Equal(t, []byte("cert"), certTyped.Certificate)

	gen, err := h.transformResponse("GeneratePIVKey", map[string]interface{}{
		"slot": "9a", "certificate": "Y2VydA==", "public_key": "cHVi",
	})
	require.NoError(t, err)
	genTyped, ok := gen.(*xkmstransport.GeneratePIVKeyResponse)
	require.True(t, ok)
	assert.Equal(t, []byte("pub"), genTyped.PublicKey)

	csr, err := h.transformResponse("GeneratePIVCSR", map[string]interface{}{
		"slot": "9a", "csr": "Y3Ny",
	})
	require.NoError(t, err)
	csrTyped, ok := csr.(*xkmstransport.GeneratePIVCSRResponse)
	require.True(t, ok)
	assert.Equal(t, []byte("csr"), csrTyped.CSR)

	// Void verbs discard the {"success":true} acknowledgment.
	void, err := h.transformResponse("StorePIVCertificate", map[string]interface{}{"success": true})
	require.NoError(t, err)
	assert.Nil(t, void)

	// Unknown method is rejected.
	_, err = h.transformResponse("Nope", map[string]interface{}{})
	require.Error(t, err)

	// Malformed result shape is rejected.
	_, err = h.transformResponse("GetPIVCertificate", map[string]interface{}{
		"certificate": "!!!not-base64!!!",
	})
	require.Error(t, err)
}

// TestFIDO2MCPHandler_TransformRequest converts the xkms request structs into
// the tool argument shapes.
func TestFIDO2MCPHandler_TransformRequest(t *testing.T) {
	h := newFIDO2MCPHandler()

	args, err := h.transformRequest("BeginRegistration", &xkmstransport.BeginRegistrationRequest{
		Email:       "alice@example.com",
		DisplayName: "Alice",
	})
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", args["email"])
	assert.NotContains(t, args, "username",
		"go-xkms v0.17 removed the username wire alias (go-xkms#628); the login key travels as email only")
	assert.Equal(t, "Alice", args["display_name"])

	args, err = h.transformRequest("FinishAuthentication", &xkmstransport.FinishAuthenticationRequest{
		SessionID:     "sess-1",
		Email:         "alice@example.com",
		AssertionJSON: `{"id":"cred"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "sess-1", args["session_id"])
	assert.Equal(t, `{"id":"cred"}`, args["assertion_json"])

	_, err = h.transformRequest("BeginRegistration", nil)
	require.Error(t, err)

	_, err = h.transformRequest("BeginRegistration", 42)
	require.Error(t, err)
}

// TestFIDO2MCPHandler_TransformResponse decodes tool results into the xkms
// response types.
func TestFIDO2MCPHandler_TransformResponse(t *testing.T) {
	h := newFIDO2MCPHandler()

	begin, err := h.transformResponse("BeginRegistration", map[string]interface{}{
		"session_id": "sess-1", "options_json": `{"publicKey":{}}`,
	})
	require.NoError(t, err)
	beginTyped, ok := begin.(*xkmstransport.BeginRegistrationResponse)
	require.True(t, ok)
	assert.Equal(t, "sess-1", beginTyped.SessionID)

	finish, err := h.transformResponse("FinishRegistration", map[string]interface{}{
		"success": true, "credential_id": "cred-1", "token": "tok",
	})
	require.NoError(t, err)
	finishTyped, ok := finish.(*xkmstransport.FinishRegistrationResponse)
	require.True(t, ok)
	assert.True(t, finishTyped.Success)
	assert.Equal(t, "cred-1", finishTyped.CredentialID)

	beginAuth, err := h.transformResponse("BeginAuthentication", map[string]interface{}{
		"session_id": "sess-2", "options_json": `{}`,
	})
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.BeginAuthenticationResponse{}, beginAuth)

	finishAuth, err := h.transformResponse("FinishAuthentication", map[string]interface{}{
		"success": true, "token": "tok", "username": "alice",
	})
	require.NoError(t, err)
	finishAuthTyped, ok := finishAuth.(*xkmstransport.FinishAuthenticationResponse)
	require.True(t, ok)
	assert.Equal(t, "tok", finishAuthTyped.Token)

	// Unknown method is rejected.
	_, err = h.transformResponse("Nope", map[string]interface{}{})
	require.Error(t, err)

	// Malformed result shape is rejected.
	_, err = h.transformResponse("BeginRegistration", map[string]interface{}{
		"session_id": 12345,
	})
	require.Error(t, err)
}

// TestPIVToolName_UnknownMethod verifies the closed-set behavior both
// surfaces rely on in doRequest.
func TestPIVToolName_UnknownMethod(t *testing.T) {
	_, ok := pivToolName("Nope")
	assert.False(t, ok)
	_, ok = fido2ToolName("Nope")
	assert.False(t, ok)
}
