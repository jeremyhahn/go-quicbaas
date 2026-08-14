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
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestPIVService_Detection pins the service-name matcher.
func TestPIVService_Detection(t *testing.T) {
	assert.True(t, isPIVService("quicbaas.PIV"))
	assert.True(t, isPIVService("PIV"))
	assert.False(t, isPIVService("quicbaas.KMS"))
	assert.False(t, isPIVService("quicbaas.FIDO2"))
}

// TestFIDO2Service_Detection pins the service-name matcher.
func TestFIDO2Service_Detection(t *testing.T) {
	assert.True(t, isFIDO2Service("quicbaas.FIDO2"))
	assert.True(t, isFIDO2Service("FIDO2"))
	assert.False(t, isFIDO2Service("quicbaas.PIV"))
}

// TestPIVMethodToSubOp covers every wired method and the unknown-method
// rejection.
func TestPIVMethodToSubOp(t *testing.T) {
	want := map[string]byte{
		"ListPIVSlots":         SubOpPIVListSlots,
		"GetPIVCertificate":    SubOpPIVGetCert,
		"StorePIVCertificate":  SubOpPIVStoreCert,
		"DeletePIVCertificate": SubOpPIVDeleteCert,
		"GeneratePIVKey":       SubOpPIVGenerateKey,
		"ImportPIVCertificate": SubOpPIVImportCert,
		"ExportPIVCertificate": SubOpPIVExportCert,
		"GeneratePIVCSR":       SubOpPIVGenerateCSR,
	}
	for method, subOp := range want {
		got, ok := PIVMethodToSubOp(method)
		require.True(t, ok, method)
		assert.Equal(t, subOp, got, method)
	}
	_, ok := PIVMethodToSubOp("Nope")
	assert.False(t, ok)
}

// TestFIDO2MethodToSubOp covers every wired method and the unknown-method
// rejection.
func TestFIDO2MethodToSubOp(t *testing.T) {
	want := map[string]byte{
		"BeginRegistration":    SubOpFIDO2BeginRegistration,
		"FinishRegistration":   SubOpFIDO2FinishRegistration,
		"BeginAuthentication":  SubOpFIDO2BeginAuthentication,
		"FinishAuthentication": SubOpFIDO2FinishAuthentication,
	}
	for method, subOp := range want {
		got, ok := FIDO2MethodToSubOp(method)
		require.True(t, ok, method)
		assert.Equal(t, subOp, got, method)
	}
	_, ok := FIDO2MethodToSubOp("Nope")
	assert.False(t, ok)
}

// TestDecodePIVResponse_HappyPaths decodes each typed PIV response.
func TestDecodePIVResponse_HappyPaths(t *testing.T) {
	slots, err := decodePIVResponse("ListPIVSlots",
		[]byte(`{"slots":[{"slot":"9a","has_cert":true}]}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.ListPIVSlotsResponse{}, slots)
	assert.Len(t, slots.(*xkmstransport.ListPIVSlotsResponse).Slots, 1)

	cert, err := decodePIVResponse("GetPIVCertificate",
		[]byte(`{"slot":"9a","certificate":"Y2VydA==","format":"pem"}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.GetPIVCertificateResponse{}, cert)
	assert.Equal(t, []byte("cert"), cert.(*xkmstransport.GetPIVCertificateResponse).Certificate)

	gen, err := decodePIVResponse("GeneratePIVKey",
		[]byte(`{"slot":"9a","certificate":"Y2VydA==","public_key":"cHVi"}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.GeneratePIVKeyResponse{}, gen)

	csr, err := decodePIVResponse("GeneratePIVCSR", []byte(`{"slot":"9a","csr":"Y3Ny"}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.GeneratePIVCSRResponse{}, csr)

	// Void verb with a body: discarded after validation.
	void, err := decodePIVResponse("StorePIVCertificate", []byte(`{}`))
	require.NoError(t, err)
	assert.Nil(t, void)

	// Empty payload resolves to nil without a decoder.
	empty, err := decodePIVResponse("ListPIVSlots", nil)
	require.NoError(t, err)
	assert.Nil(t, empty)
}

// TestDecodePIVResponse_Errors rejects unknown methods and malformed JSON.
func TestDecodePIVResponse_Errors(t *testing.T) {
	_, err := decodePIVResponse("Nope", []byte(`{}`))
	require.Error(t, err)

	for _, method := range []string{
		"ListPIVSlots", "GetPIVCertificate", "GeneratePIVKey",
		"GeneratePIVCSR", "StorePIVCertificate",
	} {
		_, err := decodePIVResponse(method, []byte(`{not json`))
		require.Error(t, err, method)
	}
}

// TestDecodeFIDO2Response_HappyPaths decodes each typed FIDO2 response.
func TestDecodeFIDO2Response_HappyPaths(t *testing.T) {
	begin, err := decodeFIDO2Response("BeginRegistration",
		[]byte(`{"session_id":"s1","options_json":"{}"}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.BeginRegistrationResponse{}, begin)
	assert.Equal(t, "s1", begin.(*xkmstransport.BeginRegistrationResponse).SessionID)

	finish, err := decodeFIDO2Response("FinishRegistration",
		[]byte(`{"success":true,"credential_id":"c1","token":"tok"}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.FinishRegistrationResponse{}, finish)

	beginAuth, err := decodeFIDO2Response("BeginAuthentication",
		[]byte(`{"session_id":"s2","options_json":"{}"}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.BeginAuthenticationResponse{}, beginAuth)

	finishAuth, err := decodeFIDO2Response("FinishAuthentication",
		[]byte(`{"success":true,"token":"tok","username":"alice"}`))
	require.NoError(t, err)
	require.IsType(t, &xkmstransport.FinishAuthenticationResponse{}, finishAuth)

	empty, err := decodeFIDO2Response("BeginRegistration", nil)
	require.NoError(t, err)
	assert.Nil(t, empty)
}

// TestDecodeFIDO2Response_Errors rejects unknown methods and malformed JSON.
func TestDecodeFIDO2Response_Errors(t *testing.T) {
	_, err := decodeFIDO2Response("Nope", []byte(`{}`))
	require.Error(t, err)

	for _, method := range []string{
		"BeginRegistration", "FinishRegistration",
		"BeginAuthentication", "FinishAuthentication",
	} {
		_, err := decodeFIDO2Response(method, []byte(`{not json`))
		require.Error(t, err, method)
	}
}

// TestDoRequest_PIVDispatch proves the full wire round-trip: sub-op prepend,
// xkms request encoding, and typed response decode.
func TestDoRequest_PIVDispatch(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)

	listResp, _ := EncodePayload(&xkmstransport.ListPIVSlotsResponse{
		Slots: []xkmstransport.PIVSlotStatus{{Slot: "9a", HasCert: true}},
	})
	server.RegisterHandler(OpOrchestrator, func(req *Request) *Response {
		if len(req.Payload) < 1 {
			return &Response{Status: StatusInvalidRequest, Payload: []byte("empty payload")}
		}
		switch req.Payload[0] {
		case SubOpPIVListSlots:
			var body xkmstransport.ListPIVSlotsRequest
			if err := json.Unmarshal(req.Payload[1:], &body); err != nil || body.Backend != "pkcs11" {
				return &Response{Status: StatusInvalidRequest, Payload: []byte("bad body")}
			}
			return &Response{Status: StatusOK, Payload: listResp}
		case SubOpPIVDeleteCert:
			return &Response{Status: StatusOK}
		default:
			return &Response{Status: StatusInvalidRequest, Payload: []byte("unexpected sub-op")}
		}
	})

	trans := newDispatchTestTransport(t, server)

	resp, err := trans.Request(context.Background(), "quicbaas.PIV", "ListPIVSlots",
		&xkmstransport.ListPIVSlotsRequest{Backend: "pkcs11"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.ListPIVSlotsResponse)
	require.True(t, ok, "expected *ListPIVSlotsResponse, got %T", resp)
	require.Len(t, typed.Slots, 1)
	assert.Equal(t, "9a", typed.Slots[0].Slot)

	// Void verb: empty OK payload resolves to nil.
	void, err := trans.Request(context.Background(), "quicbaas.PIV", "DeletePIVCertificate",
		&xkmstransport.DeletePIVCertificateRequest{Backend: "pkcs11", Slot: "9a"})
	require.NoError(t, err)
	assert.Nil(t, void)

	// Unknown method fails fast client-side.
	_, err = trans.Request(context.Background(), "quicbaas.PIV", "Nope", struct{}{})
	require.Error(t, err)
}

// TestDoRequest_FIDO2Dispatch proves the full wire round-trip including the
// context tenant injection.
func TestDoRequest_FIDO2Dispatch(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)

	beginResp, _ := EncodePayload(&xkmstransport.BeginRegistrationResponse{
		SessionID:   "sess-1",
		OptionsJSON: `{"publicKey":{}}`,
	})
	server.RegisterHandler(OpOrchestrator, func(req *Request) *Response {
		if len(req.Payload) < 1 || req.Payload[0] != SubOpFIDO2BeginRegistration {
			return &Response{Status: StatusInvalidRequest, Payload: []byte("unexpected sub-op")}
		}
		var body struct {
			TenantID string `json:"tenant_id"`
			Username string `json:"username"`
		}
		if err := json.Unmarshal(req.Payload[1:], &body); err != nil {
			return &Response{Status: StatusInvalidRequest, Payload: []byte("bad body")}
		}
		if body.TenantID != "tenant-a" || body.Username != "alice@example.com" {
			return &Response{Status: StatusInvalidRequest, Payload: []byte("tenant or username missing")}
		}
		return &Response{Status: StatusOK, Payload: beginResp}
	})

	trans := newDispatchTestTransport(t, server)

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := trans.Request(ctx, "quicbaas.FIDO2", "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Username: "alice@example.com"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.BeginRegistrationResponse)
	require.True(t, ok, "expected *BeginRegistrationResponse, got %T", resp)
	assert.Equal(t, "sess-1", typed.SessionID)

	// Unknown method fails fast client-side.
	_, err = trans.Request(ctx, "quicbaas.FIDO2", "Nope", struct{}{})
	require.Error(t, err)
}
