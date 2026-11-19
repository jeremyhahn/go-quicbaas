//go:build mcp

package mcp

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newMinimalMockService creates a mockService suitable for xkms tool tests.
// The mockService type is defined in comprehensive_test.go in the same package.
func newMinimalMockService() service.QuicBaaS {
	return &mockService{}
}

// =============================================================================
// Tool Registration Tests
// =============================================================================

// TestXKMSToolsRegistered verifies all xkms tools are registered in the ToolRegistry.
func TestXKMSToolsRegistered(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	expectedTools := []string{
		"xkms.listBackends",
		"xkms.generateKey",
		"xkms.getKey",
		"xkms.deleteKey",
		"xkms.listKeys",
		"xkms.rotateKey",
		"xkms.sign",
		"xkms.verify",
		"xkms.encrypt",
		"xkms.decrypt",
		"xkms.saveCert",
		"xkms.getCert",
		"xkms.deleteCert",
		"xkms.listCerts",
		"xkms.certExists",
		"xkms.saveCertChain",
		"xkms.getCertChain",
		"xkms.seal",
		"xkms.unseal",
		"xkms.canSeal",
		"xkms.piv.listSlots",
		"xkms.piv.getCert",
		"xkms.piv.storeCert",
		"xkms.piv.deleteCert",
		"xkms.piv.generateKey",
		"xkms.piv.generateCSR",
		"xkms.ca.getBundle",
		"xkms.ca.getCert",
		"xkms.ca.signCSR",
		"xkms.ca.issueCert",
		"xkms.ca.revokeCert",
		"xkms.ca.generateCRL",
		"xkms.ca.isRevoked",
		"xkms.pin.setSO",
		"xkms.pin.setUser",
		"xkms.pin.changeSO",
		"xkms.pin.changeUser",
		"xkms.pin.verifySO",
		"xkms.pin.verifyUser",
		"xkms.pin.lockoutStatus",
		"xkms.pin.resetLockout",
	}

	tools := registry.ListTools()
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected xkms tool %q to be registered", name)
	}
}

// =============================================================================
// isXKMSTool Tests
// =============================================================================

// TestIsXKMSTool verifies the prefix-based tool identification.
func TestIsXKMSTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{name: "valid xkms tool", toolName: "xkms.generateKey", expected: true},
		{name: "valid xkms seal", toolName: "xkms.seal", expected: true},
		{name: "valid xkms list", toolName: "xkms.listBackends", expected: true},
		{name: "quicbaas tool", toolName: "qrdb_put", expected: false},
		{name: "kms tool", toolName: "quicbaas_kms_generate_key", expected: false},
		{name: "empty string", toolName: "", expected: false},
		{name: "partial prefix", toolName: "xkm.generateKey", expected: false},
		{name: "similar but wrong", toolName: "xkms_generateKey", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isXKMSTool(tt.toolName))
		})
	}
}

// =============================================================================
// Handler Dispatch Tests
// =============================================================================

// TestExecuteXKMSToolUnknown verifies that unknown xkms tool names return an error.
func TestExecuteXKMSToolUnknown(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	ctx := context.Background()
	_, err := registry.executeXKMSTool(ctx, "xkms.nonexistent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

// TestExecuteXKMSToolDispatchesCorrectly verifies the handler map is initialized
// and contains all expected tool names.
func TestExecuteXKMSToolDispatchesCorrectly(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	handlers := registry.initXKMSHandlers()
	expectedHandlers := []string{
		// Core key management
		"xkms.listBackends",
		"xkms.generateKey",
		"xkms.getKey",
		"xkms.deleteKey",
		"xkms.listKeys",
		"xkms.rotateKey",
		"xkms.sign",
		"xkms.verify",
		"xkms.encrypt",
		"xkms.decrypt",
		"xkms.saveCert",
		"xkms.getCert",
		"xkms.deleteCert",
		"xkms.listCerts",
		"xkms.certExists",
		"xkms.saveCertChain",
		"xkms.getCertChain",
		"xkms.seal",
		"xkms.unseal",
		"xkms.canSeal",
		// PIV operations
		"xkms.piv.listSlots",
		"xkms.piv.getCert",
		"xkms.piv.storeCert",
		"xkms.piv.deleteCert",
		"xkms.piv.generateKey",
		"xkms.piv.importCert",
		"xkms.piv.exportCert",
		"xkms.piv.generateCSR",
		// FIDO2 / WebAuthn ceremony operations
		"xkms.fido2.beginRegistration",
		"xkms.fido2.finishRegistration",
		"xkms.fido2.beginAddCredential",
		"xkms.fido2.beginAuthentication",
		"xkms.fido2.finishAuthentication",
		// CA operations
		"xkms.ca.getBundle",
		"xkms.ca.getCert",
		"xkms.ca.signCSR",
		"xkms.ca.issueCert",
		"xkms.ca.revokeCert",
		"xkms.ca.generateCRL",
		"xkms.ca.isRevoked",
		// PIN operations
		"xkms.pin.setSO",
		"xkms.pin.setUser",
		"xkms.pin.changeSO",
		"xkms.pin.changeUser",
		"xkms.pin.verifySO",
		"xkms.pin.verifyUser",
		"xkms.pin.lockoutStatus",
		"xkms.pin.resetLockout",
	}

	for _, name := range expectedHandlers {
		_, ok := handlers[name]
		assert.True(t, ok, "expected handler for %q to be registered", name)
	}

	assert.Equal(t, len(expectedHandlers), len(handlers),
		"handler map should contain exactly the expected number of handlers")
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestXKMSHandlersMissingRequiredArgs verifies required field validation:
// each handler rejects a call that omits one required argument and names
// that argument in the error.
func TestXKMSHandlersMissingRequiredArgs(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	tests := []struct {
		name    string
		handler XKMSToolHandler
		args    map[string]interface{}
		missing string
	}{
		{"generateKey key_id", registry.handleXKMSGenerateKey, map[string]interface{}{}, "key_id"},
		{"getKey key_id", registry.handleXKMSGetKey, map[string]interface{}{}, "key_id"},
		{"deleteKey key_id", registry.handleXKMSDeleteKey, map[string]interface{}{}, "key_id"},
		{"rotateKey key_id", registry.handleXKMSRotateKey, map[string]interface{}{}, "key_id"},
		{"sign key_id", registry.handleXKMSSign, map[string]interface{}{"data": b64("test")}, "key_id"},
		{"sign data", registry.handleXKMSSign, map[string]interface{}{"key_id": "test-key"}, "data"},
		{"verify key_id", registry.handleXKMSVerify, map[string]interface{}{"data": b64("test"), "signature": b64("sig")}, "key_id"},
		{"verify data", registry.handleXKMSVerify, map[string]interface{}{"key_id": "test-key", "signature": b64("sig")}, "data"},
		{"verify signature", registry.handleXKMSVerify, map[string]interface{}{"key_id": "test-key", "data": b64("test")}, "signature"},
		{"encrypt key_id", registry.handleXKMSEncrypt, map[string]interface{}{"plaintext": b64("test")}, "key_id"},
		{"encrypt plaintext", registry.handleXKMSEncrypt, map[string]interface{}{"key_id": "test-key"}, "plaintext"},
		{"decrypt key_id", registry.handleXKMSDecrypt, map[string]interface{}{"ciphertext": b64("test")}, "key_id"},
		{"decrypt ciphertext", registry.handleXKMSDecrypt, map[string]interface{}{"key_id": "test-key"}, "ciphertext"},
		{"saveCert key_id", registry.handleXKMSSaveCert, map[string]interface{}{"cert_pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"}, "key_id"},
		{"saveCert cert_pem", registry.handleXKMSSaveCert, map[string]interface{}{"key_id": "test-key"}, "cert_pem"},
		{"getCert key_id", registry.handleXKMSGetCert, map[string]interface{}{}, "key_id"},
		{"deleteCert key_id", registry.handleXKMSDeleteCert, map[string]interface{}{}, "key_id"},
		{"certExists key_id", registry.handleXKMSCertExists, map[string]interface{}{}, "key_id"},
		{"saveCertChain key_id", registry.handleXKMSSaveCertChain, map[string]interface{}{"chain_pems": []interface{}{"pem1"}}, "key_id"},
		{"saveCertChain chain_pems", registry.handleXKMSSaveCertChain, map[string]interface{}{"key_id": "test-key"}, "chain_pems"},
		{"getCertChain key_id", registry.handleXKMSGetCertChain, map[string]interface{}{}, "key_id"},
		{"seal data", registry.handleXKMSSeal, map[string]interface{}{}, "data"},
		{"unseal ciphertext", registry.handleXKMSUnseal, map[string]interface{}{}, "ciphertext"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(context.Background(), tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.missing)
		})
	}
}

// TestXKMSSaveCertInvalidPEM verifies PEM validation for saveCert.
func TestXKMSSaveCertInvalidPEM(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	ctx := context.Background()
	_, err := registry.handleXKMSSaveCert(ctx, map[string]interface{}{
		"key_id":   "test-key",
		"cert_pem": "not-valid-pem-data",
	})
	require.Error(t, err)
	var pemErr *XKMSInvalidPEMError
	assert.ErrorAs(t, err, &pemErr)
}

// =============================================================================
// Base64 Decode Error Tests
// =============================================================================

// TestDecodeBase64Arg verifies decoding, the nil result for a missing or
// empty field, and the typed error for invalid input.
func TestDecodeBase64Arg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		want    []byte
		wantErr bool
	}{
		{name: "valid data", args: map[string]interface{}{"data": base64.StdEncoding.EncodeToString([]byte("hello world"))}, want: []byte("hello world")},
		{name: "missing field", args: map[string]interface{}{}},
		{name: "empty string", args: map[string]interface{}{"data": ""}},
		{name: "invalid data", args: map[string]interface{}{"data": "not-valid-base64!!!@@@"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := decodeBase64Arg(tt.args, "data")
			if tt.wantErr {
				var b64Err *Base64DecodeError
				require.ErrorAs(t, err, &b64Err)
				assert.Equal(t, "data", b64Err.Field)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, data)
		})
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

// TestParseHashAlgorithm verifies hash algorithm string parsing.
func TestParseHashAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected crypto.Hash
	}{
		{name: "SHA-256 uppercase", input: "SHA-256", expected: crypto.SHA256},
		{name: "SHA-256 lowercase", input: "sha-256", expected: crypto.SHA256},
		{name: "SHA256 no dash", input: "sha256", expected: crypto.SHA256},
		{name: "SHA-384", input: "sha-384", expected: crypto.SHA384},
		{name: "SHA-512", input: "sha-512", expected: crypto.SHA512},
		{name: "empty defaults to SHA256", input: "", expected: crypto.SHA256},
		{name: "unknown defaults to SHA256", input: "md5", expected: crypto.SHA256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseHashAlgorithm(tt.input))
		})
	}
}

// TestExtractPublicKeyPEM verifies public key PEM extraction.
func TestExtractPublicKeyPEM(t *testing.T) {
	// Nil private key
	assert.Equal(t, "", extractPublicKeyPEM(nil))

	// Non-signer interface
	assert.Equal(t, "", extractPublicKeyPEM("not-a-key"))
}

// TestExtractPublicKeyPEMValidKey verifies PEM extraction from a valid ECDSA key.
func TestExtractPublicKeyPEMValidKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	result := extractPublicKeyPEM(key)
	assert.Contains(t, result, "-----BEGIN PUBLIC KEY-----")
	assert.Contains(t, result, "-----END PUBLIC KEY-----")
}

// TestXKMSKeyAttrsFromArgs verifies KeyAttributes construction from args and
// the tenant carried in the context.
func TestXKMSKeyAttrsFromArgs(t *testing.T) {
	tests := []struct {
		name           string
		ctx            context.Context
		args           map[string]interface{}
		wantCN         string
		wantExportable bool
		wantStoreType  string
		wantTenant     string
	}{
		{
			name:           "backend without tenant",
			ctx:            context.Background(),
			args:           map[string]interface{}{"key_id": "my-test-key", "backend": "software", "exportable": true},
			wantCN:         "my-test-key",
			wantExportable: true,
			wantStoreType:  "software",
		},
		{
			name:   "no backend",
			ctx:    context.Background(),
			args:   map[string]interface{}{"key_id": "my-key"},
			wantCN: "my-key",
		},
		{
			name:           "tenant in context",
			ctx:            tenantctx.WithTenant(context.Background(), "my-tenant"),
			args:           map[string]interface{}{"key_id": "tenant-key", "backend": "software", "exportable": true},
			wantCN:         "tenant-key",
			wantExportable: true,
			wantStoreType:  "software",
			wantTenant:     "my-tenant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := xkmsKeyAttrsFromArgs(tt.ctx, tt.args)
			assert.Equal(t, tt.wantCN, attrs.CN)
			assert.Equal(t, tt.wantExportable, attrs.Exportable)
			assert.Equal(t, tt.wantStoreType, string(attrs.StoreType))
			assert.Equal(t, tt.wantTenant, attrs.TenantID)
		})
	}
}

// =============================================================================
// Tool Definition Schema Tests
// =============================================================================

// TestXKMSToolSchemas verifies that tool definitions have proper InputSchema structure.
func TestXKMSToolSchemas(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	tools := registry.ListTools()

	// Find the xkms tools and verify they have proper schemas
	for _, tool := range tools {
		if !isXKMSTool(tool.Name) {
			continue
		}

		assert.NotEmpty(t, tool.Description, "tool %q must have a description", tool.Name)
		assert.NotNil(t, tool.InputSchema, "tool %q must have an InputSchema", tool.Name)

		schemaType, ok := tool.InputSchema["type"].(string)
		assert.True(t, ok, "tool %q InputSchema must have a 'type' field", tool.Name)
		assert.Equal(t, "object", schemaType, "tool %q InputSchema type must be 'object'", tool.Name)
	}
}

// TestXKMSGenerateKeySchemaHasRequiredFields verifies the generateKey schema.
func TestXKMSGenerateKeySchemaHasRequiredFields(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	tools := registry.ListTools()

	var genTool *ToolDefinition
	for i := range tools {
		if tools[i].Name == "xkms.generateKey" {
			genTool = &tools[i]
			break
		}
	}

	require.NotNil(t, genTool, "xkms.generateKey must be registered")

	required, ok := genTool.InputSchema["required"].([]string)
	require.True(t, ok, "InputSchema must have 'required' field")
	assert.Contains(t, required, "key_id")

	props, ok := genTool.InputSchema["properties"].(map[string]interface{})
	require.True(t, ok, "InputSchema must have 'properties' field")
	assert.Contains(t, props, "key_id")
	assert.Contains(t, props, "backend")
	assert.Contains(t, props, "key_type")
	assert.Contains(t, props, "algorithm")
}

// TestXKMSSignSchemaHasRequiredFields verifies the sign schema.
func TestXKMSSignSchemaHasRequiredFields(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	tools := registry.ListTools()

	var signTool *ToolDefinition
	for i := range tools {
		if tools[i].Name == "xkms.sign" {
			signTool = &tools[i]
			break
		}
	}

	require.NotNil(t, signTool, "xkms.sign must be registered")

	required, ok := signTool.InputSchema["required"].([]string)
	require.True(t, ok, "InputSchema must have 'required' field")
	assert.Contains(t, required, "key_id")
	assert.Contains(t, required, "data")
}

// =============================================================================
// CanSeal Handler Tests
// =============================================================================

// TestXKMSCanSealHandler verifies the canSeal handler returns a boolean result.
// Since xkms may not be initialized in unit tests, we verify the response format.
func TestXKMSCanSealHandler(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	ctx := context.Background()

	// canSeal should return a result regardless of initialization state
	result, err := registry.handleXKMSCanSeal(ctx, map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	_, hasSealField := resultMap["can_seal"]
	assert.True(t, hasSealField, "result must contain 'can_seal' field")
}

// TestXKMSCanSealWithBackend verifies canSeal with a specific backend parameter.
func TestXKMSCanSealWithBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	ctx := context.Background()

	result, err := registry.handleXKMSCanSeal(ctx, map[string]interface{}{
		"backend": "nonexistent-backend",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	canSeal, ok := resultMap["can_seal"].(bool)
	require.True(t, ok)
	assert.False(t, canSeal, "nonexistent backend should not support sealing")
}

// =============================================================================
// ListBackends Handler Tests
// =============================================================================

// TestXKMSListBackendsHandler verifies the listBackends handler.
func TestXKMSListBackendsHandler(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	ctx := context.Background()
	result, err := registry.handleXKMSListBackends(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)

	_, hasBackends := resultMap["backends"]
	assert.True(t, hasBackends, "result must contain 'backends' field")
}

// =============================================================================
// ensureXKMSInitialized Tests
// =============================================================================

// TestEnsureXKMSInitialized verifies the initialization check returns an error
// when the xkms service is not initialized.
func TestEnsureXKMSInitialized(t *testing.T) {
	err := ensureXKMSInitialized()
	require.Error(t, err)

	var initErr *XKMSNotInitializedError
	assert.ErrorAs(t, err, &initErr)
	assert.Equal(t, "xkms service not initialized", initErr.Error())
}

// =============================================================================
// PIV Handler Tests - xkms not initialized
// =============================================================================

// TestXKMSPIVHandlersNotInitialized verifies PIV handlers return initialization
// errors when xkms is not set up.
func TestXKMSPIVHandlersNotInitialized(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(ctx context.Context, args map[string]interface{}) (interface{}, error)
		args    map[string]interface{}
	}{
		{
			name:    "PIV ListSlots",
			handler: registry.handleXKMSPIVListSlots,
			args:    map[string]interface{}{"backend": "pkcs11"},
		},
		{
			name:    "PIV GetCert",
			handler: registry.handleXKMSPIVGetCert,
			args:    map[string]interface{}{"backend": "pkcs11", "slot": "9a"},
		},
		{
			name:    "PIV StoreCert",
			handler: registry.handleXKMSPIVStoreCert,
			args: map[string]interface{}{
				"backend":     "pkcs11",
				"slot":        "9a",
				"certificate": base64.StdEncoding.EncodeToString([]byte("cert-data")),
				"format":      "der",
			},
		},
		{
			name:    "PIV DeleteCert",
			handler: registry.handleXKMSPIVDeleteCert,
			args:    map[string]interface{}{"backend": "pkcs11", "slot": "9a"},
		},
		{
			name:    "PIV GenerateKey",
			handler: registry.handleXKMSPIVGenerateKey,
			args:    map[string]interface{}{"backend": "pkcs11", "slot": "9a"},
		},
		{
			name:    "PIV GenerateCSR",
			handler: registry.handleXKMSPIVGenerateCSR,
			args:    map[string]interface{}{"backend": "pkcs11", "slot": "9a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(ctx, tt.args)
			require.Error(t, err)
			var initErr *XKMSNotInitializedError
			assert.ErrorAs(t, err, &initErr)
		})
	}
}

// =============================================================================
// CA Handler Tests - xkms not initialized
// =============================================================================

// TestTrustedCAHandlersNotInitialized verifies CA handlers return errors when
// xkms is not initialized.
func TestTrustedCAHandlersNotInitialized(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(ctx context.Context, args map[string]interface{}) (interface{}, error)
		args    map[string]interface{}
	}{
		{
			name:    "CA GetBundle",
			handler: registry.handleTrustedCAGetBundle,
			args:    map[string]interface{}{},
		},
		{
			name:    "CA GetCert",
			handler: registry.handleTrustedCAGetCert,
			args:    map[string]interface{}{},
		},
		{
			name:    "CA SignCSR",
			handler: registry.handleTrustedCASignCSR,
			args:    map[string]interface{}{"csr_pem": "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----"},
		},
		{
			name:    "CA IssueCert",
			handler: registry.handleTrustedCAIssueCert,
			args:    map[string]interface{}{"profile": "server", "common_name": "test.example.com"},
		},
		{
			name:    "CA RevokeCert",
			handler: registry.handleTrustedCARevokeCert,
			args:    map[string]interface{}{"serial_number": "123456"},
		},
		{
			name:    "CA GenerateCRL",
			handler: registry.handleTrustedCAGenerateCRL,
			args:    map[string]interface{}{},
		},
		{
			name:    "CA IsRevoked",
			handler: registry.handleTrustedCAIsRevoked,
			args:    map[string]interface{}{"serial_number": "123456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(ctx, tt.args)
			require.Error(t, err)
			var initErr *XKMSNotInitializedError
			assert.ErrorAs(t, err, &initErr)
		})
	}
}

// =============================================================================
// PIN Handler Tests - xkms not initialized
// =============================================================================

// TestXKMSPINHandlersNotInitialized verifies PIN handlers return errors when
// xkms is not initialized.
func TestXKMSPINHandlersNotInitialized(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(ctx context.Context, args map[string]interface{}) (interface{}, error)
		args    map[string]interface{}
	}{
		{
			name:    "PIN SetSO",
			handler: registry.handleXKMSPINSetSO,
			args:    map[string]interface{}{"new_so_pin": "123456"},
		},
		{
			name:    "PIN SetUser",
			handler: registry.handleXKMSPINSetUser,
			args:    map[string]interface{}{"so_pin": "123456", "new_user_pin": "654321"},
		},
		{
			name:    "PIN ChangeSO",
			handler: registry.handleXKMSPINChangeSO,
			args:    map[string]interface{}{"current_so_pin": "123456", "new_so_pin": "654321"},
		},
		{
			name:    "PIN ChangeUser",
			handler: registry.handleXKMSPINChangeUser,
			args:    map[string]interface{}{"current_user_pin": "123456", "new_user_pin": "654321"},
		},
		{
			name:    "PIN VerifySO",
			handler: registry.handleXKMSPINVerifySO,
			args:    map[string]interface{}{"so_pin": "123456"},
		},
		{
			name:    "PIN VerifyUser",
			handler: registry.handleXKMSPINVerifyUser,
			args:    map[string]interface{}{"user_pin": "123456"},
		},
		{
			name:    "PIN LockoutStatus",
			handler: registry.handleXKMSPINLockoutStatus,
			args:    map[string]interface{}{},
		},
		{
			name:    "PIN ResetLockout",
			handler: registry.handleXKMSPINResetLockout,
			args:    map[string]interface{}{"so_pin": "123456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(ctx, tt.args)
			require.Error(t, err)
			var initErr *XKMSNotInitializedError
			assert.ErrorAs(t, err, &initErr)
		})
	}
}

// =============================================================================
// Handler Tests (xkms not initialized)
// =============================================================================

// TestXKMSHandlersUninitialized verifies that each key and certificate handler,
// given valid arguments, returns an XKMSOperationError naming its operation
// when xkms is not initialized.
func TestXKMSHandlersUninitialized(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	keyArgs := map[string]interface{}{"key_id": "test-key"}

	tests := []struct {
		name      string
		handler   XKMSToolHandler
		args      map[string]interface{}
		operation string
	}{
		{"listCerts", registry.handleXKMSListCerts, map[string]interface{}{}, "listCerts"},
		{"listCerts with backend", registry.handleXKMSListCerts, map[string]interface{}{"backend": "software"}, "listCerts"},
		{"listCerts with pkcs11 backend", registry.handleXKMSListCerts, map[string]interface{}{"backend": "pkcs11"}, "listCerts"},
		{"certExists", registry.handleXKMSCertExists, keyArgs, "certExists"},
		{"getCert", registry.handleXKMSGetCert, keyArgs, "getCert"},
		{"deleteCert", registry.handleXKMSDeleteCert, keyArgs, "deleteCert"},
		{"getCertChain", registry.handleXKMSGetCertChain, keyArgs, "getCertChain"},
		{"getCertChain with backend", registry.handleXKMSGetCertChain, map[string]interface{}{"key_id": "chain-key", "backend": "software"}, "getCertChain"},
		{"generateKey", registry.handleXKMSGenerateKey, keyArgs, "generateKey"},
		{"generateKey rsa", registry.handleXKMSGenerateKey, map[string]interface{}{"key_id": "rsa-key", "key_type": "rsa"}, "generateKey"},
		{"generateKey rsa with key size", registry.handleXKMSGenerateKey, map[string]interface{}{"key_id": "rsa-key-4096", "key_type": "rsa", "key_size": float64(4096)}, "generateKey"},
		{"generateKey rsa algorithm", registry.handleXKMSGenerateKey, map[string]interface{}{"key_id": "rsa-alg-key", "algorithm": "rsa"}, "generateKey"},
		{"generateKey ecdsa", registry.handleXKMSGenerateKey, map[string]interface{}{"key_id": "ecdsa-key", "key_type": "ecdsa"}, "generateKey"},
		{"generateKey ecdsa with curve", registry.handleXKMSGenerateKey, map[string]interface{}{"key_id": "ecdsa-p384", "key_type": "ecdsa", "curve": "P-384"}, "generateKey"},
		{"generateKey ed25519", registry.handleXKMSGenerateKey, map[string]interface{}{"key_id": "ed25519-key", "key_type": "ed25519"}, "generateKey"},
		{"generateKey with backend", registry.handleXKMSGenerateKey, map[string]interface{}{"key_id": "backend-key", "backend": "software"}, "generateKey"},
		{"getKey", registry.handleXKMSGetKey, keyArgs, "getKey"},
		{"getKey with backend", registry.handleXKMSGetKey, map[string]interface{}{"key_id": "test-key", "backend": "software"}, "getKey"},
		{"deleteKey", registry.handleXKMSDeleteKey, keyArgs, "deleteKey"},
		{"rotateKey", registry.handleXKMSRotateKey, keyArgs, "rotateKey"},
		{"rotateKey with backend", registry.handleXKMSRotateKey, map[string]interface{}{"key_id": "test-key", "backend": "software"}, "rotateKey"},
		{"sign", registry.handleXKMSSign, map[string]interface{}{"key_id": "test-key", "data": b64("data-to-sign")}, "sign"},
		{"sign with hash algorithm", registry.handleXKMSSign, map[string]interface{}{"key_id": "sign-key", "data": b64("test data"), "hash_algorithm": "sha-512"}, "sign"},
		{"encrypt", registry.handleXKMSEncrypt, map[string]interface{}{"key_id": "test-key", "plaintext": b64("secret-data")}, "encrypt"},
		{"encrypt with aad", registry.handleXKMSEncrypt, map[string]interface{}{"key_id": "enc-key", "plaintext": b64("secret"), "aad": b64("additional-data")}, "encrypt"},
		{"decrypt", registry.handleXKMSDecrypt, map[string]interface{}{"key_id": "test-key", "ciphertext": b64("cipher")}, "decrypt"},
		{"decrypt with aad", registry.handleXKMSDecrypt, map[string]interface{}{"key_id": "dec-key", "ciphertext": b64("encrypted"), "aad": b64("additional-data")}, "decrypt"},
		{"seal with aad", registry.handleXKMSSeal, map[string]interface{}{"data": b64("seal-data"), "aad": b64("seal-aad")}, "seal"},
		{"seal with backend", registry.handleXKMSSeal, map[string]interface{}{"data": b64("seal-data"), "backend": "software"}, "seal"},
		{"listKeys", registry.handleXKMSListKeys, map[string]interface{}{}, "listKeys"},
		{"listKeys with backend", registry.handleXKMSListKeys, map[string]interface{}{"backend": "software"}, "listKeys"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(context.Background(), tt.args)
			require.Error(t, err)

			var opErr *XKMSOperationError
			require.ErrorAs(t, err, &opErr)
			assert.Equal(t, tt.operation, opErr.Operation)
		})
	}
}

// TestXKMSSaveCertChainInvalidPEM verifies saveCertChain rejects invalid PEM data.
func TestXKMSSaveCertChainInvalidPEM(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSSaveCertChain(ctx, map[string]interface{}{
		"key_id":     "test-key",
		"chain_pems": []interface{}{"not-valid-pem"},
	})
	require.Error(t, err)
	var pemErr *XKMSInvalidPEMError
	assert.ErrorAs(t, err, &pemErr)
}

// TestXKMSVerifyUninitialized verifies verify returns valid=false when
// xkms is not initialized (verification fails gracefully).
func TestXKMSVerifyUninitialized(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.handleXKMSVerify(ctx, map[string]interface{}{
		"key_id":    "test-key",
		"data":      base64.StdEncoding.EncodeToString([]byte("data")),
		"signature": base64.StdEncoding.EncodeToString([]byte("sig")),
	})
	// Verify handler converts verify errors to valid=false result, not an error
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	valid, ok := resultMap["valid"].(bool)
	require.True(t, ok)
	assert.False(t, valid, "verification with uninitialized xkms should be invalid")
}

// =============================================================================
// executeXKMSTool Dispatch Tests
// =============================================================================

// TestExecuteXKMSToolInitializesHandlerMap verifies the handler map is lazily
// initialized on first call.
func TestExecuteXKMSToolInitializesHandlerMap(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Reset global handler map to test lazy initialization
	xkmsHandlerMap = nil

	// Any xkms tool call should initialize the map
	_, err := registry.executeXKMSTool(ctx, "xkms.canSeal", map[string]interface{}{})
	require.NoError(t, err)

	// Map should now be initialized
	assert.NotNil(t, xkmsHandlerMap)
	assert.Greater(t, len(xkmsHandlerMap), 0)
}

// TestSaveCertChainInvalidCertificateParse verifies that saveCertChain returns
// a parse error when PEM contains invalid certificate data.
func TestSaveCertChainInvalidCertificateParse(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Valid PEM block but invalid certificate data
	invalidCertPEM := "-----BEGIN CERTIFICATE-----\nYm9ndXMgZGF0YQ==\n-----END CERTIFICATE-----"

	_, err := registry.handleXKMSSaveCertChain(ctx, map[string]interface{}{
		"key_id":     "test-key",
		"chain_pems": []interface{}{invalidCertPEM},
	})
	require.Error(t, err)
	var parseErr *XKMSCertificateParseError
	assert.ErrorAs(t, err, &parseErr)
}

// errTestService is a test error for mock service failures.
var errTestService = errors.New("test service error")

// newTestRegistryWithMockService creates a ToolRegistry backed by a custom mockService.
func newTestRegistryWithMockService(t *testing.T, svc service.QuicBaaS) *ToolRegistry {
	t.Helper()
	log := newTestLogger(t)
	return NewToolRegistry(svc, log)
}

// =============================================================================
// executeRebalanceStatus (0% coverage)
// =============================================================================

func TestMCPCoverage91Extra_RebalanceStatus_Success(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeRebalanceStatus(ctx, map[string]interface{}{
		"operation_id": "op-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestMCPCoverage91Extra_RebalanceStatus_Error(t *testing.T) {
	svc := &mockService{
		getMigrationProgressFunc: func(_ context.Context, _ *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
			return nil, errTestService
		},
	}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeRebalanceStatus(ctx, map[string]interface{}{
		"operation_id": "rebalance-1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// executeRebalanceCluster error path
// =============================================================================

func TestMCPCoverage91Extra_RebalanceCluster_NilResponse(t *testing.T) {
	svc := &mockService{} // RebalanceCluster returns nil, nil
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeRebalanceCluster(ctx, map[string]interface{}{
		"targetShardCount": float64(3),
		"strategy":         "balanced",
	})
	require.NoError(t, err)
	// nil response from service is marshaled as JSON null
	assert.Nil(t, result)
}

// =============================================================================
// executeBackupCreate error path (returns nil from mock)
// =============================================================================

func TestMCPCoverage91Extra_BackupCreate_NilResponse(t *testing.T) {
	svc := &mockService{} // CreateBackup returns nil, nil
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeBackupCreate(ctx, map[string]interface{}{
		"name":        "test-backup",
		"description": "test desc",
		"compression": "zstd",
		"encryption":  "aes256",
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// executeBackupRestore error path (returns nil from mock)
// =============================================================================

func TestMCPCoverage91Extra_BackupRestore_NilResponse(t *testing.T) {
	svc := &mockService{} // RestoreBackup returns nil, nil
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeBackupRestore(ctx, map[string]interface{}{
		"backup_id": "backup-123",
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// executeRoleCreate error path (returns nil from mock)
// =============================================================================

func TestMCPCoverage91Extra_RoleCreate_NilResponse(t *testing.T) {
	svc := &mockService{} // CreateAuthRole returns nil, nil
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeRoleCreate(ctx, map[string]interface{}{
		"roleName":    "testrole",
		"permissions": []interface{}{"read", "write"},
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// executeCacheConfigUpdate success path
// =============================================================================

func TestMCPCoverage91Extra_CacheConfigUpdate_NilResponse(t *testing.T) {
	svc := &mockService{} // UpdateCacheConfig returns nil, nil
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeCacheConfigUpdate(ctx, map[string]interface{}{
		"l1_size":        float64(1000),
		"l1_ttl_seconds": float64(300),
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// Batch operation parse errors - executeBatchPut/Get/Delete
// =============================================================================

// TestMCPCoverage91Extra_BatchOperationParseErrors verifies each batch tool
// rejects a malformed operation with a BatchOperationParseError naming the
// offending field.
func TestMCPCoverage91Extra_BatchOperationParseErrors(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)

	type batchFunc func(context.Context, map[string]interface{}) (interface{}, error)

	tests := []struct {
		name  string
		exec  batchFunc
		op    interface{}
		field string // empty: only the error type is asserted
	}{
		{"put not an object", registry.executeBatchPut, "not-an-object", "operation"},
		{"put missing entity type", registry.executeBatchPut, map[string]interface{}{"id": float64(1), "data": `{"key":"value"}`}, "entityType"},
		{"put missing id", registry.executeBatchPut, map[string]interface{}{"entity_type": "users", "data": `{"key":"value"}`}, "id"},
		{"put missing data", registry.executeBatchPut, map[string]interface{}{"entity_type": "users", "id": float64(1)}, "data"},
		{"get not an object", registry.executeBatchGet, "not-an-object", ""},
		{"get missing entity type", registry.executeBatchGet, map[string]interface{}{"id": float64(1)}, "entityType"},
		{"get missing id", registry.executeBatchGet, map[string]interface{}{"entity_type": "users"}, "id"},
		{"delete not an object", registry.executeBatchDelete, "not-an-object", ""},
		{"delete missing entity type", registry.executeBatchDelete, map[string]interface{}{"id": float64(1)}, "entityType"},
		{"delete missing id", registry.executeBatchDelete, map[string]interface{}{"entity_type": "users"}, "id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.exec(context.Background(), map[string]interface{}{
				"operations": []interface{}{tt.op},
			})
			require.Error(t, err)

			var batchErr *BatchOperationParseError
			require.ErrorAs(t, err, &batchErr)
			assert.Equal(t, 0, batchErr.Index)
			if tt.field != "" {
				assert.Equal(t, tt.field, batchErr.Field)
			}
		})
	}
}

// =============================================================================
// parseBatchOperationsArray - invalid type and empty array
// =============================================================================

func TestMCPCoverage91Extra_ParseBatchOperationsArray_InvalidType(t *testing.T) {
	_, err := parseBatchOperationsArray(map[string]interface{}{
		"operations": "not-an-array",
	})
	require.Error(t, err)
	var invalidErr *BatchOperationsInvalidError
	assert.ErrorAs(t, err, &invalidErr)
}

func TestMCPCoverage91Extra_ParseBatchOperationsArray_EmptyArray(t *testing.T) {
	_, err := parseBatchOperationsArray(map[string]interface{}{
		"operations": []interface{}{},
	})
	require.Error(t, err)
	var emptyErr *BatchOperationsEmptyError
	assert.ErrorAs(t, err, &emptyErr)
}

// =============================================================================
// marshalResult - nil input
// =============================================================================

func TestMCPCoverage91Extra_MarshalResult_NilInput(t *testing.T) {
	result, err := marshalResult(nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// XKMS SaveCertChain - empty chain
// =============================================================================

func TestMCPCoverage91Extra_XKMSSaveCertChain_EmptyChain(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSSaveCertChain(ctx, map[string]interface{}{
		"key_id":     "chain-key",
		"chain_pems": []interface{}{},
	})
	require.Error(t, err)
}

// =============================================================================
// XKMS SaveCertChain - non-string PEM
// =============================================================================

func TestMCPCoverage91Extra_XKMSSaveCertChain_NonStringPEM(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSSaveCertChain(ctx, map[string]interface{}{
		"key_id":     "chain-key",
		"chain_pems": []interface{}{42},
	})
	require.Error(t, err)
}

// =============================================================================
// Crypto handler service unavailable errors
// =============================================================================

func TestMCPCoverage91Extra_CryptoSign_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeCryptoSign(ctx, map[string]interface{}{
		"key_id":         "key-1",
		"message":        "aGVsbG8=",
		"hash_algorithm": "SHA-256",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crypto")
}

func TestMCPCoverage91Extra_CryptoVerify_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeCryptoVerify(ctx, map[string]interface{}{
		"key_id":         "key-1",
		"message":        "aGVsbG8=",
		"signature":      "c2ln",
		"hash_algorithm": "SHA-256",
	})
	require.Error(t, err)
}

func TestMCPCoverage91Extra_CryptoHMAC_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeCryptoHMAC(ctx, map[string]interface{}{
		"key_id":         "key-1",
		"message":        "aGVsbG8=",
		"hash_algorithm": "SHA-256",
	})
	require.Error(t, err)
}

func TestMCPCoverage91Extra_CryptoVerifyHMAC_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeCryptoVerifyHMAC(ctx, map[string]interface{}{
		"key_id":         "key-1",
		"message":        "aGVsbG8=",
		"hmac":           "aG1hYw==",
		"hash_algorithm": "SHA-256",
	})
	require.Error(t, err)
}

func TestMCPCoverage91Extra_CryptoHash_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeCryptoHash(ctx, map[string]interface{}{
		"data":      "aGVsbG8=",
		"algorithm": "SHA-256",
	})
	require.Error(t, err)
}

func TestMCPCoverage91Extra_CryptoRandom_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeCryptoRandom(ctx, map[string]interface{}{
		"length": float64(32),
	})
	require.Error(t, err)
}

// =============================================================================
// Secrets handler service unavailable errors
// =============================================================================

func TestMCPCoverage91Extra_SecretsServiceUnavailable(t *testing.T) {
	svc := &mockService{} // Secrets() returns nil
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	// Test various secrets functions
	fns := []struct {
		name string
		fn   func(context.Context, map[string]interface{}) (interface{}, error)
		args map[string]interface{}
	}{
		{"get", registry.executeSecretsGet, map[string]interface{}{"path": "secret/test"}},
		{"list", registry.executeSecretsList, map[string]interface{}{"path": "secret/"}},
		{"delete", registry.executeSecretsDelete, map[string]interface{}{"path": "secret/test"}},
		{"versions", registry.executeSecretsVersions, map[string]interface{}{"path": "secret/test"}},
		{"rollback", registry.executeSecretsRollback, map[string]interface{}{"path": "secret/test", "version": float64(1)}},
		{"rotate", registry.executeSecretsRotate, map[string]interface{}{"path": "secret/test"}},
		{"set_rotation", registry.executeSecretsSetRotationPolicy, map[string]interface{}{"path": "secret/test", "interval_hours": float64(24)}},
		{"get_rotation", registry.executeSecretsGetRotationPolicy, map[string]interface{}{"path": "secret/test"}},
		{"update_status", registry.executeSecretsUpdateStatus, map[string]interface{}{"path": "secret/test", "status": "active"}},
		{"watch", registry.executeSecretsWatch, map[string]interface{}{"path": "secret/test"}},
	}

	for _, fn := range fns {
		t.Run(fn.name, func(t *testing.T) {
			_, err := fn.fn(ctx, fn.args)
			require.Error(t, err)
		})
	}
}

// =============================================================================
// parseTransactionOps - invalid item type in array
// =============================================================================

func TestMCPCoverage91Extra_ParseTransactionOps_InvalidItem(t *testing.T) {
	_, err := parseTransactionOps(map[string]interface{}{
		"ops": []interface{}{
			"not-an-object",
		},
	}, "ops")
	require.Error(t, err)
	var parseErr *TransactionParseError
	assert.ErrorAs(t, err, &parseErr)
}

// =============================================================================
// CacheConfig with real response
// =============================================================================

func TestMCPCoverage91Extra_CacheConfig_WithResponse(t *testing.T) {
	svc := &mockService{
		getCacheStatsFunc: func(_ context.Context, _ *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
			return &service.GetCacheStatsResponse{
				HitRate: 0.95,
				Size:    500,
				Hits:    475,
				Misses:  25,
			}, nil
		},
	}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	result, err := registry.executeCacheStats(ctx, map[string]interface{}{
		"entityType": "user",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// KMS Encrypt and Decrypt via service unavailable
// =============================================================================

func TestMCPCoverage91Extra_KMSEncrypt_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeKMSEncrypt(ctx, map[string]interface{}{
		"key_id":    "k1",
		"plaintext": "aGVsbG8=",
	})
	require.Error(t, err)
}

func TestMCPCoverage91Extra_KMSDecrypt_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeKMSDecrypt(ctx, map[string]interface{}{
		"key_id":     "k1",
		"ciphertext": "Y3Q=",
	})
	require.Error(t, err)
}

// =============================================================================
// KMS ops service unavailable
// =============================================================================

func TestMCPCoverage91Extra_KMSOps_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	// executeKMSBackendTest
	_, err := registry.executeKMSBackendTest(ctx, map[string]interface{}{
		"backend": "software",
	})
	require.Error(t, err)

	// executeKMSKeyVersions
	_, err = registry.executeKMSKeyVersions(ctx, map[string]interface{}{
		"key_id": "k1",
	})
	require.Error(t, err)

	// executeKMSKeyStatusUpdate
	_, err = registry.executeKMSKeyStatusUpdate(ctx, map[string]interface{}{
		"key_id": "k1",
		"status": "enabled",
	})
	require.Error(t, err)

	// executeKMSBackendStatus
	_, err = registry.executeKMSBackendStatus(ctx, map[string]interface{}{
		"backend": "software",
	})
	require.Error(t, err)

	// executeKMSSetKeySafety
	_, err = registry.executeKMSSetKeySafety(ctx, map[string]interface{}{
		"key_id": "k1",
		"safety": "high",
	})
	require.Error(t, err)

	// executeKMSClearKeySafety
	_, err = registry.executeKMSClearKeySafety(ctx, map[string]interface{}{
		"key_id": "k1",
	})
	require.Error(t, err)

	// executeKMSSafetyCheck
	_, err = registry.executeKMSSafetyCheck(ctx, map[string]interface{}{
		"key_id": "k1",
	})
	require.Error(t, err)

	// executeKMSSafetyReport
	_, err = registry.executeKMSSafetyReport(ctx, map[string]interface{}{})
	require.Error(t, err)
}

// =============================================================================
// KMS Export/Unwrap service unavailable
// =============================================================================

func TestMCPCoverage91Extra_KMSExport_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	registry := newTestRegistryWithMockService(t, svc)
	ctx := context.Background()

	_, err := registry.executeKMSExportKey(ctx, map[string]interface{}{
		"key_id": "k1",
	})
	require.Error(t, err)

	_, err = registry.executeKMSUnwrapKey(ctx, map[string]interface{}{
		"key_id":          "k1",
		"wrapped_key":     "d3JhcHBlZA==",
		"wrapping_key_id": "wk1",
	})
	require.Error(t, err)
}

// =============================================================================
// GenerateKey - Invalid Curve
// =============================================================================

func TestXKMSGenerateKey_ECDSAInvalidCurve(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSGenerateKey(ctx, map[string]interface{}{
		"key_id":   "ecdsa-bad-curve",
		"key_type": "ecdsa",
		"curve":    "P-999",
	})
	require.Error(t, err)
	var curveErr *XKMSInvalidCurveError
	assert.ErrorAs(t, err, &curveErr)
	assert.Equal(t, "P-999", curveErr.Curve)
}

// =============================================================================
// Invalid Base64 Arguments
// =============================================================================

// TestXKMSHandlersInvalidBase64 verifies that each handler rejects an argument
// that is not valid base64 with a Base64DecodeError naming the argument,
// before it reaches the xkms library.
func TestXKMSHandlersInvalidBase64(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)

	const bad = "not-valid-base64!!!"
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	tests := []struct {
		name    string
		handler XKMSToolHandler
		args    map[string]interface{}
		field   string
	}{
		{"sign data", registry.handleXKMSSign, map[string]interface{}{"key_id": "test-key", "data": bad}, "data"},
		{"verify data", registry.handleXKMSVerify, map[string]interface{}{"key_id": "verify-key", "data": bad, "signature": b64("sig")}, "data"},
		{"verify signature", registry.handleXKMSVerify, map[string]interface{}{"key_id": "test-key", "data": b64("test"), "signature": bad}, "signature"},
		{"encrypt plaintext", registry.handleXKMSEncrypt, map[string]interface{}{"key_id": "enc-key", "plaintext": bad}, "plaintext"},
		{"encrypt aad", registry.handleXKMSEncrypt, map[string]interface{}{"key_id": "enc-key", "plaintext": b64("data"), "aad": bad}, "aad"},
		{"decrypt ciphertext", registry.handleXKMSDecrypt, map[string]interface{}{"key_id": "dec-key", "ciphertext": bad}, "ciphertext"},
		{"decrypt aad", registry.handleXKMSDecrypt, map[string]interface{}{"key_id": "dec-key", "ciphertext": b64("encrypted"), "aad": bad}, "aad"},
		{"seal data", registry.handleXKMSSeal, map[string]interface{}{"data": bad}, "data"},
		{"seal aad", registry.handleXKMSSeal, map[string]interface{}{"data": b64("seal-data"), "aad": bad}, "aad"},
		{"unseal ciphertext", registry.handleXKMSUnseal, map[string]interface{}{"ciphertext": bad}, "ciphertext"},
		{"unseal nonce", registry.handleXKMSUnseal, map[string]interface{}{"ciphertext": b64("data"), "nonce": bad}, "nonce"},
		{"unseal tag", registry.handleXKMSUnseal, map[string]interface{}{"ciphertext": b64("data"), "tag": bad}, "tag"},
		{"unseal aad", registry.handleXKMSUnseal, map[string]interface{}{"ciphertext": b64("data"), "aad": bad}, "aad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(context.Background(), tt.args)
			require.Error(t, err)

			var b64Err *Base64DecodeError
			require.ErrorAs(t, err, &b64Err)
			assert.Equal(t, tt.field, b64Err.Field)
		})
	}
}

// =============================================================================
// SaveCert - Parse Error Path
// =============================================================================

func TestXKMSSaveCert_UninitializedAfterValidPEM(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Valid PEM block but invalid certificate data covers the parse error path
	validPEM := "-----BEGIN CERTIFICATE-----\nYm9ndXMgZGF0YQ==\n-----END CERTIFICATE-----"
	_, err := registry.handleXKMSSaveCert(ctx, map[string]interface{}{
		"key_id":   "test-key",
		"cert_pem": validPEM,
	})
	require.Error(t, err)
	var parseErr *XKMSCertificateParseError
	assert.ErrorAs(t, err, &parseErr)
}
