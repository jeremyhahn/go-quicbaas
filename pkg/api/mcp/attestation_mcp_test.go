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

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errFakeAttestation drives error propagation through the MCP handlers.
var errFakeAttestation = errors.New("attestation: boom")

// fakeAttestationService records its input and replays a fixed response.
type fakeAttestationService struct {
	// The embedded interface supplies the Verifier verbs this stub does not
	// exercise; they are tested against the real service elsewhere.
	service.AttestationService

	failNext bool

	lastVerify      *service.VerifyAttestationRequest
	lastVerifyChain *service.VerifyAttestationChainRequest
	lastAttest      *service.AttestContainerRequest
}

func (f *fakeAttestationService) Verify(_ context.Context, req *service.VerifyAttestationRequest) (*service.VerifyAttestationResponse, error) {
	f.lastVerify = req
	if f.failNext {
		return nil, errFakeAttestation
	}
	return &service.VerifyAttestationResponse{
		Valid:          true,
		Format:         req.Format,
		Backend:        req.Backend,
		ChainValid:     true,
		SignatureValid: true,
	}, nil
}

func (f *fakeAttestationService) VerifyChain(_ context.Context, req *service.VerifyAttestationChainRequest) (*service.VerifyAttestationChainResponse, error) {
	f.lastVerifyChain = req
	if f.failNext {
		return nil, errFakeAttestation
	}
	return &service.VerifyAttestationChainResponse{Valid: true}, nil
}

func (f *fakeAttestationService) AttestContainer(_ context.Context, req *service.AttestContainerRequest) (*service.AttestContainerResponse, error) {
	f.lastAttest = req
	if f.failNext {
		return nil, errFakeAttestation
	}
	return &service.AttestContainerResponse{
		Attested:        true,
		DeviceID:        req.DeviceID,
		ContainerID:     req.ContainerID,
		Image:           req.Image,
		ImageDigest:     req.ImageDigest,
		ConfigDigest:    "sha256:cfg",
		TrustLevel:      req.TrustLevel,
		Measured:        true,
		MeasurementPCR:  16,
		MeasurementBank: "sha256",
		Quoted:          []byte("tpms-attest"),
		Signature:       []byte("sig"),
		Nonce:           []byte("nonce"),
		PCRs:            map[uint32][]byte{7: {0x01}},
		AttestedAt:      time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

// newAttestationRegistry builds a ToolRegistry whose Attestation() returns
// the supplied service.
func newAttestationRegistry(t *testing.T, svc service.AttestationService) *ToolRegistry {
	t.Helper()
	return &ToolRegistry{service: &mockService{attestationSvc: svc}}
}

// testCertPEM is a syntactically valid PEM CERTIFICATE block. The MCP handler
// only splits the bundle; parsing happens in the service layer, which is
// stubbed here.
var testCertPEM = string(pem.EncodeToMemory(&pem.Block{
	Type:  "CERTIFICATE",
	Bytes: []byte("not-a-real-der-body"),
}))

func TestExecuteAttestationTool_Verify(t *testing.T) {
	fake := &fakeAttestationService{}
	reg := newAttestationRegistry(t, fake)

	result, err := reg.executeAttestationTool(context.Background(), "quicbaas_attestation_verify",
		map[string]interface{}{
			"format":            "tpm2",
			"backend":           "tpm2",
			"chain_pem":         testCertPEM,
			"attested_key_der":  base64.StdEncoding.EncodeToString([]byte("key")),
			"attestation_data":  base64.StdEncoding.EncodeToString([]byte("data")),
			"signature":         base64.StdEncoding.EncodeToString([]byte("sig")),
			"check_freshness":   true,
			"allow_self_signed": true,
		})
	require.NoError(t, err)

	out, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, out["valid"])
	assert.Equal(t, true, out["chain_valid"])
	assert.Equal(t, true, out["signature_valid"])

	require.NotNil(t, fake.lastVerify)
	assert.Equal(t, []byte("data"), fake.lastVerify.AttestationData)
	assert.Equal(t, []byte("sig"), fake.lastVerify.Signature)
	require.Len(t, fake.lastVerify.ChainPEM, 1)
	require.NotNil(t, fake.lastVerify.Options)
	assert.True(t, fake.lastVerify.Options.CheckFreshness)
	assert.True(t, fake.lastVerify.Options.AllowSelfSigned)
}

func TestExecuteAttestationTool_Verify_InvalidInput(t *testing.T) {
	valid := map[string]interface{}{
		"format":           "tpm2",
		"chain_pem":        testCertPEM,
		"attested_key_der": base64.StdEncoding.EncodeToString([]byte("key")),
		"signature":        base64.StdEncoding.EncodeToString([]byte("sig")),
	}

	mutations := map[string]func(map[string]interface{}){
		"MissingFormat":       func(m map[string]interface{}) { delete(m, "format") },
		"MissingChain":        func(m map[string]interface{}) { delete(m, "chain_pem") },
		"NonCertificatePEM":   func(m map[string]interface{}) { m["chain_pem"] = "not pem at all" },
		"MissingAttestedKey":  func(m map[string]interface{}) { delete(m, "attested_key_der") },
		"MissingSignature":    func(m map[string]interface{}) { delete(m, "signature") },
		"MalformedBase64Key":  func(m map[string]interface{}) { m["attested_key_der"] = "!!!not base64!!!" },
		"MalformedBase64Sig":  func(m map[string]interface{}) { m["signature"] = "!!!not base64!!!" },
		"MalformedBase64Data": func(m map[string]interface{}) { m["attestation_data"] = "!!!not base64!!!" },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			args := make(map[string]interface{}, len(valid))
			for k, v := range valid {
				args[k] = v
			}
			mutate(args)

			reg := newAttestationRegistry(t, &fakeAttestationService{})
			_, err := reg.executeAttestationTool(context.Background(), "quicbaas_attestation_verify", args)
			require.Error(t, err)
		})
	}

	t.Run("ServiceError", func(t *testing.T) {
		reg := newAttestationRegistry(t, &fakeAttestationService{failNext: true})
		_, err := reg.executeAttestationTool(context.Background(), "quicbaas_attestation_verify", valid)
		require.Error(t, err)
	})
}

func TestExecuteAttestationTool_VerifyChain(t *testing.T) {
	fake := &fakeAttestationService{}
	reg := newAttestationRegistry(t, fake)

	result, err := reg.executeAttestationTool(context.Background(), "quicbaas_attestation_verify_chain",
		map[string]interface{}{
			"format":            "tpm2",
			"chain_pem":         testCertPEM + testCertPEM,
			"allow_self_signed": true,
		})
	require.NoError(t, err)

	out, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, out["valid"])

	require.NotNil(t, fake.lastVerifyChain)
	assert.Len(t, fake.lastVerifyChain.ChainPEM, 2, "both PEM blocks must reach the service")
	require.NotNil(t, fake.lastVerifyChain.Options)
	assert.True(t, fake.lastVerifyChain.Options.AllowSelfSigned)
}

func TestExecuteAttestationTool_VerifyChain_InvalidInput(t *testing.T) {
	reg := newAttestationRegistry(t, &fakeAttestationService{})

	_, err := reg.executeAttestationTool(context.Background(), "quicbaas_attestation_verify_chain",
		map[string]interface{}{"format": "tpm2"})
	require.Error(t, err, "an absent chain must be rejected")

	_, err = reg.executeAttestationTool(context.Background(), "quicbaas_attestation_verify_chain",
		map[string]interface{}{"format": "tpm2", "chain_pem": "no pem blocks here"})
	require.Error(t, err, "a bundle with no CERTIFICATE block must be rejected")
}

func TestExecuteAttestationTool_AttestContainer(t *testing.T) {
	fake := &fakeAttestationService{}
	reg := newAttestationRegistry(t, fake)

	result, err := reg.executeAttestationTool(context.Background(), "quicbaas_attestation_attest_container",
		map[string]interface{}{
			"device_id":        "device-1",
			"container_id":     "container-1",
			"image":            "alpine@sha256:abc",
			"image_digest":     "sha256:abc",
			"trust_label":      "prod",
			"trust_level":      "measured",
			"measurement_pcr":  float64(16),
			"measurement_bank": "sha256",
		})
	require.NoError(t, err)

	out, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, out["attested"])
	assert.Equal(t, true, out["measured"])
	assert.Equal(t, "sha256:cfg", out["config_digest"])
	assert.Equal(t, "2026-08-09T12:00:00Z", out["attested_at"])

	// Binary evidence must be base64 so it survives the JSON tool boundary.
	quoted, ok := out["quoted"].(string)
	require.True(t, ok)
	decoded, decodeErr := base64.StdEncoding.DecodeString(quoted)
	require.NoError(t, decodeErr)
	assert.Equal(t, []byte("tpms-attest"), decoded)

	pcrs, ok := out["pcrs"].(map[string]string)
	require.True(t, ok, "PCR indices must be rendered as decimal string keys")
	assert.Contains(t, pcrs, "7")

	require.NotNil(t, fake.lastAttest)
	assert.Equal(t, "device-1", fake.lastAttest.DeviceID)
	assert.Equal(t, 16, fake.lastAttest.MeasurementPCR)
}

func TestExecuteAttestationTool_AttestContainer_InvalidInput(t *testing.T) {
	t.Run("MissingImage", func(t *testing.T) {
		reg := newAttestationRegistry(t, &fakeAttestationService{})
		_, err := reg.executeAttestationTool(context.Background(),
			"quicbaas_attestation_attest_container", map[string]interface{}{})
		require.Error(t, err)
	})

	t.Run("NotConfiguredPropagates", func(t *testing.T) {
		reg := newAttestationRegistry(t, service.NewAttestationService(nil, nil))
		_, err := reg.executeAttestationTool(context.Background(),
			"quicbaas_attestation_attest_container",
			map[string]interface{}{"image": "alpine@sha256:abc"})
		require.Error(t, err)
	})
}

func TestExecuteAttestationTool_Dispatch(t *testing.T) {
	t.Run("UnknownTool", func(t *testing.T) {
		reg := newAttestationRegistry(t, &fakeAttestationService{})
		_, err := reg.executeAttestationTool(context.Background(),
			"quicbaas_attestation_quarantine", map[string]interface{}{})
		require.Error(t, err, "Tier 3 verbs are not part of the QuicBaaS surface")
	})

	t.Run("ServiceNotConfigured", func(t *testing.T) {
		reg := newAttestationRegistry(t, nil)
		_, err := reg.executeAttestationTool(context.Background(),
			"quicbaas_attestation_verify", map[string]interface{}{})
		require.Error(t, err)
	})
}

func TestIsAttestationTool(t *testing.T) {
	for _, name := range []string{
		"quicbaas_attestation_verify",
		"quicbaas_attestation_verify_chain",
		"quicbaas_attestation_attest_container",
	} {
		assert.True(t, isAttestationTool(name), name)
	}
	for _, name := range []string{
		"quicbaas_trust_publish_dane",
		"quicbaas_attestation_",
		"attestation_verify",
		"",
	} {
		assert.False(t, isAttestationTool(name), name)
	}
}

// TestAttestationToolsAreRegistered proves the tool definitions reach the
// registry: a handler with no definition is invisible to tools/list and
// therefore dead.
func TestAttestationToolsAreRegistered(t *testing.T) {
	reg := newAttestationRegistry(t, &fakeAttestationService{})
	reg.registerAttestationTools()

	registered := make(map[string]bool)
	for _, tool := range reg.tools {
		registered[tool.Name] = true
	}
	for name := range attestationExecutors {
		assert.True(t, registered[name], "tool %s has an executor but no definition", name)
	}
	assert.Len(t, getAllAttestationTools(), len(attestationExecutors),
		"every registered tool must have exactly one executor")
}

// TestAttestationToolsAuthzMapping is the default-deny guard: authz_qrdb.go is
// exact-match and a tool with no entry is both denied AND filtered out of
// tools/list, so an unmapped registration ships a dead tool. The values must
// match the gRPC canon in pkg/api/grpc/authz_mapping.go and the QUIC sub-op
// table in pkg/api/quic/handler_authz_qrdb.go.
func TestAttestationToolsAuthzMapping(t *testing.T) {
	want := map[string]qrdbToolResource{
		"quicbaas_attestation_verify":           {"attestation", "verify"},
		"quicbaas_attestation_verify_chain":     {"attestation", "verify"},
		"quicbaas_attestation_attest_container": {"attestation", "attest"},
	}

	// Every executor must be mapped: this is what makes the test fail when
	// a verb is added without an authz entry.
	require.Len(t, want, len(attestationExecutors))

	for tool, expected := range want {
		_, isExecutor := attestationExecutors[tool]
		require.True(t, isExecutor, "expectation table names a tool with no executor: %s", tool)

		resource, action, ok := getToolQRDBPermission(tool)
		require.True(t, ok, "tool %s is unmapped and would be default-denied", tool)
		assert.Equal(t, expected.resource, resource, tool)
		assert.Equal(t, expected.action, action, tool)
	}
}

// TestAttestationAuthzIsNotCertificateAuthority pins the deliberate vocabulary
// choice: driving a host TPM must not be reachable with a certificate
// permission, and vice versa.
func TestAttestationAuthzIsNotCertificateAuthority(t *testing.T) {
	resource, action, ok := getToolQRDBPermission("quicbaas_attestation_attest_container")
	require.True(t, ok)
	assert.Equal(t, "attestation", resource)
	assert.NotEqual(t, "certs", resource)
	assert.Equal(t, "attest", action)

	// The read-strength verbs must not share the write-strength action.
	_, verifyAction, ok := getToolQRDBPermission("quicbaas_attestation_verify")
	require.True(t, ok)
	assert.NotEqual(t, action, verifyAction)
}
